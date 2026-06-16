package ingest

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/appmire/charging/internal/source"
)

// LoadFunc resolves the current set of enabled sources (typically from the DB).
type LoadFunc func(ctx context.Context) []source.Source

// Scheduler runs availability and price passes on each source's own cadence.
// It skips a pass if the previous one of the same job is still running, and it
// periodically reloads the source registry so enabling/adding a CPO does not
// require a restart.
type Scheduler struct {
	eng         *Engine
	log         *slog.Logger
	load        LoadFunc
	reloadEvery time.Duration
	crawling    map[string]bool // monta sources with a running crawler
}

func NewScheduler(eng *Engine, load LoadFunc, reloadEvery time.Duration) *Scheduler {
	if reloadEvery <= 0 {
		reloadEvery = 5 * time.Minute
	}
	return &Scheduler{eng: eng, log: eng.Log, load: load, reloadEvery: reloadEvery, crawling: map[string]bool{}}
}

// Run builds the schedule and blocks until ctx is cancelled, then waits for any
// in-flight pass to finish.
func (s *Scheduler) Run(ctx context.Context) {
	srcs := s.load(ctx)
	sig := fingerprint(srcs)
	c := s.build(ctx, srcs)
	c.Start()
	s.log.Info("scheduler started", "sources", len(srcs))

	// Run a full pass at startup so we don't wait for the first tick — but only
	// for sources that are actually due, so a restart doesn't re-pull large
	// already-fresh feeds (NL/DE/FR are hundreds of thousands of rows).
	go s.runStartupCatchup(ctx, srcs)
	s.ensureCrawlers(ctx, srcs)
	go s.runMobilithekReconcile(ctx)

	ticker := time.NewTicker(s.reloadEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler stopping; waiting for in-flight passes")
			<-c.Stop().Done()
			return
		case <-ticker.C:
			next := s.load(ctx)
			if nsig := fingerprint(next); nsig != sig {
				s.log.Info("source registry changed; rebuilding schedule",
					"before", len(srcs), "after", len(next))
				<-c.Stop().Done()
				srcs, sig = next, fingerprint(next)
				c = s.build(ctx, srcs)
				c.Start()
			}
			s.ensureCrawlers(ctx, next) // pick up newly-enabled monta sources
		}
	}
}

// runStartupCatchup runs a full price pass at boot, but skips any source that
// already ran successfully within its poll cadence — so restarting the ingester
// doesn't re-download + re-ingest large feeds that aren't due yet.
func (s *Scheduler) runStartupCatchup(ctx context.Context, srcs []source.Source) {
	var due []source.Source
	for _, src := range srcs {
		if s.dueAtStartup(ctx, src) {
			due = append(due, src)
		} else {
			s.log.Info("startup: source still fresh, skipping catch-up", "cpo", src.CPO.ID)
		}
	}
	if len(due) > 0 {
		s.eng.RunAll(ctx, due)
	}
}

// dueAtStartup reports whether a source's price pass should run at boot: yes if
// it never ran (or the lookup failed), or its last success is older than one
// full poll interval.
func (s *Scheduler) dueAtStartup(ctx context.Context, src source.Source) bool {
	last, found, err := s.eng.Store.LastSuccess(ctx, src.CPO.ID, KindPrice)
	if err != nil || !found {
		return true
	}
	interval := cronInterval(src.CPO.PollCron)
	if interval <= 0 {
		return true
	}
	return time.Since(last) >= interval
}

// cronInterval estimates the gap between consecutive fires of a 5-field cron
// expression (e.g. daily → 24h, monthly → ~30d). Returns 0 if unparseable.
func cronInterval(expr string) time.Duration {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return 0
	}
	now := time.Now()
	n1 := sched.Next(now)
	if n1.IsZero() {
		return 0
	}
	n2 := sched.Next(n1)
	if n2.IsZero() {
		return 0
	}
	return n2.Sub(n1)
}

