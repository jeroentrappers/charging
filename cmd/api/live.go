package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/appmire/charging/internal/monta"
	"github.com/appmire/charging/internal/pricing"
	"github.com/appmire/charging/internal/store"
)

// liveService answers on-demand "is this charger free right now?" queries by
// hitting Monta's per-EVSE status API, with a short cache and its own rate
// limiter so it stays within the shared Monta throttle (the background crawl
// uses most of the budget; this leaves headroom).
type liveService struct {
	client  *monta.Client // nil when Monta creds aren't configured
	vehicle pricing.Vehicle
	log     *slog.Logger
	ttl     time.Duration

	mu    sync.Mutex
	cache map[int64]liveEntry
}

type liveEntry struct {
	at        time.Time
	status    string
	available bool
	priceEUR  *float64
	currency  string
}

func newLiveService(client *monta.Client, vehicle pricing.Vehicle, log *slog.Logger) *liveService {
	if client != nil {
		// On-demand budget: ~burst 3 then 1 per 30s (~20 calls/10 min), leaving
		// the bulk of Monta's 100/10min for the background crawl.
		client.SetLimit(30*time.Second, 3)
	}
	return &liveService{client: client, vehicle: vehicle, log: log, ttl: 90 * time.Second, cache: map[int64]liveEntry{}}
}

type liveOut struct {
	Body struct {
		ID        int64     `json:"id"`
		Source    string    `json:"source"` // live | cached | unavailable
		Status    string    `json:"status"`
		Available bool      `json:"available"`
		CheckedAt time.Time `json:"checked_at"`
		PriceEUR  *float64  `json:"headline_price_eur,omitempty"`
		Currency  string    `json:"currency,omitempty"`
	}
}

// opChargerLive returns the freshest availability for a charger: a live Monta
// lookup when possible (cached briefly), otherwise the last stored status.
func (s *server) opChargerLive(ctx context.Context, in *historyIn) (*liveOut, error) {
	ref, found, err := s.st.GetLiveRef(ctx, in.ID)
	if err != nil {
		s.log.Error("live: lookup", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	if !found {
		return nil, huma.Error404NotFound("charger not found")
	}

	out := &liveOut{}
	out.Body.ID = ref.ID

	// Live path: Monta EVSE + a configured client.
	if s.live != nil && s.live.client != nil && monta.IsMonta(ref.EVSEUID) {
		if e, ok := s.live.fetch(ctx, ref); ok {
			out.Body.Source = "live"
			out.Body.Status = e.status
			out.Body.Available = e.available
			out.Body.CheckedAt = e.at
			out.Body.PriceEUR = e.priceEUR
			out.Body.Currency = e.currency
			return out, nil
		}
	}

	// Fallback: last stored status.
	if ref.StatusAt == nil {
		out.Body.Source = "unavailable"
		out.Body.Status = "UNKNOWN"
		return out, nil
	}
	out.Body.Source = "cached"
	out.Body.Status = ref.Status
	out.Body.Available = ref.AvailableCount > 0
	out.Body.CheckedAt = *ref.StatusAt
	return out, nil
}

// fetch returns a cached or freshly-fetched live entry. It never blocks long:
// the upstream call is bounded by a short context so a saturated rate limiter
// degrades to the stored status rather than hanging the request.
func (s *liveService) fetch(ctx context.Context, ref store.LiveRef) (liveEntry, bool) {
	now := time.Now().UTC()

	s.mu.Lock()
	if e, ok := s.cache[ref.ID]; ok && now.Sub(e.at) < s.ttl {
		s.mu.Unlock()
		return e, true
	}
	s.mu.Unlock()

	fctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	status, tariff, err := s.client.Status(fctx, ref.EVSEUID)
	if err != nil {
		s.log.Warn("live: monta status", "evse", ref.EVSEUID, "err", err)
		return liveEntry{}, false
	}

	e := liveEntry{at: now, status: status, available: status == "AVAILABLE"}
	if tariff != nil {
		e.currency = tariff.Currency
		if p, ok := pricing.HeadlineAt(*tariff, ref.PowerKW, ref.CurrentType, s.vehicle, now); ok {
			e.priceEUR = &p
		}
	}
	s.mu.Lock()
	s.cache[ref.ID] = e
	s.mu.Unlock()
	return e, true
}

// montaCreds resolves the Monta API credentials for the on-demand client: the
// DB-stored token for the "monta" source if present, else the MONTA_CREDS env.
func montaCreds(ctx context.Context, st *store.Store) string {
	if c, found, err := st.GetCPO(ctx, "monta"); err == nil && found && c.Token != "" {
		return c.Token
	}
	return strings.TrimSpace(os.Getenv("MONTA_CREDS"))
}
