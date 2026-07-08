package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// statusJSON serves per-source operational health as JSON: enabled/mode,
// charger/priced/available counts and the freshness of the newest status and
// price per source. The web app's /status page renders it, and it doubles as a
// stable machine-readable monitoring endpoint. Served at /api/status.
//
// Presentation (relative ages, freshness colouring, priced %, poll/push/off
// mode) is intentionally left to the client — this stays raw data.
func (s *server) statusJSON(w http.ResponseWriter, r *http.Request) {
	hs, err := s.st.SourceHealthAll(r.Context())
	if err != nil {
		http.Error(w, "cannot load source health", http.StatusServiceUnavailable)
		return
	}
	out := statusResponse{
		Generated: time.Now().UTC(),
		Sources:   make([]statusSource, 0, len(hs)),
	}
	for _, h := range hs {
		out.Sources = append(out.Sources, statusSource{
			ID:           h.ID,
			Name:         h.Name,
			Type:         h.SourceType,
			Country:      h.Country,
			Enabled:      h.Enabled,
			Chargers:     h.Chargers,
			Priced:       h.Priced,
			Available:    h.Available,
			NewestStatus: h.NewestStatus,
			NewestPrice:  h.NewestPrice,
			LastRunAt:    h.LastRunAt,
			LastRunError: h.LastRunError,
		})
		out.Totals.Sources++
		out.Totals.Chargers += h.Chargers
		out.Totals.Priced += h.Priced
		out.Totals.Available += h.Available
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// statusSource is one source's health row. Timestamps are null when the source
// has never reported that signal (e.g. a location-only register has no status).
type statusSource struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Country      string     `json:"country"`
	Enabled      bool       `json:"enabled"`
	Chargers     int        `json:"chargers"`
	Priced       int        `json:"priced"`
	Available    int        `json:"available"`
	NewestStatus *time.Time `json:"newest_status"`
	NewestPrice  *time.Time `json:"newest_price"`
	// Last finished ingest pass from the run log — non-null with an empty error
	// means the pipeline is healthy even when the source yields zero chargers.
	LastRunAt    *time.Time `json:"last_run_at"`
	LastRunError string     `json:"last_run_error"`
}

type statusResponse struct {
	Generated time.Time `json:"generated"`
	Totals    struct {
		Sources   int `json:"sources"`
		Chargers  int `json:"chargers"`
		Priced    int `json:"priced"`
		Available int `json:"available"`
	} `json:"totals"`
	Sources []statusSource `json:"sources"`
}
