package ocpi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Test that a 2.2.1 client discovers module URLs from the version-details
// endpoint, authenticates with a base64-encoded token, and reads the
// max_electric_power + tariff_ids fields.
func TestClient_V221_DiscoveryAndAuth(t *testing.T) {
	const token = "secret-token"
	wantAuth := "Token " + base64.StdEncoding.EncodeToString([]byte(token))

	mux := http.NewServeMux()
	var srvURL string

	// Version-details endpoint (the base URL) advertises module URLs at /m/*.
	mux.HandleFunc("/ocpi/cpo/2.2.1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(ObjectEnvelope[VersionDetails]{
			StatusCode: StatusSuccess,
			Data: VersionDetails{Version: "2.2.1", Endpoints: []Endpoint{
				{Identifier: "locations", Role: "SENDER", URL: srvURL + "/m/loc"},
				{Identifier: "tariffs", Role: "SENDER", URL: srvURL + "/m/tar"},
			}},
		})
	})
	mux.HandleFunc("/m/loc", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(Envelope[Location]{
			StatusCode: StatusSuccess,
			Data: []Location{{
				ID: "L1", Coordinates: GeoLocation{Latitude: "51.0", Longitude: "3.7"},
				EVSEs: []EVSE{{UID: "E1", Status: "AVAILABLE", Connectors: []Connector{
					{ID: "1", Standard: "IEC_62196_T2_COMBO", PowerType: "DC",
						MaxElectricPower: 150000, TariffIDs: []string{"T1"}},
				}}},
			}},
		})
	})
	mux.HandleFunc("/m/tar", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Envelope[Tariff]{
			StatusCode: StatusSuccess,
			Data:       []Tariff{{ID: "T1", Currency: "EUR"}},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	c := NewVersioned(srv.URL+"/ocpi/cpo/2.2.1/", token, "2.2.1")

	locs, err := c.Locations(context.Background())
	if err != nil {
		t.Fatalf("locations: %v", err)
	}
	if len(locs) != 1 || len(locs[0].EVSEs) != 1 {
		t.Fatalf("unexpected locations: %+v", locs)
	}
	con := locs[0].EVSEs[0].Connectors[0]
	if con.MaxElectricPower != 150000 {
		t.Fatalf("want max_electric_power 150000, got %d", con.MaxElectricPower)
	}
	if con.Tariff() != "T1" {
		t.Fatalf("want tariff T1 from tariff_ids, got %q", con.Tariff())
	}

	tars, err := c.Tariffs(context.Background())
	if err != nil || len(tars) != 1 || tars[0].ID != "T1" {
		t.Fatalf("tariffs: %v %+v", err, tars)
	}
}

// Test that a 2.1.1 client uses the raw token and the {base}/module fallback
// (no discovery), so existing CPOs keep working.
func TestClient_V211_RawTokenAndFallback(t *testing.T) {
	const token = "raw-token"
	var hitBase bool

	mux := http.NewServeMux()
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token "+token {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(Envelope[Location]{StatusCode: StatusSuccess, Data: []Location{{ID: "L1"}}})
	})
	// If the client wrongly tried discovery, it would GET the base ("/").
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/locations") {
			hitBase = true
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL+"/", token)
	locs, err := c.Locations(context.Background())
	if err != nil || len(locs) != 1 {
		t.Fatalf("locations: %v %+v", err, locs)
	}
	if hitBase {
		t.Fatal("2.1.1 client should not attempt version discovery")
	}
}
