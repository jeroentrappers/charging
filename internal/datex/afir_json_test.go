package datex

import (
	"testing"

	"github.com/appmire/charging/internal/model"
)

// ---- TABLE fixtures -----------------------------------------------------

// Two sites: an AC iec62196T2 @11kW with adHoc pricePerKWh 0.55, and a DC
// iec62196T2Combo @150kW (no price).
const afirTableJSON = `{"payload":{"modelBaseVersionG":"3","profileNameG":"AFIR Energy Infrastructure","profileVersionG":"01-00-00",
 "aegiEnergyInfrastructureTablePublication":{
   "lang":"en","publicationTime":"2026-06-12T00:00:15.717Z",
   "publicationCreator":{"country":"DE","nationalIdentifier":"DE-NAP-GPJOULECONNECT"},
   "energyInfrastructureTable":[{"idG":"gp-joule-connect-table-1","versionG":"1","tableName":"GP JOULE CONNECT Charging Locations",
     "energyInfrastructureSite":[
       {"idG":"site-24699","versionG":"1","typeOfSite":{"value":"onstreet"},
        "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":54.586,"longitude":8.9905},
          "locLocationExtensionG":{"FacilityLocation":{"address":{"postcode":"25821",
            "city":{"values":[{"lang":"en","value":"Struckum"}]},"countryCode":"DE",
            "addressLine":[{"order":0,"type":{"value":"street"},"text":{"values":[{"lang":"en","value":"Kennedy Weg 4"}]}}]}}}}},
        "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"GP JOULE CONNECT"}]}}},
        "energyInfrastructureStation":[{"idG":"station-34875","versionG":"1","totalMaximumPower":11000,"numberOfRefillPoints":1,
          "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-DE*CNT*EP90046*002*1-1","versionG":"1",
            "externalIdentifier":[{"identifier":"DE*CNT*EP90046*002*1","typeOfIdentifier":{"value":"extendedG","extendedValueG":"evseId"}}],
            "deliveryUnit":{"value":"kWh"},"currentType":{"value":"ac"},
            "connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":11000,"connectorFormat":{"value":"socket"},"voltage":400,"maximumCurrent":16}],
            "electricEnergy":[{"energyRate":[{"idG":"energy-rate-d011","ratePolicy":{"value":"adHoc"},"applicableCurrency":["EUR"],
              "energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.55,"taxIncluded":true,"taxRate":19}]}]}]}}]}]},
       {"idG":"site-9000","versionG":"1","typeOfSite":{"value":"onstreet"},
        "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":52.52,"longitude":13.405},
          "locLocationExtensionG":{"FacilityLocation":{"address":{"postcode":"10115",
            "city":{"values":[{"lang":"en","value":"Berlin"}]},"countryCode":"DE",
            "addressLine":[{"order":0,"type":{"value":"street"},"text":{"values":[{"lang":"en","value":"Hauptstrasse 1"}]}}]}}}}},
        "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"GP JOULE CONNECT"}]}}},
        "energyInfrastructureStation":[{"idG":"station-9001","versionG":"1","totalMaximumPower":150000,"numberOfRefillPoints":1,
          "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-DC-1","versionG":"1",
            "deliveryUnit":{"value":"kWh"},"currentType":{"value":"dc"},
            "connector":[{"connectorType":{"value":"iec62196T2Combo"},"maxPowerAtSocket":150000,"connectorFormat":{"value":"cable"},"voltage":400,"maximumCurrent":375}]}}]}]}
     ]}]}},
 "exchangeInformation":{}}`

func parseTbl(t *testing.T, s string) *AFIRDoc {
	t.Helper()
	doc, err := ParseAFIRJSON([]byte(s))
	if err != nil {
		t.Fatalf("ParseAFIRJSON error: %v", err)
	}
	return doc
}

func findConn(doc *AFIRDoc, evseUID, connID string) *model.Connector {
	for i := range doc.Connectors {
		if doc.Connectors[i].EVSEUID == evseUID && doc.Connectors[i].ConnectorID == connID {
			return &doc.Connectors[i]
		}
	}
	return nil
}

