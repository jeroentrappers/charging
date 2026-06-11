package datex

import "testing"

// Realistic DATEX II v3 envelope with a default namespace, so the
// namespace-agnostic local-name matching is exercised.
const afirStaticXML = `<?xml version="1.0" encoding="UTF-8"?>
<messageContainer xmlns="http://datex2.eu/schema/3/messageContainer"
                  xmlns:egi="http://datex2.eu/schema/3/energyInfrastructure"
                  xmlns:com="http://datex2.eu/schema/3/common">
  <payload xsi:type="egi:EnergyInfrastructureTablePublication"
           xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <egi:energyInfrastructureTable id="T1">
      <egi:energyInfrastructureSite id="S1">
        <egi:name><com:values><com:value>Site Alpha</com:value></com:values></egi:name>
        <egi:locationReference>
          <com:pointByCoordinates>
            <com:pointCoordinates>
              <com:latitude>50.85</com:latitude>
              <com:longitude>4.35</com:longitude>
            </com:pointCoordinates>
          </com:pointByCoordinates>
          <com:_pointLocationExtension>
            <com:facilityLocation>
              <com:address>
                <com:postcode>1000</com:postcode>
                <com:city>Brussels</com:city>
              </com:address>
            </com:facilityLocation>
          </com:_pointLocationExtension>
        </egi:locationReference>
        <egi:operator><com:name><com:values><com:value>ACME</com:value></com:values></com:name></egi:operator>
        <egi:energyInfrastructureStation id="ST1">
          <egi:refillPoint id="RP-AC-1">
            <egi:connector>
              <egi:connectorType>iec62196T2</egi:connectorType>
              <egi:chargingMode>mode3AC3p</egi:chargingMode>
              <egi:maxPowerAtSocket>22000</egi:maxPowerAtSocket>
            </egi:connector>
            <egi:energyRate ratePolicy="adHoc">
              <egi:applicableCurrency>EUR</egi:applicableCurrency>
              <egi:energyPrice>
                <egi:priceType>pricePerKWh</egi:priceType>
                <egi:value>0.49</egi:value>
                <egi:taxIncluded>true</egi:taxIncluded>
                <egi:taxRate>21.0</egi:taxRate>
              </egi:energyPrice>
            </egi:energyRate>
            <egi:energyRate ratePolicy="contract">
              <egi:applicableCurrency>EUR</egi:applicableCurrency>
              <egi:energyPrice>
                <egi:priceType>pricePerKWh</egi:priceType>
                <egi:value>0.35</egi:value>
              </egi:energyPrice>
            </egi:energyRate>
          </egi:refillPoint>
        </egi:energyInfrastructureStation>
      </egi:energyInfrastructureSite>
      <egi:energyInfrastructureSite id="S2">
        <egi:name><com:values><com:value>Site Beta</com:value></com:values></egi:name>
        <egi:locationReference>
          <com:pointByCoordinates>
            <com:pointCoordinates>
              <com:latitude>51.21</com:latitude>
              <com:longitude>4.40</com:longitude>
            </com:pointCoordinates>
          </com:pointByCoordinates>
        </egi:locationReference>
        <egi:operator><com:name><com:values><com:value>ACME</com:value></com:values></com:name></egi:operator>
        <egi:energyInfrastructureStation id="ST2">
          <egi:refillPoint id="RP-DC-1">
            <egi:connector>
              <egi:connectorType>iec62196T2Combo</egi:connectorType>
              <egi:chargingMode>mode4</egi:chargingMode>
              <egi:maxPowerAtSocket>150000</egi:maxPowerAtSocket>
            </egi:connector>
            <egi:energyRate ratePolicy="adHoc">
              <egi:applicableCurrency>EUR</egi:applicableCurrency>
              <egi:energyPrice>
                <egi:priceType>pricePerKWh</egi:priceType>
                <egi:value>0.69</egi:value>
              </egi:energyPrice>
              <egi:energyPrice>
                <egi:priceType>pricePerMinute</egi:priceType>
                <egi:value>0.10</egi:value>
              </egi:energyPrice>
            </egi:energyRate>
          </egi:refillPoint>
        </egi:energyInfrastructureStation>
      </egi:energyInfrastructureSite>
    </egi:energyInfrastructureTable>
  </payload>
</messageContainer>`

