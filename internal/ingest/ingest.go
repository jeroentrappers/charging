// Package ingest is the change-data-capture engine. Because the OCPI feeds only
// ever expose the *current* tariff, we manufacture history by polling: on every
// pass we detect whether a connector's tariff content changed and, if so, close
// the previous version and open a new one (SCD Type 2).
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/normalize"
	"github.com/appmire/charging/internal/pricing"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// Engine runs ingestion against the store.
type Engine struct {
	Store *store.Store
	Log   *slog.Logger
	Limit int // max concurrent sources; 0 -> NumCPU
}

func NewEngine(st *store.Store, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{Store: st, Log: log}
}

// RunAll ingests every source concurrently (bounded). It never returns early on
// a single source failure; per-source errors are logged and recorded in
// ingest_run, and the first error is returned for visibility.
func (e *Engine) RunAll(ctx context.Context, sources []source.Source) error {
	limit := e.Limit
	if limit <= 0 {
		limit = runtime.NumCPU()
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for _, src := range sources {
		g.Go(func() error {
			if !src.HasToken() {
				e.Log.Warn("skipping source without token", "cpo", src.CPO.ID)
				return nil
			}
			return e.RunSource(ctx, src)
		})
	}
	return g.Wait()
}

// RunSource performs one ingestion pass for a single CPO.
func (e *Engine) RunSource(ctx context.Context, src source.Source) (err error) {
	runID, startErr := e.Store.StartRun(ctx, src.CPO.ID)
	if startErr != nil {
		return fmt.Errorf("start run %s: %w", src.CPO.ID, startErr)
	}
	var rowsSeen, changes int
	defer func() {
		if ferr := e.Store.FinishRun(ctx, runID, rowsSeen, changes, err); ferr != nil {
			e.Log.Error("finish run", "cpo", src.CPO.ID, "err", ferr)
		}
		e.Log.Info("ingest pass complete", "cpo", src.CPO.ID,
			"connectors", rowsSeen, "tariff_changes", changes, "err", err)
	}()

	client := src.Client()
	locations, ferr := client.Locations(ctx)
	if ferr != nil {
		return fmt.Errorf("fetch locations %s: %w", src.CPO.ID, ferr)
	}
	tariffs, ferr := client.Tariffs(ctx)
	if ferr != nil {
		return fmt.Errorf("fetch tariffs %s: %w", src.CPO.ID, ferr)
	}

	res := normalize.FromOCPI(src.CPO.ID, locations, tariffs)
	rowsSeen = len(res.Connectors)

	for _, conn := range res.Connectors {
		ch, perr := e.processConnector(ctx, conn, res.Tariffs)
		if perr != nil {
			// Don't abort the whole pass for one bad row; log and continue.
			e.Log.Error("process connector", "cpo", src.CPO.ID,
				"evse", conn.EVSEUID, "connector", conn.ConnectorID, "err", perr)
			continue
		}
		if ch {
			changes++
		}
	}
	return nil
}

// processConnector upserts identity + availability, then applies tariff change
// detection. It returns whether a new tariff version was recorded.
func (e *Engine) processConnector(ctx context.Context, conn model.Connector, tariffs map[string]model.Tariff) (bool, error) {
	id, err := e.Store.UpsertCharger(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("upsert charger: %w", err)
	}

	avail := 0
	if conn.Available() {
		avail = 1
	}
	if err := e.Store.UpsertStatus(ctx, id, conn.EVSEStatus, avail); err != nil {
		return false, fmt.Errorf("upsert status: %w", err)
	}

	// No tariff referenced or not present in this feed: leave history untouched.
	// Honesty rule: absence is "unknown", never recorded as free/zero.
	if conn.TariffID == "" {
		return false, nil
	}
	tar, ok := tariffs[conn.TariffID]
	if !ok {
		return false, nil
	}

	newHash := tar.Hash()
	curHash, found, err := e.Store.CurrentTariffHash(ctx, id)
	if err != nil {
		return false, fmt.Errorf("current tariff hash: %w", err)
	}
	if found && curHash == newHash {
		return false, nil // unchanged
	}

	components, err := tar.Components()
	if err != nil {
		return false, fmt.Errorf("marshal components: %w", err)
	}
	var comparable *float64
	if c, ok := pricing.Comparable(tar, conn.PowerKW); ok {
		comparable = &c
	}
	var srcUpdated *time.Time
	if !tar.LastUpdated.IsZero() {
		t := tar.LastUpdated
		srcUpdated = &t
	}

	if err := e.Store.ReplaceTariff(ctx, id, newHash, components, comparable, tar.Currency, srcUpdated); err != nil {
		return false, fmt.Errorf("replace tariff: %w", err)
	}
	return true, nil
}