func TestParseAFIRJSON_Table(t *testing.T) {
	doc := parseTbl(t, afirTableJSON)

	if doc.Kind != "table" {
		t.Fatalf("Kind = %q, want table", doc.Kind)
	}
	if doc.Creator.Country != "DE" || doc.Creator.NationalIdentifier != "DE-NAP-GPJOULECONNECT" {
		t.Fatalf("Creator = %+v", doc.Creator)
	}
	if len(doc.Connectors) != 2 {
		t.Fatalf("connector count = %d, want 2", len(doc.Connectors))
	}

	ac := findConn(doc, "cp-DE*CNT*EP90046*002*1-1", "1")
	if ac == nil {
		t.Fatal("AC connector not found")
	}
	if ac.CPOID != "" {
		t.Errorf("CPOID should be empty, got %q", ac.CPOID)
	}
	if ac.Lat != 54.586 || ac.Lon != 8.9905 {
		t.Errorf("AC coords = %v,%v", ac.Lat, ac.Lon)
	}
	if ac.PlugType != "IEC_62196_T2" {
		t.Errorf("AC PlugType = %q", ac.PlugType)
	}
	if ac.CurrentType != model.CurrentAC {
		t.Errorf("AC CurrentType = %q", ac.CurrentType)
	}
	if ac.PowerKW != 11 {
		t.Errorf("AC PowerKW = %v, want 11", ac.PowerKW)
	}
	if ac.PostalCode != "25821" || ac.City != "Struckum" {
		t.Errorf("AC address = %q %q", ac.PostalCode, ac.City)
	}
	if ac.Address != "Kennedy Weg 4" {
		t.Errorf("AC street = %q", ac.Address)
	}
	if ac.Name != "GP JOULE CONNECT · Kennedy Weg 4" {
		t.Errorf("AC Name = %q", ac.Name)
	}
	if ac.EVSEStatus != "" {
		t.Errorf("AC EVSEStatus should be empty, got %q", ac.EVSEStatus)
	}
	if ac.TariffID != "energy-rate-d011" {
		t.Errorf("AC TariffID = %q", ac.TariffID)
	}
	tar, ok := doc.Tariffs["energy-rate-d011"]
	if !ok {
		t.Fatal("tariff energy-rate-d011 not in map")
	}
	if tar.OCPIID != "energy-rate-d011" || tar.Currency != "EUR" {
		t.Errorf("tariff meta = %+v", tar)
	}
	if len(tar.Elements) != 1 || len(tar.Elements[0].PriceComponents) != 1 {
		t.Fatalf("tariff elements = %+v", tar.Elements)
	}
	pc := tar.Elements[0].PriceComponents[0]
	if pc.Type != "ENERGY" || pc.Price != 0.55 {
		t.Errorf("price component = %+v", pc)
	}

	dc := findConn(doc, "cp-DC-1", "1")
	if dc == nil {
		t.Fatal("DC connector not found")
	}
	if dc.PlugType != "IEC_62196_T2_COMBO" {
		t.Errorf("DC PlugType = %q", dc.PlugType)
	}
	if dc.CurrentType != model.CurrentDC {
		t.Errorf("DC CurrentType = %q", dc.CurrentType)
	}
	if dc.PowerKW != 150 {
		t.Errorf("DC PowerKW = %v, want 150", dc.PowerKW)
	}
	if dc.TariffID != "" {
		t.Errorf("DC TariffID should be empty, got %q", dc.TariffID)
	}
}

