package ingest

import (
	"context"
	"strings"
	"time"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/monta"
	"github.com/appmire/charging/internal/source"
)

// RunMontaCrawl continuously refreshes per-EVSE availability + ad-hoc price for
// a Monta source, paced by the client's rate limiter. Monta has no bulk status
// endpoint and a 100 req/10 min limit, so we cycle through the already-ingested
// Monta chargers (least-recently-statused first), writing availability every
// time and a new SCD2 tariff version only when the price changes. A full cycle
// of all Monta EVSEs takes a few hours — fine for the rarely-changing price.
//
// It runs until ctx is cancelled. The crawl uses ~70% of the rate budget,
// leaving headroom for the on-demand live-status lookups (same credential).
func (e *Engine) RunMontaCrawl(ctx context.Context, src source.Source) {
	id, secret, _ := strings.Cut(src.Token, ":")
	client := monta.New(id, secret)
	client.SetLimit(9*time.Second, 60) // ~66 calls / 10 min; leaves headroom

	e.Log.Info("monta crawl started", "cpo", src.CPO.ID)
	for {
		if ctx.Err() != nil {
			return
		}
		targets, err := e.Store.ChargersToRefresh(ctx, src.CPO.ID, 100)
		if err != nil {
			e.Log.Error("monta crawl: list", "cpo", src.CPO.ID, "err", err)
			if !sleepCtx(ctx, time.Minute) {
				return
			}
			continue
		}
		statusable := 0
		for _, t := range targets {
			if ctx.Err() != nil {
				return
			}
			if !monta.IsMonta(t.EVSEUID) {
				// No status endpoint for roaming EVSEs; mark once so the cycle
				// doesn't keep re-selecting them.
				_ = e.Store.UpsertStatus(ctx, t.ID, "UNKNOWN", 0)
				continue
			}
			statusable++
			status, tar, serr := client.Status(ctx, t.EVSEUID) // rate-limited
			if serr != nil {
				e.Log.Warn("monta crawl: status", "evse", t.EVSEUID, "err", serr)
				_ = e.Store.UpsertStatus(ctx, t.ID, "UNKNOWN", 0)
				continue
			}
			avail := 0
			if status == "AVAILABLE" {
				avail = 1
			}
			if err := e.Store.UpsertStatus(ctx, t.ID, status, avail); err != nil {
				e.Log.Error("monta crawl: status upsert", "err", err)
			}
			if tar != nil {
				conn := model.Connector{
					CPOID: src.CPO.ID, EVSEUID: t.EVSEUID,
					PowerKW: t.PowerKW, CurrentType: t.CurrentType, TariffID: t.EVSEUID,
				}
				if _, err := e.processTariff(ctx, t.ID, conn, map[string]model.Tariff{t.EVSEUID: *tar}); err != nil {
					e.Log.Error("monta crawl: tariff", "evse", t.EVSEUID, "err", err)
				}
			}
		}
		if statusable == 0 {
			// Nothing to price yet (locations not ingested, or all non-Monta) —
			// back off rather than spin.
			if !sleepCtx(ctx, 5*time.Minute) {
				return
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
