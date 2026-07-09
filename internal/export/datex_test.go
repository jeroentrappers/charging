package export

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/store"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestWriteRegionFilesDATEX verifies the snapshotter emits DATEX II table files
// (XML + JSON) per region and that they round-trip through our own AFIR parser.
func TestWriteRegionFilesDATEX(t *testing.T) {
	dir := t.TempDir()
	s := &Snapshotter{
		Dir:       dir,
		FullEvery: time.Hour,
		Creator:   datex.Creator{Country: "BE", NationalIdentifier: "APM"},
	}
	now := time.Unix(1700000000, 0).UTC()
	if err := s.ensureDir(); err != nil {
		t.Fatal(err)
	}
	price := 0.42
	rows := []store.ExportCharger{
		{ID: 1, CPOID: "dotnl", Country: "BE", PostalCode: "1000", EVSEUID: "E1", ConnectorID: "1", Lat: 50.85, Lon: 4.35, PowerKW: 22, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "AVAILABLE", Currency: "EUR", PriceEUR: &price, Components: tariffRaw(t, 0.40)},
		{ID: 2, CPOID: "dotnl", Country: "BE", PostalCode: "1000", EVSEUID: "E2", ConnectorID: "1", Lat: 50.85, Lon: 4.35, PowerKW: 150, CurrentType: "DC", PlugType: "IEC_62196_T2_COMBO", Status: "CHARGING", Currency: "EUR"},
	}
	files := map[string]FileInfo{}
	if err := s.writeRegionFiles("BE-001", rows, now, files); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"datex/BE-001-table.xml", "datex/BE-001-table.json"} {
		if _, ok := files[rel]; !ok {
			t.Errorf("missing FileInfo for %s", rel)
		}
	}

	xmlBytes, err := os.ReadFile(filepath.Join(dir, "datex", "BE-001-table.xml"))
	if err != nil {
		t.Fatal(err)
	}
	conns, tariffs, err := datex.ParseAFIRStatic("x", xmlBytes)
	if err != nil {
		t.Fatalf("parse emitted table XML: %v", err)
	}
	if len(conns) != 2 {
		t.Errorf("XML round-trip connectors = %d, want 2", len(conns))
	}
	if len(tariffs) != 1 { // only E1 is priced
		t.Errorf("XML round-trip tariffs = %d, want 1", len(tariffs))
	}

	jsonBytes, err := os.ReadFile(filepath.Join(dir, "datex", "BE-001-table.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := datex.ParseAFIRJSON(jsonBytes)
	if err != nil {
		t.Fatalf("parse emitted table JSON: %v", err)
	}
	if doc.Kind != "table" || len(doc.Connectors) != 2 {
		t.Errorf("JSON round-trip: kind=%q connectors=%d", doc.Kind, len(doc.Connectors))
	}
}

// TestDatexPushDeliversFile verifies the pusher POSTs a gzipped file body with
// the right content type + bearer to the subscriber callback.
func TestDatexPushDeliversFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "datex"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("<d2:payload>hello</d2:payload>")
	if err := os.WriteFile(filepath.Join(dir, "datex", "BE-001-table.xml"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	type received struct {
		ct, enc, auth string
		body          []byte
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		b, _ := io.ReadAll(zr)
		got <- received{
			ct:   r.Header.Get("Content-Type"),
			enc:  r.Header.Get("Content-Encoding"),
			auth: r.Header.Get("Authorization"),
			body: b,
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p, err := NewDatexPusher(
		[]PushTarget{{URL: srv.URL, Token: "sekret", Encoding: "xml"}},
		nil, testLogger(), time.Second, "", "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	p.PushTable(t.Context(), dir, []string{"BE-001"})

	select {
	case rec := <-got:
		if !bytes.Equal(rec.body, want) {
			t.Errorf("body = %q, want %q", rec.body, want)
		}
		if rec.ct != "application/xml" {
			t.Errorf("content-type = %q", rec.ct)
		}
		if rec.enc != "gzip" {
			t.Errorf("content-encoding = %q", rec.enc)
		}
		if rec.auth != "Bearer sekret" {
			t.Errorf("authorization = %q", rec.auth)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("push not received within timeout")
	}
}

// TestDatexPushRetriesThenGivesUp verifies a failing target is retried and the
// pusher gives up without panicking (best-effort; snapshot never blocked).
func TestDatexPushRetriesThenGivesUp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "datex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "datex", "status.xml"), []byte("<x/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hits int
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "boom", http.StatusInternalServerError)
		if hits >= 2 {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}))
	defer srv.Close()

	p, _ := NewDatexPusher([]PushTarget{{URL: srv.URL}}, nil, testLogger(), time.Second, "", "", "")
	p.Attempts = 2
	p.PushStatus(t.Context(), dir)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("expected >=2 attempts, got %d", hits)
	}
}
