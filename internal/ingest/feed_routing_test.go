package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// nowStamp renders a timestamp the dynamic reader accepts and treats as fresh.
func nowStamp() string { return time.Now().UTC().Format("2006-01-02 15:04:05+00:00") }

// feedFor must build the right adapter for each source type, including the two
// EU expansion types and the FR variant that pairs a static base with the
// national dynamic file.
func TestFeedFor_SourceTypes(t *testing.T) {
	cases := []struct {
		sourceType string
		url        string
		want       string
	}{
		{"datex", "https://example.test/es.xml", "ingest.datexFeed"},
		{"datex_afir", "https://example.test/t|https://example.test/s", "ingest.afirPairFeed"},
		{"oicp", "https://example.test/d|https://example.test/s", "ingest.locFeed"},
		{"fintraffic", "https://example.test/api/charging-network/v1", "ingest.fintrafficFeed"},
		{"irve", "https://example.test/static.geojson", "ingest.locFeed"},
		{"irve", "https://example.test/static.geojson|https://example.test/dyn.csv", "ingest.irveFeed"},
		{"eipa", "https://example.test/reader/export-data", "ingest.eipaFeed"},
		{"econtrol", "https://example.test/charge/1.0", "ingest.econtrolFeed"},
	}
	for _, c := range cases {
		src := source.Source{CPO: store.CPO{ID: "x", SourceType: c.sourceType, OCPIBaseURL: c.url}}
		if got := fmt.Sprintf("%T", feedFor(src)); got != c.want {
			t.Errorf("%s (%s): feed = %s, want %s", c.sourceType, c.url, got, c.want)
		}
	}
}

// The French availability pass must reuse the cached static base and re-fetch
// only the small dynamic file — the static is ~585 MB, so downloading it on
// every pass is not an option. The monthly price pass re-reads it.
func TestIRVEFeed_CachesStaticAcrossAvailabilityPasses(t *testing.T) {
	var staticHits, dynHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/static":
			atomic.AddInt32(&staticHits, 1)
			_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[
				{"type":"Feature","geometry":{"type":"Point","coordinates":[2.35,48.85]},
				 "properties":{"id_pdc_itinerance":"FRA1","puissance_nominale":"22","prise_type_2":"true",
				               "nom_station":"Paris","nom_operateur":"Izivia"}}]}`))
		case "/dynamic":
			atomic.AddInt32(&dynHits, 1)
			w.Header().Set("Content-Type", "text/csv")
			fmt.Fprintf(w, "id_pdc_itinerance,etat_pdc,occupation_pdc,horodatage\nFRA1,en_service,libre,%s\n",
				nowStamp())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := irveFeed{cpoID: "irve", staticURL: srv.URL + "/static", dynamicURL: srv.URL + "/dynamic"}
	// Isolate this test from any cache state left by another test.
	irveStaticMu.Lock()
	delete(irveStaticCache, f.staticURL)
	irveStaticMu.Unlock()

	for i := 0; i < 3; i++ {
		conns, err := f.Availability(context.Background())
		if err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
		if len(conns) != 1 || conns[0].EVSEStatus != "AVAILABLE" {
			t.Fatalf("pass %d: connectors = %+v", i, conns)
		}
	}
	if got := atomic.LoadInt32(&staticHits); got != 1 {
		t.Errorf("static fetched %d times across 3 availability passes, want 1", got)
	}
	if got := atomic.LoadInt32(&dynHits); got != 3 {
		t.Errorf("dynamic fetched %d times, want 3", got)
	}

	// The full (price) pass refreshes the base, and reports no tariffs: France
	// publishes no structured price.
	conns, tariffs, err := f.Full(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tariffs) != 0 {
		t.Errorf("want no tariffs for FR, got %d", len(tariffs))
	}
	if len(conns) != 1 || conns[0].EVSEStatus != "AVAILABLE" {
		t.Errorf("full pass connectors = %+v", conns)
	}
	if got := atomic.LoadInt32(&staticHits); got != 2 {
		t.Errorf("static fetched %d times, want 2 (the price pass re-reads it)", got)
	}
}

// An availability pass exists to read the dynamic file, so failing to fetch it
// must fail the pass rather than silently re-report the static base.
func TestIRVEFeed_AvailabilityFailsWithoutDynamicFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/static" {
			_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := irveFeed{cpoID: "irve", staticURL: srv.URL + "/static", dynamicURL: srv.URL + "/dynamic"}
	irveStaticMu.Lock()
	delete(irveStaticCache, f.staticURL)
	irveStaticMu.Unlock()

	if _, err := f.Availability(context.Background()); err == nil {
		t.Error("want an error when the dynamic file cannot be read")
	}
	// The identity pass still succeeds: it does not depend on the dynamic file.
	if _, _, err := f.Full(context.Background()); err != nil {
		t.Errorf("full pass should tolerate a dynamic-file failure: %v", err)
	}
}
