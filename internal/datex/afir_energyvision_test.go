package datex

import "testing"

// EnergyVision-shaped status publication (validated against the live feed,
// 2026-07): station statuses sit under energyInfrastructureSiteStatus, and the
// ad-hoc price update hangs off the STATION (no applicableCurrency), not off
// each refillPointStatus.
const energyVisionStatusXML = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:aegi="http://datex2.eu/schema/3/afirEnergyInfrastructure"
            xmlns:afac="http://datex2.eu/schema/3/afirFacilities"
            xmlns:com="http://datex2.eu/schema/3/common"
            xsi:type="aegi:EnergyInfrastructureStatusPublication"
            xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
            xmlns:d2="http://datex2.eu/schema/3/d2Payload">
  <com:publicationTime>2026-07-08T06:42:01Z</com:publicationTime>
  <aegi:energyInfrastructureSiteStatus>
    <afac:reference id="SITE1" targetClass="afac:FacilityObject"/>
    <aegi:energyInfrastructureStationStatus>
      <afac:reference id="SITE1-station" targetClass="afac:FacilityObject"/>
      <aegi:isAvailable>true</aegi:isAvailable>
      <aegi:refillPointStatus xsi:type="aegi:ElectricChargingPointStatus">
        <afac:reference id="RP1" targetClass="afac:FacilityObject"/>
        <aegi:status>available</aegi:status>
      </aegi:refillPointStatus>
      <aegi:refillPointStatus xsi:type="aegi:ElectricChargingPointStatus">
        <afac:reference id="RP2" targetClass="afac:FacilityObject"/>
        <aegi:status>charging</aegi:status>
      </aegi:refillPointStatus>
      <aegi:energyRateUpdate>
        <aegi:lastUpdated>2025-03-25T12:12:24Z</aegi:lastUpdated>
        <aegi:energyRateReference id="SITE1-station-adhoc-rate" targetClass="aegi:EnergyRate"/>
        <aegi:energyPrice>
          <aegi:priceGroupIndex>1</aegi:priceGroupIndex>
          <aegi:priceType>pricePerKWh</aegi:priceType>
          <aegi:value>0.28</aegi:value>
          <aegi:taxIncluded>true</aegi:taxIncluded>
          <aegi:taxRate>21</aegi:taxRate>
        </aegi:energyPrice>
      </aegi:energyRateUpdate>
    </aegi:energyInfrastructureStationStatus>
  </aegi:energyInfrastructureSiteStatus>
  <aegi:energyInfrastructureSiteStatus>
    <afac:reference id="SITE2" targetClass="afac:FacilityObject"/>
    <aegi:energyInfrastructureStationStatus>
      <afac:reference id="SITE2-station" targetClass="afac:FacilityObject"/>
      <aegi:refillPointStatus xsi:type="aegi:ElectricChargingPointStatus">
        <afac:reference id="RP3" targetClass="afac:FacilityObject"/>
        <aegi:status>unknown</aegi:status>
        <aegi:energyRateUpdate>
          <aegi:applicableCurrency>EUR</aegi:applicableCurrency>
          <aegi:energyPrice>
            <aegi:priceType>pricePerKWh</aegi:priceType>
            <aegi:value>0.61</aegi:value>
          </aegi:energyPrice>
        </aegi:energyRateUpdate>
      </aegi:refillPointStatus>
    </aegi:energyInfrastructureStationStatus>
  </aegi:energyInfrastructureSiteStatus>
