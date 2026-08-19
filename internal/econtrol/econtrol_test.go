package econtrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/appmire/charging/internal/model"
)

const (
	operatorsJSON = `[{"operatorId":"006","status":"ACTIVE"},{"operatorId":"014","status":"ACTIVE"}]`

	stations006 = `[{"stationId":"EPI 001","stationStatus":"ACTIVE","label":"Gössendorf",
	  "owner":"EPI - Energietechnik GmbH","street":"Anton - Hubmann - Platz 2","postCode":"8077",
	  "city":"Gössendorf","latitude":46.9857,"longitude":15.4928}]`

	// One decommissioned site, which must not be ingested as a charger.
	stations014 = `[{"stationId":"OLD1","stationStatus":"INACTIVE","label":"Retired",
	  "street":"Weg 1","postCode":"1010","city":"Wien","latitude":48.2,"longitude":16.37}]`

	points006 = `[
	  {"evseId":"AT*006*E01","capacityKw":11,"latitude":null,"longitude":null,"status":"AVAILABLE",
	   "freeOfCharge":false,"priceCentKwh":35,"priceCentMin":0,"startFeeCent":50,
	   "blockingFeeCentMin":5,"blockingFeeFromMinute":45,
	   "connectorType":["CTYPE2"],"electricityType":["AC_3_PHASE"]},
	  {"evseId":"AT*006*E02","capacityKw":150,"latitude":46.99,"longitude":15.50,"status":"CHARGING",
	   "freeOfCharge":false,"priceCentKwh":59,"priceCentMin":0,"startFeeCent":0,
	   "blockingFeeCentMin":0,"blockingFeeFromMinute":0,
	   "connectorType":["CTESLA","CG105","CCCS2","S309-1P-16A"],"electricityType":["DC"]},
	  {"evseId":"AT*006*E03","capacityKw":22,"status":"UNKNOWN","freeOfCharge":true,
	   "priceCentKwh":0,"priceCentMin":0,"startFeeCent":0,
	   "connectorType":["STYPE2"],"electricityType":["AC_3_PHASE"]},
	  {"evseId":"AT*006*E04","capacityKw":22,"status":"UNKNOWN","freeOfCharge":false,
	   "priceCentKwh":0,"priceCentMin":0,"startFeeCent":0,
	   "connectorType":["STYPE2"],"electricityType":["AC_3_PHASE"]}]`
)

