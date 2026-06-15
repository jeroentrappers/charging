package store

import (
	"testing"
	"time"
)

func p(v float64) *float64 { return &v }

func TestClusterByLocationPower(t *testing.T) {
	now := time.Now()
	// One site (50.9119, 4.3404) with 3×11kW chargers (2 free, 1 busy) all priced
	// the same, plus 1×50kW charger; and a second site nearby with 1 charger.
	rows := []NearbyCharger{
		{ID: 1, EVSEUID: "A1", CPOID: "monta", Lat: 50.9119, Lon: 4.3404, PowerKW: 11, PriceEUR: p(8.0), Available: 1, StatusAt: &now},
		{ID: 2, EVSEUID: "A2", CPOID: "monta", Lat: 50.9119, Lon: 4.3404, PowerKW: 11, PriceEUR: p(8.0), Available: 1, StatusAt: &now},
		{ID: 3, EVSEUID: "A3", CPOID: "monta", Lat: 50.91191, Lon: 4.34041, PowerKW: 11, PriceEUR: p(8.0), Available: 0, StatusAt: &now},
		{ID: 4, EVSEUID: "A4", CPOID: "monta", Lat: 50.9119, Lon: 4.3404, PowerKW: 50, PriceEUR: p(20.0), Available: 1, StatusAt: &now},
		{ID: 5, EVSEUID: "B1", CPOID: "monta", Lat: 51.20, Lon: 4.40, PowerKW: 22, PriceEUR: p(12.0), Available: 0, StatusAt: &now},
	}

	out := ClusterByLocationPower(rows, 100, false)
	// Expect 3 clusters: site-A 11kW (3), site-A 50kW (1), site-B 22kW (1).
	if len(out) != 3 {
		t.Fatalf("got %d clusters; want 3", len(out))
	}

	var a11 *NearbyCharger
	for i := range out {
		if out[i].PowerKW == 11 {
			a11 = &out[i]
		}
	}
	if a11 == nil {
		t.Fatal("missing 11kW cluster")
	}
	if a11.GroupTotal != 3 || a11.GroupAvailable != 2 || a11.GroupBusy != 1 {
		t.Errorf("11kW cluster = total %d avail %d busy %d; want 3/2/1", a11.GroupTotal, a11.GroupAvailable, a11.GroupBusy)
	}
	if len(a11.Members) != 3 {
		t.Errorf("11kW members = %d; want 3", len(a11.Members))
	}

	// Cheapest cluster (8.0) ranks first; singles keep GroupTotal 0.
	if out[0].PriceEUR == nil || *out[0].PriceEUR != 8.0 {
		t.Errorf("first cluster price = %v; want 8.0 (cheapest)", out[0].PriceEUR)
	}
	for _, c := range out {
		if c.GroupTotal == 1 {
			t.Errorf("single-charger group should leave GroupTotal 0, got 1")
		}
	}
}

func TestClusterByLocationPower_PreferPricedDropsUnpriced(t *testing.T) {
	now := time.Now()
	rows := []NearbyCharger{
		{ID: 1, EVSEUID: "P1", CPOID: "x", Lat: 50.0, Lon: 4.0, PowerKW: 22, PriceEUR: p(10), Available: 1, StatusAt: &now},
		{ID: 2, EVSEUID: "U1", CPOID: "monta", Lat: 50.1, Lon: 4.1, PowerKW: 22, Available: 1, StatusAt: &now}, // unpriced
	}
	// preferPriced: the unpriced cluster is dropped because a priced one exists.
	out := ClusterByLocationPower(rows, 100, true)
	if len(out) != 1 || out[0].CPOID != "x" {
		t.Fatalf("preferPriced kept %d clusters (%v); want 1 priced", len(out), out)
	}

	// With no priced chargers at all, unpriced is returned as fallback.
	out = ClusterByLocationPower(rows[1:], 100, true)
	if len(out) != 1 || out[0].CPOID != "monta" {
		t.Fatalf("fallback kept %d (%v); want the unpriced cluster", len(out), out)
	}
}
