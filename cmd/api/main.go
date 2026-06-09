// Command api serves the public-facing endpoints: find the cheapest available
// charger nearby, and read a charger's ad-hoc price history.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/appmire/charging/internal/config"
	"github.com/appmire/charging/internal/store"
)

type server struct {
	st  *store.Store
	log *slog.Logger
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	st, err := store.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Error("connect db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	s := &server{st: st, log: log}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Get("/healthz", s.health)
	r.Get("/chargers/cheapest", s.cheapest)
	r.Get("/chargers/{id}/price-history", s.priceHistory)

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("api listening", "addr", cfg.APIAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Pool.Ping(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /chargers/cheapest?lat=&lon=&radius=&min_power=&plug=&available=&limit=
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

	nq := store.NearbyQuery{
		Lat: lat, Lon: lon, RadiusM: radius,
		MinPowerKW: minPower,
		PlugType:   q.Get("plug"),
		OnlyAvail:  q.Get("available") == "true" || q.Get("available") == "1",
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