// Multiple connectors in one refillPoint → ConnectorID "1","2".
const afirMultiConnJSON = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
 "aegiEnergyInfrastructureTablePublication":{"lang":"en","publicationTime":"x",
   "publicationCreator":{"country":"DE","nationalIdentifier":"NAP"},
   "energyInfrastructureTable":[{"idG":"t","tableName":"T",
     "energyInfrastructureSite":[{"idG":"s","typeOfSite":{"value":"onstreet"},
       "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.0,"longitude":4.0}}},
       "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"Op"}]}}},
       "energyInfrastructureStation":[{"idG":"st","totalMaximumPower":22000,
         "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-multi","currentType":{"value":"ac"},
           "connector":[
             {"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":11000},
             {"connectorType":{"value":"iec62196T1"},"maxPowerAtSocket":22000}
           ]}}]}]}]}]}}}`

func TestParseAFIRJSON_MultiConnector(t *testing.T) {
	doc := parseTbl(t, afirMultiConnJSON)
	if len(doc.Connectors) != 2 {
		t.Fatalf("connectors = %d, want 2", len(doc.Connectors))
	}
	c1 := findConn(doc, "cp-multi", "1")
	c2 := findConn(doc, "cp-multi", "2")
	if c1 == nil || c2 == nil {
		t.Fatalf("missing connector IDs: %+v", doc.Connectors)
	}
	if c1.PowerKW != 11 || c2.PowerKW != 22 {
		t.Errorf("powers = %v %v", c1.PowerKW, c2.PowerKW)
	}
	if c1.PlugType != "IEC_62196_T2" || c2.PlugType != "IEC_62196_T1" {
		t.Errorf("plugs = %q %q", c1.PlugType, c2.PlugType)
	}
}

// pricePerMinute (×60 → TIME), flatRate (→ FLAT), multiple components.
const afirPriceVariantsJSON = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
 "aegiEnergyInfrastructureTablePublication":{"lang":"en","publicationTime":"x",
   "publicationCreator":{"country":"DE","nationalIdentifier":"NAP"},
   "energyInfrastructureTable":[{"idG":"t","tableName":"T",
     "energyInfrastructureSite":[{"idG":"s","typeOfSite":{"value":"onstreet"},
       "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.0,"longitude":4.0}}},
       "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"Op"}]}}},
       "energyInfrastructureStation":[{"idG":"st","totalMaximumPower":22000,
         "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-px","currentType":{"value":"ac"},
           "connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":22000}],
           "electricEnergy":[{"energyRate":[{"idG":"rate-px","ratePolicy":{"value":"adHoc"},"applicableCurrency":["EUR"],
             "energyPrice":[
               {"priceType":{"value":"pricePerKWh"},"value":0.40},
               {"priceType":{"value":"pricePerMinute"},"value":0.05},
               {"priceType":{"value":"flatRate"},"value":1.0}
             ]}]}]}}]}]}]}]}}}`

func TestParseAFIRJSON_PriceVariants(t *testing.T) {
	doc := parseTbl(t, afirPriceVariantsJSON)
	tar, ok := doc.Tariffs["rate-px"]
	if !ok {
		t.Fatal("rate-px not in tariff map")
	}
	pcs := tar.Elements[0].PriceComponents
	if len(pcs) != 3 {
		t.Fatalf("components = %d, want 3: %+v", len(pcs), pcs)
	}
	want := map[string]float64{"ENERGY": 0.40, "TIME": 3.0, "FLAT": 1.0}
	for _, pc := range pcs {
		w, ok := want[pc.Type]
		if !ok {
			t.Errorf("unexpected component type %q", pc.Type)
			continue
		}
		if pc.Price != w {
			t.Errorf("%s price = %v, want %v", pc.Type, pc.Price, w)
		}
		delete(want, pc.Type)
	}
	if len(want) != 0 {
		t.Errorf("missing components: %v", want)
	}
}

