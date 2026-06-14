package datex

import "testing"

// A compact, namespace-prefixed DATEX II XML table publication (one AC Type 2
// @11 kW, ad-hoc €0.50/kWh) — the encoding LISY / municipal aggregators and
// some brokers push. ParseAFIR must sniff XML and route it through the XML path.
const xmlTablePush = `<?xml version="1.0"?>
<ns2:messageContainer xmlns:ns2="http://datex2.eu/schema/3/messageContainer" xmlns:ns11="http://datex2.eu/schema/3/energyInfrastructure">
<ns2:payload xsi:type="ns11:EnergyInfrastructureTablePublication" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <publicationCreator><country>de</country><nationalIdentifier>DELND</nationalIdentifier></publicationCreator>
  <ns11:energyInfrastructureTable id="t1">
    <ns11:energyInfrastructureSite id="s1">
      <name><values><value lang="de">Marktplatz</value></values></name>
      <locationReference><pointByCoordinates><pointCoordinates>
        <latitude>50.94</latitude><longitude>6.95</longitude>
      </pointCoordinates></pointByCoordinates></locationReference>
      <operator><name><values><value lang="de">Stadtwerke Musterstadt</value></values></name></operator>
      <ns11:energyInfrastructureStation>
        <refillPoint id="cp-1">
          <connector><connectorType>iec62196T2</connectorType><maxPowerAtSocket>11000</maxPowerAtSocket></connector>
          <energyRate><ratePolicy>adHoc</ratePolicy><applicableCurrency>EUR</applicableCurrency>
            <energyPrice><priceType>pricePerKWh</priceType><value>0.50</value></energyPrice>
          </energyRate>
        </refillPoint>
      </ns11:energyInfrastructureStation>
    </ns11:energyInfrastructureSite>
  </ns11:energyInfrastructureTable>
</ns2:payload></ns2:messageContainer>`

const xmlStatusPush = `<?xml version="1.0"?>
<ns2:messageContainer xmlns:ns2="http://datex2.eu/schema/3/messageContainer" xmlns:ns11="http://datex2.eu/schema/3/energyInfrastructure">
<ns2:payload xsi:type="ns11:EnergyInfrastructureStatusPublication" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <publicationCreator><country>de</country><nationalIdentifier>DELND</nationalIdentifier></publicationCreator>
  <ns11:energyInfrastructureStatus>
    <ns11:energyInfrastructureStationStatus>
      <refillPointStatus><reference id="cp-1" targetClass="ElectricChargingPoint"/><status>available</status></refillPointStatus>
    </ns11:energyInfrastructureStationStatus>
  </ns11:energyInfrastructureStatus>
</ns2:payload></ns2:messageContainer>`

func TestParseAFIR_XML(t *testing.T) {
	// Table: routed to XML, creator + operator + a priced connector extracted.
	doc, err := ParseAFIR([]byte(xmlTablePush))
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	if doc.Kind != "table" {
		t.Fatalf("kind=%q want table", doc.Kind)
	}
	if doc.Creator.NationalIdentifier != "DELND" {
		t.Errorf("creator=%q want DELND", doc.Creator.NationalIdentifier)
	}
	if doc.Operator != "Stadtwerke Musterstadt" {
		t.Errorf("operator=%q", doc.Operator)
	}
	if len(doc.Connectors) != 1 {
		t.Fatalf("connectors=%d want 1", len(doc.Connectors))
	}
	c := doc.Connectors[0]
	if c.EVSEUID != "cp-1" || c.PlugType != "IEC_62196_T2" || c.PowerKW != 11 || c.TariffID == "" {
		t.Errorf("connector=%+v", c)
	}
	if _, ok := doc.Tariffs[c.TariffID]; !ok {
		t.Errorf("tariff %q missing", c.TariffID)
	}

	// Status: routed to XML, mapped to our vocab.
	sdoc, err := ParseAFIR([]byte(xmlStatusPush))
	if err != nil || sdoc.Kind != "status" {
		t.Fatalf("status: kind=%q err=%v", sdoc.Kind, err)
	}
	if len(sdoc.Statuses) != 1 || sdoc.Statuses[0].EVSEUID != "cp-1" || sdoc.Statuses[0].Status != "AVAILABLE" {
		t.Errorf("statuses=%+v", sdoc.Statuses)
	}

	// JSON still routes to the JSON path (regression guard).
	jdoc, err := ParseAFIR([]byte(mobJSONProbe))
	if err != nil || jdoc.Kind != "table" {
		t.Fatalf("json route: kind=%q err=%v", jdoc.Kind, err)
	}
}

// minimal JSON table (aegi… encoding) to prove ParseAFIR still routes JSON.
const mobJSONProbe = `{"payload":{"aegiEnergyInfrastructureTablePublication":{"publicationCreator":{"country":"DE","nationalIdentifier":"DE-NAP-X"},"energyInfrastructureTable":[{"idG":"t","energyInfrastructureSite":[{"idG":"s","locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.9,"longitude":6.9}}},"energyInfrastructureStation":[{"idG":"st","refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp","currentType":{"value":"ac"},"connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":11000}]}}]}]}]}]}}}`

// Station-level location/operator (e.g. Grid and Co.): the site carries no
// coordinates; they live on energyInfrastructureStation. The builder must fall
// through to the station rather than skip the site.
const jsonStationLevel = `{"payload":{"aegiEnergyInfrastructureTablePublication":{
"publicationCreator":{"country":"de","nationalIdentifier":"Grid and Co."},
"energyInfrastructureTable":[{"idG":"t","energyInfrastructureSite":[{"idG":"s",
"energyInfrastructureStation":[{"idG":"st","totalMaximumPower":22000,
"locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":52.5,"longitude":13.4}}},
"operator":{"afacAnOrganisation":{"name":{"values":[{"value":"Grid and Co."}]}}},
"refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-9","currentType":{"value":"ac"},
"connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":22000}],
"electricEnergy":[{"energyRate":[{"idG":"r","ratePolicy":{"value":"adHoc"},"applicableCurrency":["EUR"],
"energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.59}]}]}]}}]}]}]}]}}}`

func TestParseAFIRJSON_StationLevelLocation(t *testing.T) {
	doc, err := ParseAFIR([]byte(jsonStationLevel))
	if err != nil || doc.Kind != "table" {
		t.Fatalf("kind=%q err=%v", doc.Kind, err)
	}
	if len(doc.Connectors) != 1 {
		t.Fatalf("connectors=%d want 1 (station-level coords must not be skipped)", len(doc.Connectors))
	}
	c := doc.Connectors[0]
	if c.Lat != 52.5 || c.Lon != 13.4 || c.PowerKW != 22 || c.TariffID == "" {
		t.Errorf("connector=%+v", c)
	}
	if doc.Operator != "Grid and Co." {
		t.Errorf("operator=%q want Grid and Co.", doc.Operator)
	}
}
