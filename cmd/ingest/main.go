// Command ingest polls the registered CPO OCPI feeds and maintains the price
// history. Run with -once for a single pass (cron/CI), or without flags to run
// the in-process scheduler.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"

	"github.com/appmire/charging/internal/config"
	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

func main() {
	once := flag.Bool("once", false, "run a single ingestion pass and exit")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()
	ctx := context.Background()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("connect db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	// Register the known sources (idempotent).
	for _, c := range source.Seeds() {
		if err := st.UpsertCPO(ctx, c); err != nil {
			log.Error("seed cpo", "cpo", c.ID, "err", err)
			os.Exit(1)
		}
	}

	eng := ingest.NewEngine(st, log)

	if *once {
		runOnce(ctx, st, eng, log)
		return
	}

	runScheduler(ctx, st, eng, log)
}

func runOnce(ctx context.Context, st *store.Store, eng *ingest.Engine, log *slog.Logger) {
	srcs := resolveEnabled(ctx, st, log)
	if len(srcs) == 0 {
		log.Warn("no enabled sources with tokens; nothing to ingest",
			"hint", "request a key (myevplatform@energyvision.be), set ENERGYVISION_TOKEN, enable the source")
		return
	}
	if err := eng.RunAll(ctx, srcs); err != nil {
		log.Error("ingestion finished with errors", "err", err)
	}
}

// runScheduler schedules each source by its own cron expression and also runs
// one pass immediately at startup.
func runScheduler(ctx context.Context, st *store.Store, eng *ingest.Engine, log *slog.Logger) {
	srcs := resolveEnabled(ctx, st, log)
	if len(srcs) == 0 {
		log.Warn("no enabled sources with tokens; scheduler idle",
			"hint", "set a source token and enable it, then restart")
	}

	c := cron.New()
	for _, src := range srcs {
		s := src
		if _, err := c.AddFunc(s.CPO.PollCron, func() {
			if err := eng.RunSource(ctx, s); err != nil {
				log.Error("scheduled ingest", "cpo", s.CPO.ID, "err", err)
			}
		}); err != nil {
			log.Error("invalid cron", "cpo", s.CPO.ID, "cron", s.CPO.PollCron, "err", err)
		}
	}
	c.Start()
	defer c.Stop()

	// Initial pass so we don't wait for the first tick.
	if len(srcs) > 0 {
		if err := eng.RunAll(ctx, srcs); err != nil {
			log.Error("startup ingestion errors", "err", err)
		}
	}

	log.Info("ingest scheduler running", "sources", len(srcs))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutting down")
}

func resolveEnabled(ctx context.Context, st *store.Store, log *slog.Logger) []source.Source {
	cpos, err := st.ListEnabledCPOs(ctx)
	if err != nil {
		log.Error("list cpos", "err", err)
		return nil
	}
	return source.Resolve(cpos)
}
