package main

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/pricing"
	"github.com/appmire/charging/internal/report"
	"github.com/appmire/charging/internal/store"
)

// registerPublic wires the read API onto the OpenAPI document.
func (s *server) registerPublic(api huma.API) {
	huma.Get(api, "/sessions", s.opSessions, tag("Comparison"),
		summary("List comparison session profiles"))
	huma.Get(api, "/chargers/cheapest", s.opCheapest, tag("Chargers"),
		summary("Find the cheapest chargers nearby (server-ranked)"))
	huma.Get(api, "/chargers/nearby", s.opNearby, tag("Chargers"),
		summary("Nearest chargers with structured tariffs (for client-side pricing/ranking)"))
	huma.Get(api, "/chargers/{id}", s.opGetCharger, tag("Chargers"),
		summary("Get a charger by id (for shareable deep links)"))
	huma.Get(api, "/chargers/{id}/price-history", s.opPriceHistory, tag("Chargers"),
		summary("Ad-hoc price history for a charger"))
	huma.Get(api, "/chargers/{id}/live", s.opChargerLive, tag("Chargers"),
		summary("Live availability for a charger (on-demand Monta lookup)"))
	huma.Get(api, "/stats/overview", s.opStatsOverview, tag("Statistics"),
		summary("Market counts and headline-price aggregates"))
	huma.Get(api, "/stats/sessions", s.opStatsSessions, tag("Statistics"),
		summary("Price aggregates per comparison session"))
	huma.Get(api, "/stats/regions", s.opStatsRegions, tag("Statistics"),
		summary("Price aggregates by city or postal code"))
	huma.Get(api, "/stats/price-trend", s.opStatsTrend, tag("Statistics"),
		summary("Monthly headline-price trend"))
}

func tag(t string) func(*huma.Operation) {
	return func(o *huma.Operation) { o.Tags = append(o.Tags, t) }
}
func summary(s string) func(*huma.Operation) { return func(o *huma.Operation) { o.Summary = s } }

// ---- GET /sessions ----

type sessionsOut struct {
	Body struct {
		Vehicle  pricing.Vehicle           `json:"vehicle"`
		Sessions []pricing.ResolvedProfile `json:"sessions"`
	}
}

func (s *server) opSessions(_ context.Context, _ *struct{}) (*sessionsOut, error) {
	out := &sessionsOut{}
	out.Body.Vehicle = s.vehicle
	out.Body.Sessions = pricing.Profiles(s.vehicle)
	return out, nil
}

// ---- GET /chargers/cheapest ----

type cheapestIn struct {
	Lat            float64 `query:"lat" required:"true" doc:"Origin latitude"`
	Lon            float64 `query:"lon" required:"true" doc:"Origin longitude"`
	Radius         float64 `query:"radius" default:"5000" doc:"Search radius in metres"`
	MinPower       float64 `query:"min_power" doc:"Only chargers rated at least this many kW"`
	Plug           string  `query:"plug" doc:"OCPI connector standard, e.g. IEC_62196_T2_COMBO"`
	Available      bool    `query:"available" doc:"Only chargers currently reported free"`
	IncludePrivate bool    `query:"include_private" doc:"Include private (home / peer-to-peer) chargers"`
	Session        string  `query:"session" doc:"Standard comparison profile key (see GET /sessions)"`
	EnergyKWh      float64 `query:"energy_kwh" doc:"Custom session: energy to add to the battery (kWh, 1-250). Overrides 'session'."`
	PowerKW        float64 `query:"power_kw" doc:"Custom session: target power (kW, 1-400). Omit (or 0) for as-fast-as-possible."`
	// Car parameters for the price calc (0 = server default vehicle).
	UsableKWh   float64 `query:"usable_kwh" doc:"Car usable battery (kWh) for the price calc"`
	Consumption float64 `query:"consumption_kwh100" doc:"Car consumption (kWh/100km)"`
	// Detour weighting: rank by charging price PLUS the estimated round-trip
	// detour cost (extra energy + time to drive there and back).
	Detour      bool    `query:"detour" doc:"Add an estimated round-trip detour cost to the ranking"`
	DetourPrice float64 `query:"detour_price" doc:"Reference energy price (€/kWh) for detour energy; 0 = 0.30"`
	DetourPerH  float64 `query:"detour_eur_per_h" doc:"Value of time (€/h) for detour driving time; 0 = ignore time"`
	Limit       int     `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Maximum results to return"`
}

type cheapestOut struct {
	Body struct {
		Query   map[string]any        `json:"query"`
		Count   int                   `json:"count"`
		Results []store.NearbyCharger `json:"results"`
	}
}

