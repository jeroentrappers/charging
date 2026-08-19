package fintraffic

import (
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/appmire/charging/internal/normalize"
)

const locationsJSON = `{
  "type": "FeatureCollection",
  "modifiedAt": "2026-08-18T14:14:38.085Z",
  "pagination": {"limit": 1},
  "features": [
    {
      "type": "Feature",
      "geometry": {"type": "Point", "coordinates": [24.875, 60.1911]},
      "properties": {
        "id": "loc-1",
        "name": "Kamppi P1",
        "operator": {"details": {"name": "Liikennevirta"}},
        "address": {"street": "Kärkitie 4", "city": "Helsinki", "postalCode": "00330", "countryCode": "FIN"},
        "evses": [
          {
            "id": "FI*001*E*1",
            "geometry": null,
            "connectors": [
              {"powerType": "DC", "standard": "IEC_62196_T2_COMBO", "format": "CABLE",
               "maxVoltage": 500, "maxAmperage": 300, "maxElectricPower": 150000,
               "tariffIds": ["t-net"]},
              {"powerType": "AC_3_PHASE", "standard": "IEC_62196_T2", "format": "SOCKET",
               "maxVoltage": 400, "maxAmperage": 32, "maxElectricPower": 22000,
               "tariffIds": []}
            ]
          }
        ]
      }
    }
  ]
}`

const statusesJSON = `{"pagination":{"limit":1},"statuses":[{"evseId":"FI*001*E*1","status":"AVAILABLE"}]}`

const tariffsJSON = `{"pagination":{"limit":2},"tariffs":[
  {"id":"t-net","currency":"EUR","type":"AD_HOC_PAYMENT","taxIncluded":"NO",
   "elements":[{"priceComponents":[{"type":"ENERGY","price":0.4,"vat":25.5,"stepSize":1}],
                "restrictions":{"startTime":"07:00","endTime":"23:00","minDuration":300}}],
   "lastUpdated":"2026-08-18T04:00:19.357Z"},
  {"id":"t-gross","currency":"EUR","taxIncluded":"YES",
   "elements":[{"priceComponents":[{"type":"ENERGY","price":0.5,"stepSize":1}]}]}
]}`

func TestToOCPI_ShapeAndStatus(t *testing.T) {
	locs, statuses, tars := decodeAll(t)
	ol, ot := ToOCPI(locs, statuses, tars)
	if len(ol) != 1 {
		t.Fatalf("want 1 location, got %d", len(ol))
	}
	loc := ol[0]
	if loc.Name != "Liikennevirta · Kamppi P1" {
		t.Errorf("name = %q", loc.Name)
	}
	if loc.Coordinates.Latitude != "60.1911" || loc.Coordinates.Longitude != "24.875" {
		t.Errorf("coords = %+v", loc.Coordinates)
	}
	if loc.Address != "Kärkitie 4" || loc.City != "Helsinki" || loc.PostalCode != "00330" {
		t.Errorf("address = %q / %q / %q", loc.Address, loc.City, loc.PostalCode)
	}
	if len(loc.EVSEs) != 1 || loc.EVSEs[0].Status != "AVAILABLE" {
		t.Fatalf("evses = %+v", loc.EVSEs)
	}
	// Connectors carry no id in this feed, so they are numbered by position.
	cs := loc.EVSEs[0].Connectors
	if len(cs) != 2 || cs[0].ID != "1" || cs[1].ID != "2" {
		t.Fatalf("connector ids = %+v", cs)
	}

	// End-to-end through the shared OCPI normalizer: power comes from
	// maxElectricPower and the tariff reference survives.
	res := normalize.FromOCPI("fi-fintraffic", ol, ot)
	if len(res.Connectors) != 2 {
		t.Fatalf("want 2 connectors, got %d", len(res.Connectors))
	}
	if res.Connectors[0].PowerKW != 150 || res.Connectors[0].CurrentType != "DC" {
		t.Errorf("dc connector = %+v", res.Connectors[0])
	}
	if res.Connectors[0].TariffID != "t-net" {
		t.Errorf("tariff id = %q", res.Connectors[0].TariffID)
	}
	if res.Connectors[1].PowerKW != 22 {
		t.Errorf("ac power = %v", res.Connectors[1].PowerKW)
	}
}

// Fintraffic publishes net prices with the VAT rate alongside; every other
// source we ingest quotes tax-inclusive ad-hoc prices, so net prices must be
// grossed up or Finnish chargers rank as ~20% cheaper than they are.
func TestToOCPI_GrossesUpVAT(t *testing.T) {
	_, _, tars := decodeAll(t)
	_, ot := ToOCPI(nil, nil, tars)
	by := map[string]float64{}
	for _, tf := range ot {
		by[tf.ID] = tf.Elements[0].PriceComponents[0].Price
	}
	if got, want := by["t-net"], 0.502; got != want { // 0.40 * 1.255
		t.Errorf("net tariff = %v, want %v", got, want)
	}
	if got, want := by["t-gross"], 0.5; got != want { // taxIncluded=YES: untouched
		t.Errorf("gross tariff = %v, want %v", got, want)
	}
}

func TestToOCPI_KeepsRestrictions(t *testing.T) {
	_, _, tars := decodeAll(t)
	_, ot := ToOCPI(nil, nil, tars)
	for _, tf := range ot {
		if tf.ID != "t-net" {
			continue
		}
		r := tf.Elements[0].Restrictions
		if r == nil {
			t.Fatal("want restrictions to survive the camelCase mapping")
		}
		if r.StartTime != "07:00" || r.EndTime != "23:00" {
			t.Errorf("time window = %q-%q", r.StartTime, r.EndTime)
		}
		if r.MinDuration == nil || *r.MinDuration != 300 {
			t.Errorf("minDuration = %v", r.MinDuration)
		}
	}
}

// The server rejects requests that do not identify themselves or do not accept
// gzip (HTTP 406), and it gzips its responses — so the client must set both
// headers and gunzip itself.
func TestClient_SendsRequiredHeadersAndGunzips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Digitraffic-User") == "" || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		if r.URL.Query().Get("limit") != "ALL" {
			t.Errorf("limit = %q, want ALL", r.URL.Query().Get("limit"))
		}
		body := map[string]string{
			"/locations":          locationsJSON,
			"/locations/statuses": statusesJSON,
			"/tariffs":            tariffsJSON,
		}[r.URL.Path]
		if body == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_, _ = gz.Write([]byte(body))
	}))
	defer srv.Close()

	locs, tars, err := New(srv.URL).Snapshot(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 || len(locs[0].EVSEs) != 1 {
		t.Fatalf("locations = %+v", locs)
	}
	if locs[0].EVSEs[0].Status != "AVAILABLE" {
		t.Errorf("status not folded in: %+v", locs[0].EVSEs[0])
	}
	if len(tars) != 2 {
		t.Errorf("want 2 tariffs, got %d", len(tars))
	}

	// The availability path must skip the tariffs fetch entirely.
	if _, tars, err = New(srv.URL).Snapshot(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(tars) != 0 {
		t.Errorf("want no tariffs on the light path, got %d", len(tars))
	}
}

// decodeAll parses the fixtures through the package's own wire types.
func decodeAll(t *testing.T) ([]locationFeature, map[string]string, []tariff) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/locations":
			_, _ = w.Write([]byte(locationsJSON))
		case "/locations/statuses":
			_, _ = w.Write([]byte(statusesJSON))
		case "/tariffs":
			_, _ = w.Write([]byte(tariffsJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL)
	ctx := context.Background()
	locs, _, err := c.locations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tars, err := c.tariffs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return locs, st, tars
}