// refillPoint with no energyRate → connector emitted, TariffID "", not in map.
const afirNoRateJSON = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
 "aegiEnergyInfrastructureTablePublication":{"lang":"en","publicationTime":"x",
   "publicationCreator":{"country":"DE","nationalIdentifier":"NAP"},
   "energyInfrastructureTable":[{"idG":"t","tableName":"T",
     "energyInfrastructureSite":[{"idG":"s","typeOfSite":{"value":"onstreet"},
       "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.0,"longitude":4.0}}},
       "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"Op"}]}}},
       "energyInfrastructureStation":[{"idG":"st","totalMaximumPower":11000,
         "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-norate","currentType":{"value":"ac"},
           "connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":11000}]}}]}]}]}]}}}`

func TestParseAFIRJSON_NoRate(t *testing.T) {
	doc := parseTbl(t, afirNoRateJSON)
	if len(doc.Connectors) != 1 {
		t.Fatalf("connectors = %d, want 1", len(doc.Connectors))
	}
	if doc.Connectors[0].TariffID != "" {
		t.Errorf("TariffID = %q, want empty", doc.Connectors[0].TariffID)
	}
	if len(doc.Tariffs) != 0 {
		t.Errorf("Tariffs should be empty, got %+v", doc.Tariffs)
	}
}

// Connector with zero maxPowerAtSocket falls back to station totalMaximumPower.
const afirPowerFallbackJSON = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
 "aegiEnergyInfrastructureTablePublication":{"lang":"en","publicationTime":"x",
   "publicationCreator":{"country":"DE","nationalIdentifier":"NAP"},
   "energyInfrastructureTable":[{"idG":"t","tableName":"T",
     "energyInfrastructureSite":[{"idG":"s","typeOfSite":{"value":"onstreet"},
       "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.0,"longitude":4.0}}},
       "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"Op"}]}}},
       "energyInfrastructureStation":[{"idG":"st","totalMaximumPower":50000,
         "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-fb","currentType":{"value":"dc"},
           "connector":[{"connectorType":{"value":"chademo"}}]}}]}]}]}]}}}`

func TestParseAFIRJSON_PowerFallback(t *testing.T) {
	doc := parseTbl(t, afirPowerFallbackJSON)
	if len(doc.Connectors) != 1 {
		t.Fatalf("connectors = %d", len(doc.Connectors))
	}
	if doc.Connectors[0].PowerKW != 50 {
		t.Errorf("PowerKW = %v, want 50 (station fallback)", doc.Connectors[0].PowerKW)
	}
}

// Site with missing coordinates → skipped.
const afirNoCoordsJSON = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
 "aegiEnergyInfrastructureTablePublication":{"lang":"en","publicationTime":"x",
   "publicationCreator":{"country":"DE","nationalIdentifier":"NAP"},
   "energyInfrastructureTable":[{"idG":"t","tableName":"T",
     "energyInfrastructureSite":[{"idG":"s","typeOfSite":{"value":"onstreet"},
       "locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":0,"longitude":0}}},
       "operator":{"afacAnOrganisation":{"name":{"values":[{"lang":"en","value":"Op"}]}}},
       "energyInfrastructureStation":[{"idG":"st","totalMaximumPower":11000,
         "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-x","currentType":{"value":"ac"},
           "connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":11000}]}}]}]}]}]}}}`

func TestParseAFIRJSON_MissingCoords(t *testing.T) {
	doc := parseTbl(t, afirNoCoordsJSON)
	if doc.Kind != "table" {
		t.Fatalf("Kind = %q", doc.Kind)
	}
	if len(doc.Connectors) != 0 {
		t.Errorf("connectors = %d, want 0 (site skipped)", len(doc.Connectors))
	}
}

// ---- STATUS fixtures ----------------------------------------------------

const afirStatusJSON = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
 "aegiEnergyInfrastructureStatusPublication":{
   "lang":"en","publicationTime":"2026-06-12T09:22:04Z",
   "publicationCreator":{"country":"DE","nationalIdentifier":"DE-NAP-GPJOULECONNECT"},
   "energyInfrastructureSiteStatus":[{"lastUpdated":"2026-06-12T09:22:00Z","reference":{"targetClass":"FacilityObject","idG":"site-24699"},
     "energyInfrastructureStationStatus":[{"reference":{"idG":"station-34875"},
       "refillPointStatus":[
         {"aegiElectricChargingPointStatus":{"reference":{"targetClass":"FacilityObject","idG":"cp-avail"},
           "status":{"value":"available"},
           "energyRateUpdate":[{"energyRateReference":{"idG":"energy-rate-d011"},"applicableCurrency":["EUR"],"ratePolicy":{"value":"adHoc"},
             "energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.55,"taxIncluded":true,"taxRate":19}]}]}},
         {"aegiElectricChargingPointStatus":{"reference":{"idG":"cp-blocked"},"status":{"value":"blocked"}}},
         {"aegiElectricChargingPointStatus":{"reference":{"idG":"cp-charging"},"status":{"value":"charging"}}},
         {"aegiElectricChargingPointStatus":{"reference":{"idG":"cp-weird"},"status":{"value":"someUnknownValue"}}}
       ]}]}]}},
 "exchangeInformation":{}}`

