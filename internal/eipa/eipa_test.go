package eipa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/appmire/charging/internal/model"
)

// Shaped like the live files: one electric site with two points (one priced in
// several ad-hoc variants), one gas station and one suspended station.
const (
	poolJSON = `{"generated":"2026-08-19T10:30:03+02:00","data":[
	  {"id":240,"operator_id":4,"charging":true,"code":"PL-GJC-PEVP01001","name":"Business Garden",
	   "latitude":51.116886,"longitude":16.99725,"street":"ul. Legnicka","house_number":"48G-H",
	   "postal_code":"54-202","city":"Wrocław"},
	  {"id":999,"operator_id":4,"charging":false,"code":"PL-X","name":"Gas only",
	   "latitude":52.0,"longitude":21.0,"street":"ul. Gazowa","postal_code":"00-001","city":"Warszawa"}]}`

	stationJSON = `{"data":[
	  {"id":1153,"pool_id":240,"type":"E","suspended":false,"latitude":51.116886,"longitude":16.99725},
	  {"id":1154,"pool_id":240,"type":"E","suspended":true,"latitude":51.116886,"longitude":16.99725},
	  {"id":1155,"pool_id":999,"type":"G","suspended":false,"latitude":52.0,"longitude":21.0}]}`

	pointJSON = `{"data":[
	  {"id":13477,"station_id":1153,"code":"PL-GJC-EEVP01001",
	   "charging_solutions":[{"mode":6,"power":22}],
	   "connectors":[{"interfaces":[10],"power":22,"cable_attached":false}]},
	  {"id":13478,"station_id":1153,"code":"PL-GJC-EEVP01002",
	   "charging_solutions":[{"mode":7,"power":150}],
	   "connectors":[{"interfaces":[29],"power":150,"cable_attached":true}]},
	  {"id":13479,"station_id":1154,"code":"PL-SUSPENDED-1",
	   "connectors":[{"interfaces":[10],"power":22}]},
	  {"id":13480,"station_id":1155,"code":"PL-GAS-1","connectors":[]}]}`

	operatorJSON = `{"data":[{"id":4,"name":"EV PLUS Sp. z o.o.","short_name":"EV PLUS","code":"PL-GJC"}]}`

	dictJSON = `{"connector_interface":[
	   {"id":10,"name":"IEC-62196-T2-F-NOCABLE"},{"id":29,"name":"IEC-62196-T2-COMBO"},
	   {"id":11,"name":"CHADEMO"},{"id":21,"name":"IEC-309-2-1PH"}],
	  "charging_mode":[{"id":6,"name":"Mode3-AC-3p"},{"id":7,"name":"Mode4-DC"}]}`
)

// dynamicJSON is built per-test so the status timestamps can be fresh or stale.
func dynamicJSON(fresh, stale string) string {
	return `{"data":[
	  {"point_id":13477,"code":"PL-GJC-EEVP01001",
	   "status":{"availability":1,"status":1,"ts":"` + fresh + `"},
	   "prices":[{"literal":"opłata za kWh DZIEŃ","price":"2.00","unit":"kWh","ts":"` + fresh + `"},
	             {"literal":"ładowanie za pomocą Ad hoc","price":"2.40","unit":"kWh","ts":"` + fresh + `"},
	             {"literal":"opłata za minutę","price":"0.10","unit":"min","ts":"` + fresh + `"},
	             {"literal":"gaz","price":"5.00","unit":"m3","ts":"` + fresh + `"}]},
	  {"point_id":13478,"code":"PL-GJC-EEVP01002",
	   "status":{"availability":1,"status":0,"ts":"` + stale + `"},
	   "prices":[{"literal":"","price":"3.00","unit":"kWh","ts":"` + fresh + `"},
	             {"literal":"","price":"3.50","unit":"kWh","ts":"` + fresh + `"},
	             {"literal":"0.50 zł/min (naliczany po 60 min)","price":"163.00","unit":"kWh","ts":"` + fresh + `"}]}]}`
}

func serve(t *testing.T, dyn string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range map[string]string{
		"/pool/tok": poolJSON, "/station/tok": stationJSON, "/point/tok": pointJSON,
		"/operator/tok": operatorJSON, "/dictionary/tok": dictJSON, "/dynamic/tok": dyn,
	} {
		b := body
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(b)) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL, "tok")
}

