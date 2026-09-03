package datex

import "testing"

// Shape of the Belgian NAP feed (nap-be.eco-movement.com): NO envelope — both
// publications sit at the document root — coordinates and address on the site's
// `entrance`, the CPO on `energyDistributor` rather than `operator`, the roaming
// EVSE id only on the connector's externalIdentifier, and net prices whose
// taxRate is written as a percentage on one point and a fraction on the next.
const afirNAPJSON = `{"modelBaseVersionG":"3","profileNameG":"AFIR Energy Infrastructure","profileVersionG":"01-00-00",
 "aegiEnergyInfrastructureTablePublication":{
   "lang":"en","publicationTime":"2026-09-03T22:43:57Z",
   "publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
   "energyInfrastructureTable":[{"idG":"019c04a9-93ed-7df9-88fc-14db9d22060d","versionG":"1",
     "energyInfrastructureSite":[
       {"idG":"Eneco-9E733663","versionG":"1",
        "brand":{"values":[{"lang":"be","value":"Eneco eMobility HOOGSTRAAT 24 C"}]},
        "entrance":[{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.78419,"longitude":3.35766},
          "locLocationExtensionG":{"FacilityLocation":{"timeZone":"Europe/Brussels","address":{"postcode":"8554",
            "city":{"values":[{"lang":"be","value":"SINT-DENIJS"}]},"countryCode":"BE",
            "addressLine":[{"order":0,"type":{"value":"street"},"text":{"values":[{"lang":"be","value":"HOOGSTRAAT 24 C"}]}}]}}}}}],
        "energyDistributor":{"afacAnOrganisation":{"name":{"values":[{"lang":"be","value":"Eneco"}]}}},
        "energyInfrastructureStation":[{"idG":"9E733663","versionG":"1","totalMaximumPower":22000,"numberOfRefillPoints":1,
          "energyDistributor":{"afacAnOrganisation":{"name":{"values":[{"lang":"be","value":"Eneco"}]}}},
          "refillPoint":[{"aegiElectricChargingPoint":{"idG":"BE-ENE-EENECO_G44971-1","versionG":"1",
            "deliveryUnit":{"value":"kWh"},"currentType":{"value":"ac"},"numberOfConnectors":1,
            "connector":[{"externalIdentifier":[{"identifier":"BE*ENE*EENECO*G44971*1",
                "typeOfIdentifier":{"value":"extendedG","extendedValueG":"evseId"}}],
              "connectorType":{"value":"iec62196T2"},"connectorFormat":{"value":"cableMode3"},
              "maxPowerAtSocket":22000,"voltage":230,"maximumCurrent":32}],
            "energyProduct":[{"aegiElectricEnergy":{}}]}}]}]},
       {"idG":"Allego-0012951","versionG":"1",
        "entrance":[{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.86486,"longitude":4.34946},
          "locLocationExtensionG":{"FacilityLocation":{"address":{"postcode":"1000",
            "city":{"values":[{"lang":"be","value":"Brussels"}]},"countryCode":"BE",
            "addressLine":[{"order":0,"type":{"value":"street"},"text":{"values":[{"lang":"be","value":"Havenlaan 1"}]}}]}}}}}],
        "energyDistributor":{"afacAnOrganisation":{"name":{"values":[{"lang":"be","value":"Allego"}]}}},
        "energyInfrastructureStation":[{"idG":"st-0012951","versionG":"1","totalMaximumPower":150000,"numberOfRefillPoints":1,
          "refillPoint":[{"aegiElectricChargingPoint":{"idG":"BE-ALL-0012951","versionG":"1",
            "deliveryUnit":{"value":"kWh"},"currentType":{"value":"dc"},"numberOfConnectors":1,
            "connector":[{"externalIdentifier":[{"identifier":"BEALLEGO0012951",
                "typeOfIdentifier":{"value":"extendedG","extendedValueG":"evseId"}}],
              "connectorType":{"value":"iec62196T2Combo"},"connectorFormat":{"value":"cable"},
              "maxPowerAtSocket":150000,"voltage":400,"maximumCurrent":375}]}}]}]}
     ]}]},
 "aegiEnergyInfrastructureStatusPublication":{
   "lang":"en","publicationTime":"2026-09-03T22:43:57Z",
   "publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
   "tableReference":[{"targetClass":"EnergyInfrastructureTable","idG":"019c04a9-93ed-7df9-88fc-14db9d22060d","versionG":"1"}],
   "energyInfrastructureSiteStatus":[
     {"reference":{"targetClass":"FacilityObject","idG":"Eneco-9E733663","versionG":"1"},
      "energyInfrastructureStationStatus":[{"reference":{"idG":"9E733663"},
        "refillPointStatus":[{"aegiElectricChargingPointStatus":{
          "reference":{"targetClass":"FacilityObject","idG":"BE-ENE-EENECO_G44971-1","versionG":"1"},
          "lastUpdated":"2026-09-03T15:02:35Z","status":{"value":"available"},
          "energyRateUpdate":[{"energyRateReference":{"targetClass":"EnergyRate","idG":"CREGWPVG"},
            "energyPrice":[{"priceGroupIndex":0,"priceType":{"value":"pricePerKWh"},"value":0.3722,"taxIncluded":false,"taxRate":21}]}]}}]}]},
     {"reference":{"targetClass":"FacilityObject","idG":"Allego-0012951","versionG":"1"},
      "energyInfrastructureStationStatus":[{"reference":{"idG":"st-0012951"},
        "refillPointStatus":[{"aegiElectricChargingPointStatus":{
          "reference":{"targetClass":"FacilityObject","idG":"BE-ALL-0012951","versionG":"1"},
          "status":{"value":"charging"},
          "energyRateUpdate":[{"energyRateReference":{"idG":"ALLEGO-DC"},
            "energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.60,"taxIncluded":false,"taxRate":0.21},
                           {"priceType":{"value":"pricePerMinute"},"value":0.10,"taxIncluded":false,"taxRate":0.21},
                           {"priceType":{"value":"flatRate"},"value":0.50,"taxIncluded":true,"taxRate":21}]}]}}]}]}
   ]}}`

