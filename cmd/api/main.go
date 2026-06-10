// Command api serves the public-facing endpoints: find the cheapest available
// charger nearby, and read a charger's ad-hoc price history.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/appmire/charging/internal/config"
	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/metrics"
	"github.com/appmire/charging/internal/pricing"
	"github.com/appmire/charging/internal/store"
)

type server struct {
	st              *store.Store
	log             *slog.Logger
	vehicle         pricing.Vehicle
	staleAfter      time.Duration
	priceStaleAfter time.Duration
	adminToken      string
	engine          *ingest.Engine
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local /healthz endpoint and exit (for container healthchecks)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	if *healthcheck {
		os.Exit(runHealthcheck(cfg.APIAddr))
	}

	st, err := store.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Error("connect db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	s := &server{
		st:  st,
		log: log,
		vehicle: pricing.Vehicle{
			UsableKWh:         cfg.VehicleUsableKWh,
			ConsumptionKWh100: cfg.VehicleConsumption,
		},
		staleAfter:      cfg.AvailabilityStaleAfter,
		priceStaleAfter: cfg.PriceStaleAfter,
		adminToken:      cfg.AdminToken,
	}
	s.engine = ingest.NewEngine(st, log)
	s.engine.Vehicle = s.vehicle

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           s.routes(cfg.CORSOrigins),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("api listening", "addr", cfg.APIAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// routes builds the HTTP handler (read API + protected admin control plane).
func (s *server) routes(corsOrigins string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(corsMiddleware(corsOrigins))
	r.Get("/healthz", s.health)
	r.Get("/readyz", s.ready)
	r.Handle("/metrics", metrics.Handler())
	r.Get("/sessions", s.sessions)
	r.Get("/chargers/cheapest", s.cheapest)
	r.Get("/chargers/{id}/price-history", s.priceHistory)
	r.Get("/stats/overview", s.statsOverview)
	r.Get("/stats/sessions", s.statsSessions)
	r.Get("/stats/regions", s.statsRegions)
	r.Get("/stats/price-trend", s.statsPriceTrend)

	// Admin (control plane) — protected by ADMIN_TOKEN bearer.
	r.Route("/admin", func(ar chi.Router) {
		ar.Use(s.adminAuth)
		ar.Get("/sources", s.adminListSources)
		ar.Post("/sources", s.adminUpsertSource)
		ar.Delete("/sources/{id}", s.adminDeleteSource)
		ar.Post("/sources/{id}/enable", s.adminEnable(true))
		ar.Post("/sources/{id}/disable", s.adminEnable(false))
		ar.Put("/sources/{id}/token", s.adminSetToken)
		ar.Post("/ingest/{id}/run", s.adminRunIngest)
		ar.Get("/runs", s.adminRuns)
	})
	return r
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Pool.Ping(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /readyz — ready only if the DB is reachable and every enabled source has
// produced a recent successful availability and price ingest. Returns 503 with
// per-source detail otherwise. (No enabled sources => ready, with a note.)
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.st.Pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false, "reason": "database unavailable"})
		return
	}
	cpos, err := s.st.ListEnabledCPOs(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false, "reason": "cannot list sources"})
		return
	}

	availWindow := 2 * s.staleAfter
	sources := []map[string]any{}
	ready := true
	for _, c := range cpos {
		a := s.freshness(ctx, c.ID, ingest.KindAvailability, availWindow)
		p := s.freshness(ctx, c.ID, ingest.KindPrice, s.priceStaleAfter)
		if !a.OK || !p.OK {
			ready = false
		}
		sources = append(sources, map[string]any{"cpo": c.ID, "availability": a, "price": p})
	}

	code := http.StatusOK
	if !ready {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"ready":          ready,
		"enabled_source": len(cpos),
		"sources":        sources,
	})
}

type freshness struct {
	OK     bool       `json:"ok"`
	LastAt *time.Time `json:"last_success_at"`
}