func TestBuild(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Hour).Format(time.RFC3339)
	stale := now.Add(-90 * 24 * time.Hour).Format(time.RFC3339)
	c := serve(t, dynamicJSON(fresh, stale))

	st, err := c.Static(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dyn, err := c.Dynamic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conns, tariffs := Build("pl-eipa", st, dyn, now)

	// The suspended station, the gas station and the non-charging site are all out.
	if len(conns) != 2 {
		t.Fatalf("want 2 connectors, got %d: %+v", len(conns), conns)
	}
	by := map[string]int{}
	for i, cn := range conns {
		by[cn.EVSEUID] = i
	}

	ac := conns[by["PL-GJC-EEVP01001"]]
	if ac.PlugType != "IEC_62196_T2" || ac.CurrentType != "AC" || ac.PowerKW != 22 {
		t.Errorf("ac connector = %+v", ac)
	}
	if ac.EVSEStatus != "AVAILABLE" {
		t.Errorf("status = %q, want AVAILABLE", ac.EVSEStatus)
	}
	if ac.Name != "EV PLUS · Business Garden" {
		t.Errorf("name = %q", ac.Name)
	}
	if want := "ul. Legnicka 48G-H, 54-202 Wrocław"; ac.Address != want {
		t.Errorf("address = %q, want %q", ac.Address, want)
	}

	// The ad-hoc labelled variant wins over the cheaper "DZIEŃ" one, the gas price
	// is ignored, and per-minute becomes our per-hour TIME component.
	tf := tariffs[ac.TariffID]
	if tf.Currency != Currency {
		t.Errorf("currency = %q, want %s", tf.Currency, Currency)
	}
	got := map[string]float64{}
	for _, el := range tf.Elements {
		for _, pc := range el.PriceComponents {
			got[pc.Type] = pc.Price
		}
	}
	if got["ENERGY"] != 2.40 {
		t.Errorf("ENERGY = %v, want the ad-hoc labelled 2.40 (got %v)", got["ENERGY"], got)
	}
	if got["TIME"] != 6 { // 0.10/min * 60
		t.Errorf("TIME = %v, want 6", got["TIME"])
	}

	// The DC point's status is months old, so it must not claim to be occupied.
	dc := conns[by["PL-GJC-EEVP01002"]]
	if dc.EVSEStatus != "UNKNOWN" {
		t.Errorf("stale status = %q, want UNKNOWN", dc.EVSEStatus)
	}
	if dc.PlugType != "IEC_62196_T2_COMBO" || dc.CurrentType != "DC" {
		t.Errorf("dc connector = %+v", dc)
	}
	// Unlabelled variants: the dearest plausible one wins, and the mis-published
	// 163.00 (its own label says zł/min) is rejected by the ceiling.
	if p := priceOf(tariffs[dc.TariffID], "ENERGY"); p != 3.50 {
		t.Errorf("ENERGY = %v, want 3.50", p)
	}
}

// Operational unavailability outranks the free/occupied flag: an out-of-service
// point is unusable whether or not something is plugged into it.
func TestStatusVocab(t *testing.T) {
	now := time.Now()
	ts := now.Add(-time.Hour).Format(time.RFC3339)
	i := func(v int) *int { return &v }
	cases := []struct {
		avail, status *int
		ts            string
		want          string
	}{
		{i(1), i(1), ts, "AVAILABLE"},
		{i(1), i(0), ts, "CHARGING"},
		{i(0), i(1), ts, "OUTOFORDER"},
		{i(0), i(0), ts, "OUTOFORDER"},
		{nil, nil, ts, "UNKNOWN"},
		{i(1), i(1), now.Add(-40 * time.Hour).Format(time.RFC3339), "UNKNOWN"}, // too old
		{i(1), i(1), "", "UNKNOWN"},
	}
	for _, c := range cases {
		var s *dynamicStatus
		if c.avail != nil || c.status != nil || c.ts != "" {
			s = &dynamicStatus{Availability: c.avail, Status: c.status, TS: c.ts}
		}
		if got := statusVocab(s, now); got != c.want {
			t.Errorf("availability=%v status=%v ts=%q -> %q, want %q", c.avail, c.status, c.ts, got, c.want)
		}
	}
}

func TestPlugStandard(t *testing.T) {
	cases := map[string]string{
		"IEC-62196-T2-F-NOCABLE": "IEC_62196_T2",
		"IEC-62196-T2-F-CABLE":   "IEC_62196_T2",
		"IEC-62196-T2-COMBO":     "IEC_62196_T2_COMBO",
		"IEC-62196-T1-COMBO":     "IEC_62196_T1_COMBO",
		"CHADEMO":                "CHADEMO",
		"TESLA-SPECIFIC":         "TESLA_S",
		"DOMESTIC-F":             "DOMESTIC_F",
		"IEC-309-2-1PH":          "", // industrial socket, not a car plug we rank on
	}
	for in, want := range cases {
		if got := plugStandard(in); got != want {
			t.Errorf("plugStandard(%q) = %q, want %q", in, got, want)
		}
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