func TestParseAFIRJSON_NAP_RootEnvelopeCarriesBothPublications(t *testing.T) {
	doc := parseTbl(t, afirNAPJSON)

	if doc.Kind != "table" { // a combined document reports as its table
		t.Fatalf("Kind = %q, want table", doc.Kind)
	}
	if got, want := len(doc.Connectors), 2; got != want {
		t.Fatalf("connectors = %d, want %d", got, want)
	}
	if got, want := len(doc.Statuses), 2; got != want {
		t.Fatalf("statuses = %d, want %d", got, want)
	}
	if doc.Creator.NationalIdentifier != "NL-EC0" {
		t.Errorf("creator = %+v, want NL-EC0", doc.Creator)
	}
}

func TestParseAFIRJSON_NAP_EntranceLocationAndDistributor(t *testing.T) {
	doc := parseTbl(t, afirNAPJSON)

	c := findConn(doc, "BE-ENE-EENECO_G44971-1", "1")
	if c == nil {
		t.Fatal("connector not found — entrance coordinates not read?")
	}
	if c.Lat != 50.78419 || c.Lon != 3.35766 {
		t.Errorf("coords = %v,%v want 50.78419,3.35766", c.Lat, c.Lon)
	}
	if c.PostalCode != "8554" || c.City != "SINT-DENIJS" || c.Address != "HOOGSTRAAT 24 C" {
		t.Errorf("address = %q %q %q", c.Address, c.PostalCode, c.City)
	}
	if want := "Eneco · HOOGSTRAAT 24 C"; c.Name != want {
		t.Errorf("name = %q, want %q (energyDistributor as operator)", c.Name, want)
	}
	if c.PowerKW != 22 || c.CurrentType != "AC" {
		t.Errorf("power/current = %v %q", c.PowerKW, c.CurrentType)
	}
}

func TestParseAFIRJSON_NAP_ExposesRoamingEVSEIDs(t *testing.T) {
	doc := parseTbl(t, afirNAPJSON)

	want := map[string]string{
		"BE-ENE-EENECO_G44971-1": "BE*ENE*EENECO*G44971*1",
		"BE-ALL-0012951":         "BEALLEGO0012951",
	}
	for idG, evseID := range want {
		if got := doc.EVSEIDs[idG]; got != evseID {
			t.Errorf("EVSEIDs[%q] = %q, want %q", idG, got, evseID)
		}
	}
}

func TestParseAFIRJSON_NAP_GrossesUpNetPrices(t *testing.T) {
	doc := parseTbl(t, afirNAPJSON)

	byPoint := map[string]AFIRStatusUpdate{}
	for _, s := range doc.Statuses {
		byPoint[s.EVSEUID] = s
	}

	ac := byPoint["BE-ENE-EENECO_G44971-1"]
	if ac.Status != "AVAILABLE" {
		t.Errorf("status = %q, want AVAILABLE", ac.Status)
	}
	if ac.Tariff == nil {
		t.Fatal("no tariff on the AC point")
	}
	if ac.Tariff.OCPIID != "CREGWPVG" || ac.Tariff.Currency != "EUR" {
		t.Errorf("tariff id/currency = %q/%q", ac.Tariff.OCPIID, ac.Tariff.Currency)
	}
	// 0.3722 net + 21% VAT (stated as a percentage).
	if got, want := ac.Tariff.Elements[0].PriceComponents[0].Price, 0.4504; got != want {
		t.Errorf("ENERGY = %v, want %v", got, want)
	}

	dc := byPoint["BE-ALL-0012951"]
	if dc.Status != "CHARGING" {
		t.Errorf("status = %q, want CHARGING", dc.Status)
	}
	comps := map[string]float64{}
	for _, pc := range dc.Tariff.Elements[0].PriceComponents {
		comps[pc.Type] = pc.Price
	}
	// The same 21% VAT, this time stated as a fraction; the FLAT price already
	// includes tax and must be left alone.
	if got, want := comps["ENERGY"], 0.726; got != want {
		t.Errorf("ENERGY = %v, want %v", got, want)
	}
	if got, want := comps["TIME"], 7.26; got != want { // €0.121/min grossed, per hour
		t.Errorf("TIME = %v, want %v", got, want)
	}
	if got, want := comps["FLAT"], 0.50; got != want {
		t.Errorf("FLAT = %v, want %v", got, want)
	}
}
