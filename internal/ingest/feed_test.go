package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDatexURL(t *testing.T) {
	const base = "https://api.eco-movement.com/api/nap/datexii/locations"
	cases := []struct {
		name, url, token, want string
	}{
		{"open feed, no token", base, "", base},
		{"token appended", base, "abc123", base + "?token=abc123"},
		{"token merged into existing query", base + "?perPage=500", "abc123", base + "?perPage=500&token=abc123"},
		{"token is escaped", base, "a b/c", base + "?token=a+b%2Fc"},
	}
	for _, c := range cases {
		if got := datexURL(c.url, c.token); got != c.want {
			t.Errorf("%s: datexURL(%q,%q) = %q, want %q", c.name, c.url, c.token, got, c.want)
		}
	}
}

// EnergyVision-shaped table: one site with coordinates, one without any
// locationReference (the live v1 feed publishes none at all).
const afirPairTableXML = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:aegi="http://datex2.eu/schema/3/afirEnergyInfrastructure"
            xmlns:com="http://datex2.eu/schema/3/common"
            xsi:type="aegi:EnergyInfrastructureTablePublication"
            xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
            xmlns:d2="http://datex2.eu/schema/3/d2Payload">
  <aegi:energyInfrastructureTable id="T1">
    <aegi:energyInfrastructureSite id="SITE1">
      <aegi:name><com:values><com:value>Located</com:value></com:values></aegi:name>
      <aegi:locationReference>
        <com:pointByCoordinates><com:pointCoordinates>
          <com:latitude>50.85</com:latitude><com:longitude>4.35</com:longitude>
        </com:pointCoordinates></com:pointByCoordinates>
      </aegi:locationReference>
      <aegi:energyInfrastructureStation id="SITE1-station">
        <aegi:refillPoint id="RP1">
          <aegi:connector>
            <aegi:connectorType>iec62196T2</aegi:connectorType>
            <aegi:maxPowerAtSocket>7400</aegi:maxPowerAtSocket>
          </aegi:connector>
        </aegi:refillPoint>
      </aegi:energyInfrastructureStation>
    </aegi:energyInfrastructureSite>
    <aegi:energyInfrastructureSite id="SITE2">
      <aegi:energyInfrastructureStation id="SITE2-station">
        <aegi:refillPoint id="RP2">
          <aegi:connector>
            <aegi:connectorType>iec62196T2</aegi:connectorType>
            <aegi:maxPowerAtSocket>7400</aegi:maxPowerAtSocket>
          </aegi:connector>
        </aegi:refillPoint>
      </aegi:energyInfrastructureStation>
    </aegi:energyInfrastructureSite>
  </aegi:energyInfrastructureTable>
</d2:payload>`

const afirPairStatusXML = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:aegi="http://datex2.eu/schema/3/afirEnergyInfrastructure"
            xmlns:afac="http://datex2.eu/schema/3/afirFacilities"
            xsi:type="aegi:EnergyInfrastructureStatusPublication"
            xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
            xmlns:d2="http://datex2.eu/schema/3/d2Payload">
  <aegi:energyInfrastructureSiteStatus>
    <aegi:energyInfrastructureStationStatus>
      <afac:reference id="SITE1-station" targetClass="afac:FacilityObject"/>
      <aegi:refillPointStatus xsi:type="aegi:ElectricChargingPointStatus">
        <afac:reference id="RP1" targetClass="afac:FacilityObject"/>
        <aegi:status>charging</aegi:status>
      </aegi:refillPointStatus>
      <aegi:energyRateUpdate>
        <aegi:energyPrice>
          <aegi:priceType>pricePerKWh</aegi:priceType>
          <aegi:value>0.28</aegi:value>
        </aegi:energyPrice>
      </aegi:energyRateUpdate>
    </aegi:energyInfrastructureStationStatus>
  </aegi:energyInfrastructureSiteStatus>
</d2:payload>`

// TestAFIRPairFeed drives the Bearer table+status pair end to end: auth header,
// status + station-level price overlay, the Null-Island filter, and the static
// table cache (the table endpoint must be hit only once across two passes).
func TestAFIRPairFeed(t *testing.T) {
	var tableHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/datex/energy-infrastructure-table", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		tableHits.Add(1)
		w.Write([]byte(afirPairTableXML))
	})
	mux.HandleFunc("/datex/energy-infrastructure-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(afirPairStatusXML))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	base := srv.URL + "/datex/energy-infrastructure-table|" + srv.URL + "/datex/energy-infrastructure-status"
	f := newAFIRPairFeed("energyvision", base, "key123")

	conns, tariffs, err := f.Full(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d connectors, want 1 (coordinate-less RP2 dropped)", len(conns))
	}
	c := conns[0]
	if c.EVSEUID != "RP1" || c.Lat != 50.85 || c.Lon != 4.35 {
		t.Errorf("unexpected connector: %+v", c)
	}
	if c.EVSEStatus != "CHARGING" {
		t.Errorf("status overlay: got %q, want CHARGING", c.EVSEStatus)
	}
	if c.TariffID == "" {
		t.Fatal("station-level price update not attached")
	}
	tar, ok := tariffs[c.TariffID]
	if !ok || tar.Elements[0].PriceComponents[0].Price != 0.28 {
		t.Errorf("tariff = %+v, want ENERGY 0.28", tar)
	}

	// Second pass: table comes from the cache, status is re-fetched.
	if _, err := f.Availability(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tableHits.Load(); got != 1 {
		t.Errorf("table endpoint hit %d times, want 1 (cached)", got)
	}
}