const afirStatusXML = `<?xml version="1.0" encoding="UTF-8"?>
<messageContainer xmlns="http://datex2.eu/schema/3/messageContainer"
                  xmlns:egi="http://datex2.eu/schema/3/energyInfrastructure">
  <payload xsi:type="egi:EnergyInfrastructureStatusPublication"
           xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <egi:energyInfrastructureStatus id="ST1S">
      <egi:energyInfrastructureStationStatus>
        <egi:refillPointStatus>
          <egi:reference id="RP-AC-1" targetClass="egi:RefillPoint"/>
          <egi:status>occupied</egi:status>
          <egi:energyRateUpdate>
            <egi:applicableCurrency>EUR</egi:applicableCurrency>
            <egi:energyPrice>
              <egi:priceType>pricePerKWh</egi:priceType>
              <egi:value>0.55</egi:value>
            </egi:energyPrice>
          </egi:energyRateUpdate>
        </egi:refillPointStatus>
      </egi:energyInfrastructureStationStatus>
    </egi:energyInfrastructureStatus>
  </payload>
</messageContainer>`

func TestParseAFIRStatic(t *testing.T) {
	conns, tariffs, err := ParseAFIRStatic("cpo-de", []byte(afirStaticXML))
	if err != nil {
		t.Fatalf("ParseAFIRStatic: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("want 2 connectors, got %d", len(conns))
	}

	byID := map[string]int{}
	for i, c := range conns {
		byID[c.EVSEUID] = i
	}

	ac := conns[byID["RP-AC-1"]]
	if ac.PlugType != "IEC_62196_T2" {
		t.Errorf("AC plug = %q, want IEC_62196_T2", ac.PlugType)
	}
	if ac.CurrentType != "AC" {
		t.Errorf("AC current = %q, want AC", ac.CurrentType)
	}
	if ac.PowerKW != 22.0 {
		t.Errorf("AC power = %v, want 22", ac.PowerKW)
	}
	if ac.ConnectorID != "1" {
		t.Errorf("AC connectorID = %q, want 1", ac.ConnectorID)
	}
	if ac.TariffID != "RP-AC-1" {
		t.Errorf("AC tariffID = %q, want RP-AC-1", ac.TariffID)
	}

	dc := conns[byID["RP-DC-1"]]
	if dc.PlugType != "IEC_62196_T2_COMBO" {
		t.Errorf("DC plug = %q, want IEC_62196_T2_COMBO", dc.PlugType)
	}
	if dc.CurrentType != "DC" {
		t.Errorf("DC current = %q, want DC", dc.CurrentType)
	}
	if dc.PowerKW != 150.0 {
		t.Errorf("DC power = %v, want 150", dc.PowerKW)
	}

	// AC tariff: ad-hoc ENERGY 0.49 (not the 0.35 contract rate).
	tAC, ok := tariffs["RP-AC-1"]
	if !ok {
		t.Fatalf("no tariff for RP-AC-1")
	}
	if tAC.Currency != "EUR" {
		t.Errorf("AC currency = %q", tAC.Currency)
	}
	if len(tAC.Elements) != 1 || len(tAC.Elements[0].PriceComponents) != 1 {
		t.Fatalf("AC tariff shape wrong: %+v", tAC.Elements)
	}
	pc := tAC.Elements[0].PriceComponents[0]
	if pc.Type != "ENERGY" || pc.Price != 0.49 {
		t.Errorf("AC component = %+v, want ENERGY/0.49", pc)
	}

	// DC tariff: ENERGY 0.69 + TIME = 0.10*60 = 6.0.
	tDC := tariffs["RP-DC-1"]
	comps := tDC.Elements[0].PriceComponents
	if len(comps) != 2 {
		t.Fatalf("DC components = %d, want 2", len(comps))
	}
	var gotEnergy, gotTime bool
	for _, c := range comps {
		switch c.Type {
		case "ENERGY":
			gotEnergy = true
			if c.Price != 0.69 {
				t.Errorf("DC energy = %v, want 0.69", c.Price)
			}
		case "TIME":
			gotTime = true
			if c.Price != 6.0 {
				t.Errorf("DC time = %v, want 6.0", c.Price)
			}
		}
	}
	if !gotEnergy || !gotTime {
		t.Errorf("DC missing components: energy=%v time=%v", gotEnergy, gotTime)
	}
}

func TestParseAFIRStatus(t *testing.T) {
	statuses, err := ParseAFIRStatus([]byte(afirStatusXML))
	if err != nil {
		t.Fatalf("ParseAFIRStatus: %v", err)
	}
	s, ok := statuses["RP-AC-1"]
	if !ok {
		t.Fatalf("no status for RP-AC-1, got keys %v", statuses)
	}
	if s.Status != "CHARGING" {
		t.Errorf("status = %q, want CHARGING (occupied)", s.Status)
	}
	if s.Tariff == nil {
		t.Fatalf("expected live tariff update")
	}
	if s.Tariff.OCPIID != "RP-AC-1" {
		t.Errorf("tariff OCPIID = %q, want RP-AC-1", s.Tariff.OCPIID)
	}
	pc := s.Tariff.Elements[0].PriceComponents[0]
	if pc.Type != "ENERGY" || pc.Price != 0.55 {
		t.Errorf("live component = %+v, want ENERGY/0.55", pc)
	}
}