func findStatus(doc *AFIRDoc, uid string) *AFIRStatusUpdate {
	for i := range doc.Statuses {
		if doc.Statuses[i].EVSEUID == uid {
			return &doc.Statuses[i]
		}
	}
	return nil
}

func TestParseAFIRJSON_Status(t *testing.T) {
	doc, err := ParseAFIRJSON([]byte(afirStatusJSON))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if doc.Kind != "status" {
		t.Fatalf("Kind = %q, want status", doc.Kind)
	}
	if doc.Creator.Country != "DE" || doc.Creator.NationalIdentifier != "DE-NAP-GPJOULECONNECT" {
		t.Fatalf("Creator = %+v", doc.Creator)
	}
	if len(doc.Statuses) != 4 {
		t.Fatalf("statuses = %d, want 4", len(doc.Statuses))
	}

	checks := map[string]string{
		"cp-avail":    "AVAILABLE",
		"cp-blocked":  "OUTOFORDER",
		"cp-charging": "CHARGING",
		"cp-weird":    "UNKNOWN",
	}
	for uid, want := range checks {
		s := findStatus(doc, uid)
		if s == nil {
			t.Errorf("status %s missing", uid)
			continue
		}
		if s.Status != want {
			t.Errorf("%s status = %q, want %q", uid, s.Status, want)
		}
	}

	av := findStatus(doc, "cp-avail")
	if av.Tariff == nil {
		t.Fatal("cp-avail tariff should be non-nil")
	}
	if av.Tariff.OCPIID != "energy-rate-d011" || av.Tariff.Currency != "EUR" {
		t.Errorf("cp-avail tariff meta = %+v", av.Tariff)
	}
	pc := av.Tariff.Elements[0].PriceComponents[0]
	if pc.Type != "ENERGY" || pc.Price != 0.55 {
		t.Errorf("cp-avail price = %+v", pc)
	}

	if blk := findStatus(doc, "cp-blocked"); blk.Tariff != nil {
		t.Errorf("cp-blocked tariff should be nil, got %+v", blk.Tariff)
	}
}

// ---- edge cases ---------------------------------------------------------

// Synthetic test packet: payload is an ARRAY of commonGenericPublication.
const afirTestArrayJSON = `{"payload":[
  {"commonGenericPublication":{"lang":"en","publicationTime":"2026-06-12T00:00:00Z"}},
  {"commonGenericPublication":{"lang":"en"}}
 ],"exchangeInformation":{}}`

func TestParseAFIRJSON_SyntheticTestArray(t *testing.T) {
	doc, err := ParseAFIRJSON([]byte(afirTestArrayJSON))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if doc.Kind != "" {
		t.Errorf("Kind = %q, want empty", doc.Kind)
	}
	if len(doc.Connectors) != 0 || len(doc.Statuses) != 0 {
		t.Errorf("expected empty connectors/statuses, got %d/%d", len(doc.Connectors), len(doc.Statuses))
	}
}

func TestParseAFIRJSON_Malformed(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{`,
		`{"payload":`,
	}
	for _, c := range cases {
		doc, err := ParseAFIRJSON([]byte(c))
		if err == nil && doc != nil && doc.Kind != "" {
			t.Errorf("input %q: expected error or empty Kind, got Kind=%q", c, doc.Kind)
		}
		// must not panic; reaching here is success.
	}
}

func TestParseAFIRJSON_EmptyPayload(t *testing.T) {
	doc, err := ParseAFIRJSON([]byte(`{}`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if doc.Kind != "" {
		t.Errorf("Kind = %q, want empty", doc.Kind)
	}
}