// server mimics the register, asserting the two headers the API demands.
func server(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Apikey") == "" || r.Header.Get("Referer") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Zum E-Control Ladestellenverzeichnis gibt es eine …"))
			return
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		switch {
		case r.URL.Path == "/countries/AT/operators":
			_, _ = w.Write([]byte(operatorsJSON))
		case r.URL.Path == "/countries/AT/operators/006/stations":
			_, _ = w.Write([]byte(stations006))
		case r.URL.Path == "/countries/AT/operators/014/stations":
			_, _ = w.Write([]byte(stations014))
		case strings.HasSuffix(r.URL.Path, "/stations/EPI%20001/points"),
			r.URL.Path == "/countries/AT/operators/006/stations/EPI 001/points":
			_, _ = w.Write([]byte(points006))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestStationsAndPoints(t *testing.T) {
	var hits int32
	srv := server(t, &hits)
	defer srv.Close()
	c := New(srv.URL, "key")
	ctx := context.Background()

	stations, err := c.Stations(ctx, "AT")
	if err != nil {
		t.Fatal(err)
	}
	// The INACTIVE site is dropped.
	if len(stations) != 1 || stations[0].Station.StationID != "EPI 001" {
		t.Fatalf("stations = %+v", stations)
	}

	conns, tariffs, err := c.Points(ctx, "at-econtrol", "AT", stations)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 4 {
		t.Fatalf("want 4 connectors, got %d", len(conns))
	}
	by := map[string]int{}
	for i, cn := range conns {
		by[cn.EVSEUID] = i
	}

	// AC point: station coordinates stand in for the missing point ones.
	ac := conns[by["AT*006*E01"]]
	if ac.Lat != 46.9857 || ac.Lon != 15.4928 {
		t.Errorf("coords = %v,%v", ac.Lat, ac.Lon)
	}
	if ac.PlugType != "IEC_62196_T2" || ac.CurrentType != "AC" || ac.PowerKW != 11 {
		t.Errorf("ac = %+v", ac)
	}
	if ac.EVSEStatus != "AVAILABLE" {
		t.Errorf("status = %q", ac.EVSEStatus)
	}
	if ac.Name != "EPI - Energietechnik GmbH · Gössendorf" {
		t.Errorf("name = %q", ac.Name)
	}
	// Cents become euros; the blocking fee keeps its grace threshold and stays a
	// PARKING_TIME component so it is not billed as charging time.
	got := map[string]model.PriceComponent{}
	for _, el := range tariffs[ac.TariffID].Elements {
		for _, pc := range el.PriceComponents {
			got[pc.Type] = pc
		}
	}
	if got["ENERGY"].Price != 0.35 {
		t.Errorf("ENERGY = %v, want 0.35", got["ENERGY"].Price)
	}
	if got["FLAT"].Price != 0.50 {
		t.Errorf("FLAT = %v, want 0.50", got["FLAT"].Price)
	}
	if got["PARKING_TIME"].Price != 3 || got["PARKING_TIME"].AfterMinutes != 45 {
		t.Errorf("PARKING_TIME = %+v, want 3/hour after 45 min", got["PARKING_TIME"])
	}
	if tariffs[ac.TariffID].Currency != "EUR" {
		t.Errorf("currency = %q", tariffs[ac.TariffID].Currency)
	}

	// A point listing several connector types keeps only its most defining plug,
	// and its own coordinates win over the station's.
	dc := conns[by["AT*006*E02"]]
	if dc.PlugType != "IEC_62196_T2_COMBO" || dc.CurrentType != "DC" {
		t.Errorf("dc = %+v", dc)
	}
	if dc.Lat != 46.99 || dc.Lon != 15.50 {
		t.Errorf("dc coords = %v,%v", dc.Lat, dc.Lon)
	}

	// freeOfCharge with no amounts is an explicit zero price…
	free := conns[by["AT*006*E03"]]
	if free.TariffID == "" {
		t.Fatal("want a zero tariff for an explicitly free point")
	}
	if p := priceOf(tariffs[free.TariffID], "ENERGY"); p != 0 {
		t.Errorf("free ENERGY = %v, want 0", p)
	}
	// …but a point that simply published nothing must stay unpriced, not free.
	unpriced := conns[by["AT*006*E04"]]
	if unpriced.TariffID != "" {
		t.Errorf("want no tariff for a point with no published price, got %q", unpriced.TariffID)
	}
}

// The key is bound to a domain, so a missing Referer is an auth failure.
func TestAuthHeadersRequired(t *testing.T) {
	srv := server(t, nil)
	defer srv.Close()
	c := New(srv.URL, "key")
	c.Referer = ""
	if _, err := c.Stations(context.Background(), "AT"); err == nil {
		t.Fatal("want an error without a Referer")
	}
}

// The credential may carry an explicit Referer for a key registered elsewhere.
func TestCredentialParsing(t *testing.T) {
	c := New("https://api.example.test", "abc")
	if c.APIKey != "abc" || c.Referer != DefaultReferer {
		t.Errorf("got key=%q referer=%q", c.APIKey, c.Referer)
	}
	c = New("https://api.example.test", "abc|https://other.test")
	if c.APIKey != "abc" || c.Referer != "https://other.test" {
		t.Errorf("got key=%q referer=%q", c.APIKey, c.Referer)
	}
}

func priceOf(t model.Tariff, kind string) float64 {
	for _, el := range t.Elements {
		for _, pc := range el.PriceComponents {
			if pc.Type == kind {
				return pc.Price
			}
		}
	}
	return -1
}

// A pass is ~15,800 requests, so the odd failure must not discard everything —
// but a widespread one has to fail the pass rather than shrink the register.
func TestPoints_ToleratesIsolatedFailuresButNotWidespreadOnes(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/points") {
			// "bad" stations always fail; the rest fail only when the switch is on.
			if strings.Contains(r.URL.Path, "BAD") || fail.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(points006))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := New(srv.URL, "key")

	// 40 good stations and 1 that always fails: 2.4% — under the tolerance.
	stations := make([]Station, 0, 41)
	for i := 0; i < 40; i++ {
		stations = append(stations, Station{OperatorID: "006", Station: stationJSON{
			StationID: "OK" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Latitude:  47, Longitude: 15, StationStatus: "ACTIVE",
		}})
	}
	stations = append(stations, Station{OperatorID: "006", Station: stationJSON{
		StationID: "BAD1", Latitude: 47, Longitude: 15, StationStatus: "ACTIVE",
	}})

	conns, _, err := c.Points(context.Background(), "at-econtrol", "AT", stations)
	if err != nil {
		t.Fatalf("one bad station out of 41 should not fail the pass: %v", err)
	}
	if len(conns) != 40*4 {
		t.Errorf("got %d connectors, want the 40 good stations' points", len(conns))
	}

	// Now everything fails: the pass must error instead of reporting an empty
	// register (which would otherwise look like every Austrian charger vanished).
	fail.Store(true)
	if _, _, err := c.Points(context.Background(), "at-econtrol", "AT", stations); err == nil {
		t.Error("want an error when every station fails")
	}
}
