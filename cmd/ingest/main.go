// Command ingest polls the registered CPO OCPI feeds and maintains the price
// history. Run with -once for a single pass (cron/CI), or without flags to run
// the in-process scheduler.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/appmire/charging/internal/config"
	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/metrics"
	"github.com/appmire/charging/internal/pricing"
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
	eng.Vehicle = pricing.Vehicle{
		UsableKWh:         cfg.VehicleUsableKWh,
		ConsumptionKWh100: cfg.VehicleConsumption,
	}
	eng.OnRun = func(cpo, kind string, rows, changes int, dur time.Duration, err error) {
		metrics.Observe(time.Now(), cpo, kind, rows, changes, dur, err)
	}
	log.Info("reference vehicle", "usable_kwh", cfg.VehicleUsableKWh, "consumption_kwh100", cfg.VehicleConsumption)

	// Expose Prometheus metrics (best-effort; ingestion continues regardless).
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		log.Info("metrics listening", "addr", cfg.MetricsAddr)
		srv := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server", "err", err)
		}
	}()

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

// runScheduler runs availability + price passes on each source's cadence,
// reloading the registry periodically, until a termination signal arrives.
func runScheduler(ctx context.Context, st *store.Store, eng *ingest.Engine, log *slog.Logger) {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sched := ingest.NewScheduler(eng, func(ctx context.Context) []source.Source {
		return resolveEnabled(ctx, st, log)
	}, 5*time.Minute)
	sched.Run(ctx)
	log.Info("shutdown complete")
}

func resolveEnabled(ctx context.Context, st *store.Store, log *slog.Logger) []source.Source {
	cpos, err := st.ListEnabledCPOs(ctx)
	if err != nil {
		log.Error("list cpos", "err", err)
		return nil
	}
	return source.Resolve(cpos)
}
