package main

import (
	"encoding/json"
	"net/http"
)

// fxResponse publishes the exchange rates the client needs to compare
// foreign-currency tariffs with euro ones.
//
// The PWA prices chargers itself, from the structured tariff each source
// publishes in its own currency (Poland's EIPA quotes PLN). Without a rate the
// client would compare a PLN number against euros, so it fetches this once per
// session and scales. Rates are units of the currency per euro, exactly as the
// ECB publishes them, so a euro amount is the price divided by the rate.
type fxResponse struct {
	Base  string             `json:"base"`  // always EUR
	Date  string             `json:"date"`  // the ECB publication date (YYYY-MM-DD)
	Rates map[string]float64 `json:"rates"` // currency -> units per euro
}

// serveFX returns the current reference rates. It answers 503 when no usable set
// is available, which the client treats as "don't convert" rather than guessing.
func (s *server) serveFX(w http.ResponseWriter, r *http.Request) {
	rates := s.fx.Rates(r.Context())
	if rates == nil || len(rates.Rate) == 0 {
		http.Error(w, "exchange rates unavailable", http.StatusServiceUnavailable)
		return
	}
	out := fxResponse{Base: "EUR", Date: rates.Date.Format("2006-01-02"), Rates: rates.Rate}
	w.Header().Set("Content-Type", "application/json")
	// The feed changes at most once a working day; let clients and any proxy
	// cache it for an hour.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(out)
}