// runMobilithekReconcile periodically pulls each Mobilithek source's STATIC
// publication (those with a pull_static_id) and ingests it, backstopping the
// delta-based push feed — which can drop the static snapshot entirely, leaving a
// source with only status deltas and no chargers to attach them to. Slow +
// staggered so a fleet of large tables doesn't surge the DB; status stays on
// push. No-op until sources are configured and the client cert is present.
func (s *Scheduler) runMobilithekReconcile(ctx context.Context) {
	every := durationEnv("MOBILITHEK_PULL_EVERY", 24*time.Hour)
	stagger := durationEnv("MOBILITHEK_PULL_STAGGER", 60*time.Second)
	// Let boot settle (startup catch-up + push warmup) before the first cycle.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	for {
		s.reconcileMobilithekOnce(ctx, stagger)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func (s *Scheduler) reconcileMobilithekOnce(ctx context.Context, stagger time.Duration) {
	targets, err := s.eng.Store.MobilithekPullTargets(ctx)
	if err != nil {
		s.log.Error("mobilithek reconcile: load targets", "err", err)
		return
	}
	if len(targets) == 0 {
		return
	}
	s.log.Info("mobilithek static reconcile: starting", "sources", len(targets), "stagger", stagger.String())
	for i, t := range targets {
		if ctx.Err() != nil {
			return
		}
		if err := s.eng.ReconcileMobilithekStatic(ctx, t.CPOID, t.StaticID); err != nil {
			s.log.Error("mobilithek static reconcile", "cpo", t.CPOID, "err", err)
		}
		if i < len(targets)-1 { // pause between sources to avoid a DB/spool surge
			select {
			case <-ctx.Done():
				return
			case <-time.After(stagger):
			}
		}
	}
}

// durationEnv parses a time.Duration from an env var, falling back to def when
// unset or invalid.
func durationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// ensureCrawlers starts a background price/status crawl for each ready Monta
// source not already crawling. Crawlers run until ctx is cancelled.
func (s *Scheduler) ensureCrawlers(ctx context.Context, srcs []source.Source) {
	for _, src := range srcs {
		if src.CPO.SourceType != "monta" || !src.Ready() || s.crawling[src.CPO.ID] {
			continue
		}
		s.crawling[src.CPO.ID] = true
		src := src
		go s.eng.RunMontaCrawl(ctx, src)
	}
}

// build constructs a cron with skip-if-still-running protection and registers
// both an availability job and a price job per source.
func (s *Scheduler) build(ctx context.Context, srcs []source.Source) *cron.Cron {
	c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))
	for _, src := range srcs {
		if !src.Ready() {
			s.log.Warn("source not ready (missing token); not scheduling", "cpo", src.CPO.ID)
			continue
		}
		src := src
		s.add(c, src.CPO.StatusCron, src.CPO.ID, "availability", func() {
			s.eng.RunAvailability(ctx, src)
		})
		s.add(c, src.CPO.PollCron, src.CPO.ID, "price", func() {
			s.eng.RunPrices(ctx, src)
		})
	}
	return c
}

func (s *Scheduler) add(c *cron.Cron, spec, cpoID, kind string, fn func()) {
	if spec == "" {
		return
	}
	if _, err := c.AddFunc(spec, fn); err != nil {
		s.log.Error("invalid cron spec", "cpo", cpoID, "kind", kind, "spec", spec, "err", err)
	}
}

// fingerprint summarizes the scheduling-relevant fields so reload only rebuilds
// when something actually changed.
func fingerprint(srcs []source.Source) string {
	parts := make([]string, 0, len(srcs))
	for _, s := range srcs {
		hasTok := "0"
		if s.HasToken() {
			hasTok = "1"
		}
		parts = append(parts, s.CPO.ID+"|"+s.CPO.PollCron+"|"+s.CPO.StatusCron+"|"+hasTok)
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}
