package ingest

import (
	"context"
	"log/slog"
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
}

func NewScheduler(eng *Engine, load LoadFunc, reloadEvery time.Duration) *Scheduler {
	if reloadEvery <= 0 {
		reloadEvery = 5 * time.Minute
	}
	return &Scheduler{eng: eng, log: eng.Log, load: load, reloadEvery: reloadEvery}
}

// Run builds the schedule and blocks until ctx is cancelled, then waits for any
// in-flight pass to finish.
func (s *Scheduler) Run(ctx context.Context) {
	srcs := s.load(ctx)
	sig := fingerprint(srcs)
	c := s.build(ctx, srcs)
	c.Start()
	s.log.Info("scheduler started", "sources", len(srcs))

	// Run a full pass at startup so we don't wait for the first tick.
	go s.eng.RunAll(ctx, srcs)

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
		}
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
