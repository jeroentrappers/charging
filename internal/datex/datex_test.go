package datex

import (
	"os"
	"testing"

	"github.com/appmire/charging/internal/model"
)

// A small DATEX II v3 EnergyInfrastructure publication shaped like the live
// Eco-Movement feed: one site (Gent) with an AC and a DC refill point.
const fixture = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:d2="http://datex2.eu/schema/3/d2Payload" xmlns:egi="http://datex2.eu/schema/3/energyInfrastructure">
  <egi:energyInfrastructureTable>
    <egi:energyInfrastructureSite id="SITE1">
      <egi:name><egi:values><egi:value lang="en">Markt Charging</egi:value></egi:values></egi:name>
      <egi:locationReference>
        <egi:pointByCoordinates>
          <egi:pointCoordinates><egi:latitude>51.0543</egi:latitude><egi:longitude>3.7250</egi:longitude></egi:pointCoordinates>
        </egi:pointByCoordinates>
        <egi:_pointLocationExtension>
          <egi:facilityLocation><egi:address><egi:postcode>9000</egi:postcode><egi:city>Gent</egi:city></egi:address></egi:facilityLocation>
        </egi:_pointLocationExtension>
      </egi:locationReference>
      <egi:operator><egi:name><egi:values><egi:value lang="en">Allego</egi:value></egi:values></egi:name></egi:operator>
      <egi:energyInfrastructureStation id="1">
        <egi:refillPoint id="1"><egi:externalIdentifier>BE*ALL*E1*1</egi:externalIdentifier>
          <egi:connector><egi:connectorType>iec62196T2</egi:connectorType><egi:chargingMode>mode3AC3p</egi:chargingMode><egi:maxPowerAtSocket>22000</egi:maxPowerAtSocket></egi:connector>
        </egi:refillPoint>
        <egi:refillPoint id="1"><egi:externalIdentifier>BE*ALL*E2*1</egi:externalIdentifier>
          <egi:connector><egi:connectorType>iec62196T2Combo</egi:connectorType><egi:chargingMode>mode4</egi:chargingMode><egi:maxPowerAtSocket>150000</egi:maxPowerAtSocket></egi:connector>
        </egi:refillPoint>
      </egi:energyInfrastructureStation>
    </egi:energyInfrastructureSite>
  </egi:energyInfrastructureTable>
</d2:payload>`

func TestParse_DatexEnergyInfrastructure(t *testing.T) {
	conns, tariffs, err := Parse("ecomovement", []byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 2 {
		t.Fatalf("want 2 connectors, got %d", len(conns))
	}
	if len(tariffs) != 0 {
		t.Fatalf("this DATEX profile has no tariffs, got %d", len(tariffs))
	}
	by := map[string]model.Connector{}
	for _, c := range conns {
		by[c.EVSEUID] = c
	}
	ac := by["BE*ALL*E1*1"]
	if ac.CurrentType != model.CurrentAC || ac.PowerKW != 22 {
		t.Fatalf("AC point mapped wrong: %+v", ac)
	}
	if ac.City != "Gent" || ac.Lat == 0 || ac.Lon == 0 {
		t.Fatalf("location not mapped: %+v", ac)
	}
	if ac.Name != "Allego · Markt Charging" {
		t.Fatalf("operator/name not mapped: %q", ac.Name)
	}
	dc := by["BE*ALL*E2*1"]
	if dc.CurrentType != model.CurrentDC || dc.PowerKW != 150 {
		t.Fatalf("DC point mapped wrong: %+v", dc)
	}
	if ac.Available() {
		t.Fatal("DATEX connectors should default to unknown/unavailable status")
	}
}

// Parses the real feed when DATEX_SAMPLE points at a saved response.
func TestParse_RealSample(t *testing.T) {
	path := os.Getenv("DATEX_SAMPLE")
	if path == "" {
		t.Skip("set DATEX_SAMPLE to a DATEX II XML file to run")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	conns, _, err := Parse("ecomovement", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) < 1000 {
		t.Fatalf("expected many connectors from the real feed, got %d", len(conns))
	}
	t.Logf("parsed %d connectors from real DATEX feed", len(conns))
}
