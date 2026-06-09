package datex

import (
	"testing"

	"github.com/appmire/charging/internal/model"
)

// A small DATEX II v3 EnergyInfrastructure publication: one site in Gent with
// two refill points (an AC and a DC), the DC one carrying an ad-hoc price.
const fixture = `<?xml version="1.0" encoding="UTF-8"?>
<payload xmlns="http://datex2.eu/schema/3/energyInfrastructure">
  <energyInfrastructureTable>
    <energyInfrastructureSite id="SITE1">
      <name><values><value lang="nl">Markt Charging</value></values></name>
      <locationReference>
        <pointByCoordinates>
          <pointCoordinates><latitude>51.0543</latitude><longitude>3.7250</longitude></pointCoordinates>
        </pointByCoordinates>
        <addressByName><postalCode>9000</postalCode><city>Gent</city></addressByName>
      </locationReference>
      <energyInfrastructureStation id="ST1">
        <refillPoint id="RP1">
          <connector>
            <connectorType>iec62196T2</connectorType>
            <chargingMode>mode3</chargingMode>
            <maximumPower>22000</maximumPower>
          </connector>
        </refillPoint>
        <refillPoint id="RP2">
          <connector>
            <connectorType>iec62196T2Combo</connectorType>
            <chargingMode>mode4</chargingMode>
            <maximumPower>150000</maximumPower>
          </connector>
          <applicablePrice>
            <priceForEnergy><priceForKWh>0.59</priceForKWh><currency>EUR</currency></priceForEnergy>
          </applicablePrice>
        </refillPoint>
      </energyInfrastructureStation>
    </energyInfrastructureSite>
  </energyInfrastructureTable>
</payload>`

func TestParse_DatexEnergyInfrastructure(t *testing.T) {
	conns, tariffs, err := Parse("ecomovement", []byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 2 {
		t.Fatalf("want 2 connectors, got %d", len(conns))
	}

	byID := map[string]model.Connector{}
	for _, c := range conns {
		byID[c.ConnectorID] = c
	}

	ac := byID["RP1"]
	if ac.CurrentType != model.CurrentAC || ac.PowerKW != 22 {
		t.Fatalf("AC refill point mapped wrong: %+v", ac)
	}
	if ac.City != "Gent" || ac.Lat == 0 || ac.Lon == 0 {
		t.Fatalf("location not mapped: %+v", ac)
	}
	if ac.TariffID != "" {
		t.Fatalf("AC point has no price, should have no tariff: %q", ac.TariffID)
	}

	dc := byID["RP2"]
	if dc.CurrentType != model.CurrentDC || dc.PowerKW != 150 {
		t.Fatalf("DC refill point mapped wrong: %+v", dc)
	}
	if dc.TariffID == "" {
		t.Fatal("DC point has a price; expected a tariff id")
	}
	tar, ok := tariffs[dc.TariffID]
	if !ok {
		t.Fatalf("tariff %s not present", dc.TariffID)
	}
	if tar.Currency != "EUR" || len(tar.Elements) != 1 ||
		tar.Elements[0].PriceComponents[0].Price != 0.59 {
		t.Fatalf("tariff mapped wrong: %+v", tar)
	}

	// DATEX static feed carries no live status.
	if ac.Available() {
		t.Fatal("DATEX connectors should default to unknown/unavailable status")
	}
}