func (s *server) opCheapest(ctx context.Context, in *cheapestIn) (*cheapestOut, error) {
	// Pricing mode. A user-defined session (energy_kwh, optional power_kw) takes
	// precedence over a named comparison profile (session).
	spec := priceSpec{session: in.Session}
	if in.EnergyKWh > 0 {
		if in.EnergyKWh > 250 {
			return nil, huma.Error400BadRequest("energy_kwh must be between 1 and 250")
		}
		if in.PowerKW < 0 || in.PowerKW > 400 {
			return nil, huma.Error400BadRequest("power_kw must be between 1 and 400 (omit for as-fast-as-possible)")
		}
		spec.custom = &pricing.CustomSession{BatteryKWh: in.EnergyKWh, PowerKW: in.PowerKW}
		spec.session = "" // custom wins
	} else if spec.session != "" && !pricing.IsProfile(spec.session) {
		return nil, huma.Error400BadRequest("unknown session profile; see GET /sessions")
	}

	radius := in.Radius
	if radius <= 0 {
		radius = 5000
	}
	vehicle := s.vehicle
	if in.UsableKWh > 0 {
		vehicle.UsableKWh = in.UsableKWh
	}
	if in.Consumption > 0 {
		vehicle.ConsumptionKWh100 = in.Consumption
	}

	// Pull a generous pool of the nearest candidates, then rank by the weighted
	// price+detour cost and keep the best `Limit`. Detour grows with distance, so
	// the optimum is always near — the nearest pool is more than enough.
	pool := 8 * in.Limit
	if pool < 200 {
		pool = 200
	}
	nq := store.NearbyQuery{
		Lat: in.Lat, Lon: in.Lon, RadiusM: radius,
		MinPowerKW:     in.MinPower,
		PlugType:       in.Plug,
		OnlyAvail:      in.Available,
		IncludePrivate: in.IncludePrivate,
		Session:        spec.session,
		StaleAfter:     s.staleAfter,
		Limit:          pool,
	}
	res, err := s.st.CheapestNearby(ctx, nq)
	if err != nil {
		s.log.Error("cheapest query", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}

	// Re-price each candidate at the *current* local time (so time-of-day tariffs
	// rank correctly) for the user's car.
	now := time.Now()
	if brussels != nil {
		now = now.In(brussels)
	}
	for i := range res {
		s.repriceNow(&res[i], spec, vehicle, now)
	}

	s.attachReports(ctx, res)

	// Estimated round-trip detour cost (extra energy + time), added to the price
	// for ranking when requested.
	if in.Detour {
		refPrice := in.DetourPrice
		if refPrice <= 0 {
			refPrice = 0.30
		}
		perH := in.DetourPerH
		if perH < 0 {
			perH = 0
		}
		for i := range res {
			roundTripKm := 2 * res[i].DistanceM / 1000
			energy := vehicle.ConsumptionKWh100 * roundTripKm / 100 * refPrice
			timeCost := roundTripKm / 50.0 * perH // ~50 km/h detour driving
			d := energy + timeCost
			res[i].DetourEUR = &d
		}
	}

	sortByWeighted(res, spec.selecting())
	if len(res) > in.Limit {
		res = res[:in.Limit]
	}
	if res == nil {
		res = []store.NearbyCharger{}
	}
	for i := range res {
		res[i].Components = nil // this endpoint already priced server-side; keep it lean
	}

	out := &cheapestOut{}
	out.Body.Query = queryEcho(nq, in.Limit, spec)
	out.Body.Count = len(res)
	out.Body.Results = res
	return out, nil
}

// ---- GET /chargers/nearby (geo-only; the client prices + ranks) ----

type nearbyIn struct {
	Lat            float64 `query:"lat" required:"true" doc:"Origin latitude"`
	Lon            float64 `query:"lon" required:"true" doc:"Origin longitude"`
	Radius         float64 `query:"radius" default:"50000" doc:"Search radius in metres"`
	MinPower       float64 `query:"min_power" doc:"Only chargers rated at least this many kW"`
	Plug           string  `query:"plug" doc:"OCPI connector standard"`
	Available      bool    `query:"available" doc:"Only chargers currently reported free"`
	IncludePrivate bool    `query:"include_private" doc:"Include private (home / peer-to-peer) chargers"`
	Limit          int     `query:"limit" default:"200" minimum:"1" maximum:"500" doc:"Maximum candidates to return"`
}

type nearbyOut struct {
	Body struct {
		Count   int                   `json:"count"`
		Results []store.NearbyCharger `json:"results"`
	}
}

// opNearby returns the nearest candidates (distance order) with their structured
// tariff + community reports, so the client can compute per-car, time-of-day
// prices + detour and rank locally — no server-side pricing.
func (s *server) opNearby(ctx context.Context, in *nearbyIn) (*nearbyOut, error) {
	radius := in.Radius
	if radius <= 0 {
		radius = 50000
	}
	res, err := s.st.CheapestNearby(ctx, store.NearbyQuery{
		Lat: in.Lat, Lon: in.Lon, RadiusM: radius,
		MinPowerKW: in.MinPower, PlugType: in.Plug, OnlyAvail: in.Available,
		IncludePrivate: in.IncludePrivate, StaleAfter: s.staleAfter, Limit: in.Limit,
	})
	if err != nil {
		s.log.Error("nearby query", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	s.attachReports(ctx, res)
	if res == nil {
		res = []store.NearbyCharger{}
	}
	out := &nearbyOut{}
	out.Body.Count = len(res)
	out.Body.Results = res
	return out, nil
}

// attachReports fills each candidate's active community reports + avoid flag.
func (s *server) attachReports(ctx context.Context, res []store.NearbyCharger) {
	ids := make([]int64, len(res))
	for i := range res {
		ids[i] = res[i].ID
	}
	reps, err := s.st.ReportsForIDs(ctx, ids)
	if err != nil {
		s.log.Warn("attach reports", "err", err)
		return
	}
	if len(reps) == 0 {
		return
	}
	now := time.Now().UTC()
	for i := range res {
		if raws := reps[res[i].ID]; len(raws) > 0 {
			aggs := report.Aggregate(now, raws)
			res[i].Reports = aggs
			res[i].Avoid = report.Avoid(aggs)
		}
	}
}

// priceSpec selects how each candidate's effective price is computed: a named
// comparison profile, a user-defined custom session (which takes precedence),
// or neither (headline price for the default sort).
type priceSpec struct {
	session string                 // named profile key
	custom  *pricing.CustomSession // ad-hoc session; wins over session
}

// selecting reports whether a specific session (named or custom) was requested,
// i.e. whether the effective price lives in SessionPrice rather than PriceEUR.
func (p priceSpec) selecting() bool { return p.custom != nil || p.session != "" }

// queryEcho is the request summary returned to the client (with the user's
// requested display limit, not the inflated candidate pool).
func queryEcho(nq store.NearbyQuery, limit int, spec priceSpec) map[string]any {
	q := map[string]any{
		"lat": nq.Lat, "lon": nq.Lon, "radius": nq.RadiusM,
		"min_power": nq.MinPowerKW, "plug": nq.PlugType,
		"available": nq.OnlyAvail, "limit": limit,
	}
	if spec.custom != nil {
		q["custom_session"] = spec.custom
	} else if spec.session != "" {
		q["session"] = spec.session
	}
	return q
}

var brussels, _ = time.LoadLocation("Europe/Brussels")

// repriceNow overrides a candidate's headline (and selected-session) price with
// the value evaluated at `now` for the given vehicle, when the structured tariff
// is available.
func (s *server) repriceNow(c *store.NearbyCharger, spec priceSpec, v pricing.Vehicle, now time.Time) {
	if len(c.Components) == 0 {
		return // no structured tariff (e.g. Monta snapshot) — keep stored value
	}
	var tar model.Tariff
	if err := json.Unmarshal(c.Components, &tar); err != nil {
		return
	}
	if p, ok := pricing.HeadlineAt(tar, c.PowerKW, c.CurrentType, v, now); ok {
		c.PriceEUR = &p
	}
	switch {
	case spec.custom != nil:
		if p, ok := pricing.CustomPriceAt(tar, c.PowerKW, c.CurrentType, *spec.custom, v, now); ok {
			c.SessionPrice = &p
		} else {
			c.SessionPrice = nil
		}
	case spec.session != "":
		if p, ok := pricing.SessionPriceAt(tar, c.PowerKW, c.CurrentType, spec.session, v, now); ok {
			c.SessionPrice = &p
		} else {
			c.SessionPrice = nil
		}
	}
}

// weighted is the ranking cost: the effective charging price (session price when
// a session is selected, else headline) plus any estimated detour cost. Nil when
// the charger has no priceable charging cost.
func weighted(c store.NearbyCharger, selecting bool) *float64 {
	base := c.PriceEUR
	if selecting {
		base = c.SessionPrice
	}
	if base == nil {
		return nil
	}
	v := *base
	if c.DetourEUR != nil {
		v += *c.DetourEUR
	}
	return &v
}

// sortByWeighted ranks by the weighted price+detour cost (nulls last), with
// corroborated-bad chargers sunk to the bottom (never hidden), then distance.
func sortByWeighted(res []store.NearbyCharger, selecting bool) {
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Avoid != res[j].Avoid {
			return !res[i].Avoid
		}
		wi, wj := weighted(res[i], selecting), weighted(res[j], selecting)
		if (wi == nil) != (wj == nil) {
			return wi != nil // priced before unpriced
		}
		if wi != nil && *wi != *wj {
			return *wi < *wj
		}
		return res[i].DistanceM < res[j].DistanceM
	})
}

// ---- GET /chargers/{id} ----

type chargerIn struct {
	ID          int64   `path:"id"`
	Lat         float64 `query:"lat" doc:"Optional origin latitude (for distance)"`
	Lon         float64 `query:"lon" doc:"Optional origin longitude (for distance)"`
	UsableKWh   float64 `query:"usable_kwh" doc:"Car usable battery (kWh) for the price calc"`
	Consumption float64 `query:"consumption_kwh100" doc:"Car consumption (kWh/100km)"`
}

type chargerOut struct {
	Body store.NearbyCharger
}

func (s *server) opGetCharger(ctx context.Context, in *chargerIn) (*chargerOut, error) {
	c, found, err := s.st.GetCharger(ctx, in.ID, in.Lat, in.Lon, s.staleAfter)
	if err != nil {
		s.log.Error("get charger", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	if !found {
		return nil, huma.Error404NotFound("charger not found")
	}
	now := time.Now()
	if brussels != nil {
		now = now.In(brussels)
	}
	vehicle := s.vehicle
	if in.UsableKWh > 0 {
		vehicle.UsableKWh = in.UsableKWh
	}
	if in.Consumption > 0 {
		vehicle.ConsumptionKWh100 = in.Consumption
	}
	s.repriceNow(&c, priceSpec{}, vehicle, now) // headline price at the current time
	if raws, rerr := s.st.ReportsRaw(ctx, c.ID); rerr == nil {
		aggs := report.Aggregate(time.Now().UTC(), raws)
		c.Reports = aggs
		c.Avoid = report.Avoid(aggs)
	}
	return &chargerOut{Body: c}, nil
}

// ---- GET /chargers/{id}/price-history ----

type historyIn struct {
	ID int64 `path:"id" doc:"Charger id"`
}

type historyOut struct {
	Body struct {
		ChargerID int64              `json:"charger_id"`
		History   []store.PricePoint `json:"history"`
	}
}

func (s *server) opPriceHistory(ctx context.Context, in *historyIn) (*historyOut, error) {
	hist, err := s.st.PriceHistory(ctx, in.ID)
	if err != nil {
		s.log.Error("price history", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	if hist == nil {
		hist = []store.PricePoint{}
	}
	out := &historyOut{}
	out.Body.ChargerID = in.ID
	out.Body.History = hist
	return out, nil
}

// ---- GET /stats/* ----

type overviewOut struct{ Body store.Overview }

func (s *server) opStatsOverview(ctx context.Context, _ *struct{}) (*overviewOut, error) {
	o, err := s.st.Overview(ctx)
	if err != nil {
		s.log.Error("stats overview", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	return &overviewOut{Body: o}, nil
}

type sessionStatsOut struct {
	Body struct {
		Sessions []store.SessionStat `json:"sessions"`
	}
}

func (s *server) opStatsSessions(ctx context.Context, _ *struct{}) (*sessionStatsOut, error) {
	st, err := s.st.SessionStats(ctx)
	if err != nil {
		s.log.Error("stats sessions", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	if st == nil {
		st = []store.SessionStat{}
	}
	out := &sessionStatsOut{}
	out.Body.Sessions = st
	return out, nil
}

type regionsIn struct {
	By    string `query:"by" default:"city" enum:"city,postal" doc:"Group by city or postal code"`
	Limit int    `query:"limit" doc:"Maximum regions to return (0 = all)"`
}

type regionsOut struct {
	Body struct {
		By      string           `json:"by"`
		Regions []store.PriceAgg `json:"regions"`
	}
}

func (s *server) opStatsRegions(ctx context.Context, in *regionsIn) (*regionsOut, error) {
	res, err := s.st.RegionStats(ctx, in.By, in.Limit)
	if err != nil {
		s.log.Error("stats regions", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	if res == nil {
		res = []store.PriceAgg{}
	}
	out := &regionsOut{}
	out.Body.By = in.By
	out.Body.Regions = res
	return out, nil
}

type trendIn struct {
	Months int `query:"months" doc:"Number of months back (0 = default window)"`
}

type trendOut struct {
	Body struct {
		Trend []store.TrendPoint `json:"trend"`
	}
}

func (s *server) opStatsTrend(ctx context.Context, in *trendIn) (*trendOut, error) {
	res, err := s.st.PriceTrend(ctx, in.Months)
	if err != nil {
		s.log.Error("stats price-trend", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	if res == nil {
		res = []store.TrendPoint{}
	}
	out := &trendOut{}
	out.Body.Trend = res
	return out, nil
}
