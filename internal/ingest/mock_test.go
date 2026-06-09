package ingest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"github.com/appmire/charging/internal/ocpi"
)

// mockFeed is an in-memory OCPI 2.1.1 sender interface for tests. Its data can
// be swapped between ingestion passes to simulate a CPO changing its tariff.
type mockFeed struct {
	mu        sync.Mutex
	locations []ocpi.Location
	tariffs   []ocpi.Tariff
	wantToken string
}

func newMockFeed(token string) *mockFeed { return &mockFeed{wantToken: token} }

func (m *mockFeed) set(locs []ocpi.Location, tars []ocpi.Tariff) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locations, m.tariffs = locs, tars
}

func (m *mockFeed) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		writePage(w, r, m.locations)
	})
	mux.HandleFunc("/tariffs", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		writePage(w, r, m.tariffs)
	})
	srv := httptest.NewServer(authMiddleware(m.wantToken, mux))
	return srv
}

func authMiddleware(want string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want != "" && r.Header.Get("Authorization") != "Token "+want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writePage applies offset/limit pagination and the standard OCPI envelope,
// exercising the client's paging logic with small pages.
func writePage[T any](w http.ResponseWriter, r *http.Request, all []T) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	page := []T{}
	if offset < len(all) {
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		page = all[offset:end]
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ocpi.Envelope[T]{
		Data:       page,
		StatusCode: ocpi.StatusSuccess,
		StatusMsg:  "Success",
		Timestamp:  time.Now().UTC(),
	})
}

// ---- fixtures ----

func sampleLocation(status string) ocpi.Location {
	return ocpi.Location{
		ID:         "LOC1",
		Type:       "ON_STREET",
		Name:       "Markt Charging",
		Address:    "Markt 1",
		City:       "Gent",
		PostalCode: "9000",
		Country:    "BEL",
		Coordinates: ocpi.GeoLocation{
			Latitude:  "51.05432",
			Longitude: "3.72500",
		},
		EVSEs: []ocpi.EVSE{{
			UID:    "EVSE1",
			EVSEID: "BE*EVI*E1",
			Status: status,
			Connectors: []ocpi.Connector{{
				ID:        "1",
				Standard:  "IEC_62196_T2",
				Format:    "SOCKET",
				PowerType: "AC_3_PHASE",
				Voltage:   230,
				Amperage:  32,
				TariffID:  "TAR1",
			}},
		}},
		LastUpdated: time.Now().UTC(),
	}
}

func sampleTariff(energyPrice float64, updated time.Time) ocpi.Tariff {
	return ocpi.Tariff{
		ID:       "TAR1",
		Currency: "EUR",
		Elements: []ocpi.TariffElement{{
			PriceComponents: []ocpi.PriceComponent{
				{Type: "ENERGY", Price: energyPrice, StepSize: 1},
				{Type: "FLAT", Price: 0.25, StepSize: 1},
			},
		}},
		LastUpdated: updated,
	}
}
