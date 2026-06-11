// Package ingest is the change-data-capture engine. Because the OCPI feeds only
// ever expose the *current* tariff, we manufacture history by polling: on every
// price pass we detect whether a connector's tariff content changed and, if so,
// close the previous version and open a new one (SCD Type 2).
//
// Two kinds of pass run on different cadences:
//   - availability: fetch Locations only, refresh status. Frequent (minutes).
//   - price:        fetch Locations + Tariffs, run the SCD2 diff. Rare (daily).
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/pricing"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// Kinds of ingestion pass (also stored in ingest_run.kind).
const (
	KindAvailability = "availability"
	KindPrice        = "price"
)

// Engine runs ingestion against the store.
type Engine struct {
	Store   *store.Store
	Log     *slog.Logger
	Vehicle pricing.Vehicle // reference car for the comparable session prices
	Limit   int             // max concurrent sources; 0 -> NumCPU

	// OnRun, if set, is called after each pass for metrics. Safe for nil.
	OnRun func(cpoID, kind string, rowsSeen, changes int, dur time.Duration, err error)
}

func NewEngine(st *store.Store, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{Store: st, Log: log, Vehicle: pricing.DefaultVehicle}
}

// RunAll runs a full price pass (which also refreshes availability) for every
// source concurrently (bounded). Used by the one-shot `-once` mode. A single
// source failure is logged and recorded, never aborting the others.
func (e *Engine) RunAll(ctx context.Context, sources []source.Source) error {
	limit := e.Limit
	if limit <= 0 {
		limit = runtime.NumCPU()
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for _, src := range sources {
		g.Go(func() error {
			if !src.Ready() {
				e.Log.Warn("skipping source without token", "cpo", src.CPO.ID)
				return nil
			}
			return e.RunPrices(ctx, src)
		})
	}
	return g.Wait()
}

// RunAvailability refreshes connector availability only (light path).
func (e *Engine) RunAvailability(ctx context.Context, src source.Source) error {
	return e.recordRun(ctx, src.CPO.ID, KindAvailability, func() (int, int, error) {
		conns, err := feedFor(src).Availability(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("availability %s: %w", src.CPO.ID, err)
		}
		for _, conn := range conns {
			if _, err := e.upsertConnector(ctx, conn); err != nil {
				e.Log.Error("upsert connector", "cpo", src.CPO.ID,
					"evse", conn.EVSEUID, "connector", conn.ConnectorID, "err", err)
			}
		}
		return len(conns), 0, nil
	})
}

// RunPrices runs a full pass: refresh identity/availability and apply tariff
// change detection.
func (e *Engine) RunPrices(ctx context.Context, src source.Source) error {
	return e.recordRun(ctx, src.CPO.ID, KindPrice, func() (int, int, error) {
		conns, tariffs, err := feedFor(src).Full(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("full pass %s: %w", src.CPO.ID, err)
		}
		changes := 0
		for _, conn := range conns {
			id, err := e.upsertConnector(ctx, conn)
			if err != nil {
				e.Log.Error("upsert connector", "cpo", src.CPO.ID,
					"evse", conn.EVSEUID, "connector", conn.ConnectorID, "err", err)
				continue
			}
			ch, err := e.processTariff(ctx, id, conn, tariffs)
			if err != nil {
				e.Log.Error("process tariff", "cpo", src.CPO.ID,
					"evse", conn.EVSEUID, "connector", conn.ConnectorID, "err", err)
				continue
			}
			if ch {
				changes++
			}
		}
		return len(conns), changes, nil
	})
}

// recordRun wraps a pass with run-log bookkeeping, logging, and the metrics hook.
func (e *Engine) recordRun(ctx context.Context, cpoID, kind string, fn func() (rowsSeen, changes int, err error)) (err error) {
	start := time.Now()
	runID, startErr := e.Store.StartRun(ctx, cpoID, kind)
	if startErr != nil {
		return fmt.Errorf("start run %s/%s: %w", cpoID, kind, startErr)
	}
	rowsSeen, changes, err := fn()
	if ferr := e.Store.FinishRun(ctx, runID, rowsSeen, changes, err); ferr != nil {
		e.Log.Error("finish run", "cpo", cpoID, "kind", kind, "err", ferr)
	}
	dur := time.Since(start)
	e.Log.Info("ingest pass complete", "cpo", cpoID, "kind", kind,
		"connectors", rowsSeen, "tariff_changes", changes, "dur", dur, "err", err)
	if e.OnRun != nil {
		e.OnRun(cpoID, kind, rowsSeen, changes, dur, err)
	}
	return err
}

// upsertConnector refreshes a connector's identity and current availability.
func (e *Engine) upsertConnector(ctx context.Context, conn model.Connector) (int64, error) {
	id, err := e.Store.UpsertCharger(ctx, conn)
	if err != nil {
		return 0, fmt.Errorf("upsert charger: %w", err)
	}
	avail := 0
	if conn.Available() {
		avail = 1
	}
	if err := e.Store.UpsertStatus(ctx, id, conn.EVSEStatus, avail); err != nil {
		return 0, fmt.Errorf("upsert status: %w", err)
	}
	return id, nil
}

// RecordLive persists a single charger's live reading (status + optional
// tariff) using the same SCD2 change-detection as the crawlers. The on-demand
// live-status endpoint uses it so a live lookup enriches stored history AND
// bumps charger_status.updated_at — which the crawl orders by, so a
// just-refreshed EVSE is naturally deprioritized rather than re-polled.
func (e *Engine) RecordLive(ctx context.Context, chargerID int64, conn model.Connector, status string, tar *model.Tariff) error {
	avail := 0
	if status == "AVAILABLE" {
		avail = 1
	}
	if err := e.Store.UpsertStatus(ctx, chargerID, status, avail); err != nil {
		return err
	}
	if tar != nil {
		if _, err := e.processTariff(ctx, chargerID, conn, map[string]model.Tariff{conn.TariffID: *tar}); err != nil {
			return err
		}
	}
	return nil
}

// processTariff applies SCD2 change detection for one connector's tariff.
// It returns whether a new tariff version was recorded.
//
// Honesty: a missing tariff_id (or a tariff absent from this feed) is treated
// as "unknown" and leaves history untouched — we do NOT close the open version,
// since a transient feed gap must not look like a price withdrawal.
func (e *Engine) processTariff(ctx context.Context, id int64, conn model.Connector, tariffs map[string]model.Tariff) (bool, error) {
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

	// Headline (default sort) + the per-session comparison matrix.
	var comparable *float64
	if c, ok := pricing.Headline(tar, conn.PowerKW, conn.CurrentType, e.Vehicle); ok {
		comparable = &c
	}
	pricesJSON, err := json.Marshal(pricing.AllPrices(tar, conn.PowerKW, conn.CurrentType, e.Vehicle))
	if err != nil {
		return false, fmt.Errorf("marshal prices: %w", err)
	}

	var srcUpdated *time.Time
	if !tar.LastUpdated.IsZero() {
		t := tar.LastUpdated
		srcUpdated = &t
	}

	if err := e.Store.ReplaceTariff(ctx, id, store.TariffWrite{
		Hash:              newHash,
		Components:        components,
		Comparable:        comparable,
		Prices:            pricesJSON,
		Currency:          tar.Currency,
		SourceLastUpdated: srcUpdated,
	}); err != nil {
		return false, fmt.Errorf("replace tariff: %w", err)
	}
	return true, nil
}