// freshness reports whether the last successful run of kind is within window.
// A zero window disables the check (always ok).
func (s *server) freshness(ctx context.Context, cpoID, kind string, window time.Duration) freshness {
	t, found, err := s.st.LastSuccess(ctx, cpoID, kind)
	if err != nil || !found {
		return freshness{OK: window <= 0}
	}
	ok := window <= 0 || time.Since(t) <= window
	return freshness{OK: ok, LastAt: &t}
}

// GET /stats/overview — market counts + headline-price aggregates by current type.
func (s *server) statsOverview(w http.ResponseWriter, r *http.Request) {
	o, err := s.st.Overview(r.Context())
	if err != nil {
		s.log.Error("stats overview", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// GET /stats/sessions — avg/min/max price per comparison-session profile.
func (s *server) statsSessions(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.SessionStats(r.Context())
	if err != nil {
		s.log.Error("stats sessions", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if st == nil {
		st = []store.SessionStat{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": st})
}

// GET /stats/regions?by=city|postal&limit=
func (s *server) statsRegions(w http.ResponseWriter, r *http.Request) {
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "city"
	}
	if by != "city" && by != "postal" {
		writeErr(w, http.StatusBadRequest, "by must be 'city' or 'postal'")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := s.st.RegionStats(r.Context(), by, limit)
	if err != nil {
		s.log.Error("stats regions", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if res == nil {
		res = []store.PriceAgg{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"by": by, "regions": res})
}

// GET /stats/price-trend?months=
func (s *server) statsPriceTrend(w http.ResponseWriter, r *http.Request) {
	months, _ := strconv.Atoi(r.URL.Query().Get("months"))
	res, err := s.st.PriceTrend(r.Context(), months)
	if err != nil {
		s.log.Error("stats price-trend", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if res == nil {
		res = []store.TrendPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"trend": res})
}

// GET /sessions — list the comparison session profiles for the reference vehicle.
func (s *server) sessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"vehicle":  s.vehicle,
		"sessions": pricing.Profiles(s.vehicle),
	})
}

// GET /chargers/cheapest?lat=&lon=&radius=&min_power=&plug=&available=&session=&limit=
func (s *server) cheapest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, okLat := parseFloat(q.Get("lat"))
	lon, okLon := parseFloat(q.Get("lon"))
	if !okLat || !okLon {
		writeErr(w, http.StatusBadRequest, "lat and lon are required floats")
		return
	}
	radius, ok := parseFloat(q.Get("radius"))
	if !ok || radius <= 0 {
		radius = 5000 // default 5 km
	}
	minPower, _ := parseFloat(q.Get("min_power"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	session := q.Get("session")
	if session != "" && !pricing.IsProfile(session) {
		writeErr(w, http.StatusBadRequest, "unknown session profile; see GET /sessions")
		return
	}
	nq := store.NearbyQuery{
		Lat: lat, Lon: lon, RadiusM: radius,
		MinPowerKW: minPower,
		PlugType:   q.Get("plug"),
		OnlyAvail:  q.Get("available") == "true" || q.Get("available") == "1",
		Session:    session,
		StaleAfter: s.staleAfter,
		Limit:      limit,
	}
	res, err := s.st.CheapestNearby(r.Context(), nq)
	if err != nil {
		s.log.Error("cheapest query", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if res == nil {
		res = []store.NearbyCharger{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   nq,
		"count":   len(res),
		"results": res,
	})
}

// GET /chargers/{id}/price-history
func (s *server) priceHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid charger id")
		return
	}
	hist, err := s.st.PriceHistory(r.Context(), id)
	if err != nil {
		s.log.Error("price history", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if hist == nil {
		hist = []store.PricePoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"charger_id": id,
		"history":    hist,
	})
}

// runHealthcheck probes the local /healthz endpoint; used by the container
// healthcheck since the distroless image has no shell or curl.
func runHealthcheck(addr string) int {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// corsMiddleware allows browser clients on other origins to call the read API.
// origins is comma-separated; "*" allows any. Echoes a matching Origin so that
// credentialed requests still work if locked down later.
func corsMiddleware(origins string) func(http.Handler) http.Handler {
	allowAny := strings.TrimSpace(origins) == "*" || origins == ""
	allowed := map[string]bool{}
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			switch {
			case allowAny:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case origin != "" && allowed[origin]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "300")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parseFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