</d2:payload>`

func TestParseAFIRStatusStationLevelRateUpdate(t *testing.T) {
	st, err := ParseAFIRStatus([]byte(energyVisionStatusXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 3 {
		t.Fatalf("got %d refill point statuses, want 3", len(st))
	}

	rp1 := st["RP1"]
	if rp1.Status != "AVAILABLE" {
		t.Errorf("RP1 status = %q, want AVAILABLE", rp1.Status)
	}
	if rp1.Tariff == nil {
		t.Fatal("RP1: station-level energyRateUpdate not applied")
	}
	if got := rp1.Tariff.Elements[0].PriceComponents[0]; got.Type != "ENERGY" || got.Price != 0.28 {
		t.Errorf("RP1 price = %+v, want ENERGY 0.28", got)
	}
	if rp1.Tariff.Currency != "EUR" {
		t.Errorf("RP1 currency = %q, want EUR default", rp1.Tariff.Currency)
	}

	rp2 := st["RP2"]
	if rp2.Status != "CHARGING" {
		t.Errorf("RP2 status = %q, want CHARGING", rp2.Status)
	}
	if rp2.Tariff == nil || rp2.Tariff.Elements[0].PriceComponents[0].Price != 0.28 {
		t.Error("RP2: station-level rate should apply to every refill point in the station")
	}

	// A refill point's own update still wins over its station's.
	rp3 := st["RP3"]
	if rp3.Status != "UNKNOWN" {
		t.Errorf("RP3 status = %q, want UNKNOWN", rp3.Status)
	}
	if rp3.Tariff == nil || rp3.Tariff.Elements[0].PriceComponents[0].Price != 0.61 {
		t.Error("RP3: own energyRateUpdate should win over the station's")
	}
}

func TestParseAFIRXMLStationLevelRateUpdate(t *testing.T) {
	doc, err := ParseAFIR([]byte(energyVisionStatusXML))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "status" {
		t.Fatalf("kind = %q, want status", doc.Kind)
	}
	if len(doc.Statuses) != 3 {
		t.Fatalf("got %d status updates, want 3", len(doc.Statuses))
	}
	withTariff := 0
	for _, u := range doc.Statuses {
		if u.Tariff != nil {
			withTariff++
		}
	}
	if withTariff != 3 {
		t.Errorf("%d updates carry a tariff, want 3 (station rate fans out)", withTariff)
	}
}

// EnergyVision-shaped table (2026-07-08 revision): location + address live on
// an aegi:entrance PointLocation (typed address lines, multilingual city), the
// brand stands in for the missing operator, and the doc root IS d2:payload.
const energyVisionTableXML = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:aegi="http://datex2.eu/schema/3/afirEnergyInfrastructure"
            xmlns:afac="http://datex2.eu/schema/3/afirFacilities"
            xmlns:com="http://datex2.eu/schema/3/common"
            xmlns:loc="http://datex2.eu/schema/3/locationReferencing"
            xmlns:locx="http://datex2.eu/schema/3/locationExtension"
            xsi:type="aegi:EnergyInfrastructureTablePublication"
            xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
            xmlns:d2="http://datex2.eu/schema/3/d2Payload">
  <aegi:energyInfrastructureTable id="T1">
    <aegi:energyInfrastructureSite id="SITE1">
      <aegi:typeOfSite>openSpace</aegi:typeOfSite>
      <aegi:brand><com:values><com:value lang="en">EnergyVision</com:value></com:values></aegi:brand>
      <aegi:entrance xsi:type="loc:PointLocation">
        <loc:_locationExtension>
          <loc:afirFacilityLocation>
            <afac:address>
              <locx:postcode>1000</locx:postcode>
              <locx:city><com:values><com:value lang="en">Brussel</com:value></com:values></locx:city>
              <locx:countryCode>BE</locx:countryCode>
              <locx:addressLine order="1">
                <locx:type>street</locx:type>
                <locx:text><com:values><com:value lang="en">Reper-Vrevenstraat</com:value></com:values></locx:text>
              </locx:addressLine>
              <locx:addressLine order="2">
                <locx:type>houseNumber</locx:type>
                <locx:text><com:values><com:value lang="en">30</com:value></com:values></locx:text>
              </locx:addressLine>
            </afac:address>
          </loc:afirFacilityLocation>
        </loc:_locationExtension>
        <loc:pointByCoordinates>
          <loc:pointCoordinates>
            <loc:latitude>50.89055</loc:latitude>
            <loc:longitude>4.341847</loc:longitude>
          </loc:pointCoordinates>
        </loc:pointByCoordinates>
      </aegi:entrance>
      <aegi:energyInfrastructureStation id="SITE1-station">
        <aegi:refillPoint xsi:type="aegi:ElectricChargingPoint" id="RP1">
          <aegi:currentType>ac</aegi:currentType>
          <aegi:connector>
            <aegi:connectorType>iec62196T2</aegi:connectorType>
            <aegi:maxPowerAtSocket>7400</aegi:maxPowerAtSocket>
          </aegi:connector>
        </aegi:refillPoint>
      </aegi:energyInfrastructureStation>
    </aegi:energyInfrastructureSite>
  </aegi:energyInfrastructureTable>
</d2:payload>`

func TestParseAFIRStaticEnergyVisionEntranceLocation(t *testing.T) {
	conns, _, err := ParseAFIRStatic("energyvision", []byte(energyVisionTableXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d connectors, want 1", len(conns))
	}
	c := conns[0]
	if c.Lat != 50.89055 || c.Lon != 4.341847 {
		t.Errorf("entrance coordinates not picked up: lat=%v lon=%v", c.Lat, c.Lon)
	}
	if c.PostalCode != "1000" || c.City != "Brussel" {
		t.Errorf("entrance address not picked up: postcode=%q city=%q", c.PostalCode, c.City)
	}
	if c.Address != "Reper-Vrevenstraat 30" {
		t.Errorf("street address = %q, want %q", c.Address, "Reper-Vrevenstraat 30")
	}
	if c.Name != "EnergyVision" {
		t.Errorf("name = %q, want brand fallback EnergyVision", c.Name)
	}
	if c.PowerKW != 7.4 || c.PlugType != "IEC_62196_T2" {
		t.Errorf("connector: %+v", c)
	}
}
