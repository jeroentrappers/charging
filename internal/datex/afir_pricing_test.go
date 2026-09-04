package datex

import (
	"strings"
	"testing"

	"github.com/appmire/charging/internal/model"
)

// Real shapes from Eco-Movement's Belgian NAP feed: an idle fee with the AFIR
// structured grace threshold, published as a tiered schedule (a €0 tier and the
// fee itself, in either order), plus the €0 flat fee the aggregator emits for
// points whose operator has filed no tariff.
func napStatus(points string) string {
	return `{"modelBaseVersionG":"3","aegiEnergyInfrastructureStatusPublication":{
	  "lang":"en","publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
	  "energyInfrastructureSiteStatus":[{"reference":{"idG":"site-1"},
	    "energyInfrastructureStationStatus":[{"reference":{"idG":"st-1"},
	      "refillPointStatus":[` + points + `]}]}]}}`
}

func point(id, prices string) string {
	return `{"aegiElectricChargingPointStatus":{"reference":{"idG":"` + id + `"},"status":{"value":"available"},
	  "energyRateUpdate":[{"energyRateReference":{"idG":"rate-` + id + `"},"energyPrice":[` + prices + `]}]}}`
}

func statusByPoint(t *testing.T, doc *AFIRDoc) map[string]AFIRStatusUpdate {
	t.Helper()
	out := map[string]AFIRStatusUpdate{}
	for _, s := range doc.Statuses {
		out[s.EVSEUID] = s
	}
	return out
}

func comps(t *testing.T, u AFIRStatusUpdate) map[string]model.PriceComponent {
	t.Helper()
	if u.Tariff == nil {
		t.Fatalf("point %s: no tariff", u.EVSEUID)
	}
	out := map[string]model.PriceComponent{}
	for _, el := range u.Tariff.Elements {
		for _, pc := range el.PriceComponents {
			if _, dup := out[pc.Type]; dup {
				t.Errorf("point %s: more than one %s component: %+v", u.EVSEUID, pc.Type, u.Tariff.Elements)
			}
			out[pc.Type] = pc
		}
	}
	return out
}

const kwh = `{"priceGroupIndex":0,"priceType":{"value":"pricePerKWh"},"value":0.5,"taxIncluded":false,"taxRate":21}`

func TestAFIRJSON_GraceThresholdFromTimeBasedApplicability(t *testing.T) {
	doc := parseTbl(t, napStatus(strings.Join([]string{
		// the fee alone, with its threshold
		point("plain", kwh+`,{"priceGroupIndex":1,"priceType":{"value":"pricePerMinute"},"value":0.1,"taxIncluded":false,"taxRate":21,"timeBasedApplicability":{"fromMinute":240}}`),
		// tiered, free tier listed FIRST (used to drop the fee entirely)
		point("zerofirst", kwh+`,{"priceGroupIndex":1,"priceType":{"value":"pricePerMinute"},"value":0,"taxIncluded":false,"taxRate":21,"timeBasedApplicability":{"fromMinute":0}},{"priceGroupIndex":2,"priceType":{"value":"pricePerMinute"},"value":0.1,"taxIncluded":false,"taxRate":21,"timeBasedApplicability":{"fromMinute":60}}`),
		// tiered, fee listed FIRST (used to charge it from minute zero)
		point("feefirst", kwh+`,{"priceGroupIndex":0,"priceType":{"value":"pricePerMinute"},"value":0.1,"taxIncluded":false,"taxRate":21,"timeBasedApplicability":{"fromMinute":300}},{"priceGroupIndex":3,"priceType":{"value":"pricePerMinute"},"value":0,"taxIncluded":false,"taxRate":21,"timeBasedApplicability":{"fromMinute":0}}`),
	}, ",")))

	by := statusByPoint(t, doc)
	for _, tc := range []struct {
		id        string
		wantAfter int
	}{
		{"plain", 240}, {"zerofirst", 60}, {"feefirst", 300},
	} {
		c := comps(t, by[tc.id])
		time, ok := c["TIME"]
		if !ok {
			t.Errorf("%s: TIME component missing — the idle fee was dropped", tc.id)
			continue
		}
		// €0.10/min net + 21% VAT = €0.121/min = €7.26/h.
		if time.Price != 7.26 {
			t.Errorf("%s: TIME = %v €/h, want 7.26", tc.id, time.Price)
		}
		if time.AfterMinutes != tc.wantAfter {
			t.Errorf("%s: AfterMinutes = %d, want %d", tc.id, time.AfterMinutes, tc.wantAfter)
		}
	}
}

func TestAFIRJSON_ZeroFlatFeeIsNotAPrice(t *testing.T) {
	doc := parseTbl(t, napStatus(strings.Join([]string{
		// the aggregator's placeholder: a €0 session fee and nothing else
		point("placeholder", `{"priceGroupIndex":0,"priceType":{"value":"flatRate"},"value":0,"taxIncluded":false,"taxRate":21}`),
		// a real session fee stays
		point("realflat", `{"priceGroupIndex":0,"priceType":{"value":"flatRate"},"value":0.5,"taxIncluded":true,"taxRate":21}`),
		// an explicit €0 per-kWh price IS a free-charging claim
		point("free", `{"priceGroupIndex":0,"priceType":{"value":"pricePerKWh"},"value":0,"taxIncluded":false,"taxRate":21}`),
		// so is the AFIR "free" price type
		point("freetype", `{"priceGroupIndex":0,"priceType":{"value":"free"},"value":0,"taxIncluded":true,"taxRate":21}`),
	}, ",")))

	by := statusByPoint(t, doc)
	if u := by["placeholder"]; u.Tariff != nil {
		t.Errorf("a €0-flat-only rate became a tariff (%v) — it would rank as a free charger", u.Tariff.Elements)
	}
	if u := by["placeholder"]; u.Status != "AVAILABLE" {
		t.Errorf("status must survive an unusable price: got %q", u.Status)
	}
	if got := comps(t, by["realflat"])["FLAT"].Price; got != 0.5 {
		t.Errorf("FLAT = %v, want 0.5", got)
	}
	for _, id := range []string{"free", "freetype"} {
		c := comps(t, by[id])
		if e, ok := c["ENERGY"]; !ok || e.Price != 0 {
			t.Errorf("%s: want a €0 ENERGY component, got %+v", id, c)
		}
	}
}

func TestAFIRJSON_TableRateWithOnlyAZeroFlatFeeYieldsNoTariff(t *testing.T) {
	const tbl = `{"modelBaseVersionG":"3","aegiEnergyInfrastructureTablePublication":{
	  "lang":"en","publicationCreator":{"country":"NL","nationalIdentifier":"NL-EC0"},
	  "energyInfrastructureTable":[{"idG":"t1","energyInfrastructureSite":[
	    {"idG":"s1","locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":50.8,"longitude":4.3}}},
	     "energyInfrastructureStation":[{"idG":"st1","totalMaximumPower":22000,
	       "refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-1","currentType":{"value":"ac"},
	         "connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":22000}],
	         "electricEnergy":[{"energyRate":[{"idG":"rate-1","ratePolicy":{"value":"adHoc"},
	           "energyPrice":[{"priceType":{"value":"flatRate"},"value":0,"taxIncluded":false,"taxRate":21}]}]}]}}]}]}]}]}}`

	doc := parseTbl(t, tbl)
	if len(doc.Connectors) != 1 {
		t.Fatalf("connectors = %d, want 1", len(doc.Connectors))
	}
	if got := doc.Connectors[0].TariffID; got != "" {
		t.Errorf("TariffID = %q, want empty (the rate prices nothing)", got)
	}
	if len(doc.Tariffs) != 0 {
		t.Errorf("tariffs = %v, want none", doc.Tariffs)
	}
}

// The XML encoding carries the same DATEX field, so it gets the same treatment.
func TestAFIRXML_GraceThresholdFromTimeBasedApplicability(t *testing.T) {
	const statusXML = `<?xml version="1.0" encoding="UTF-8"?>
<messageContainer xmlns="http://datex2.eu/schema/3/messageContainer"
                  xmlns:egi="http://datex2.eu/schema/3/energyInfrastructure">
  <payload xsi:type="egi:EnergyInfrastructureStatusPublication"
           xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <egi:energyInfrastructureStatus>
      <egi:energyInfrastructureStationStatus>
        <egi:reference id="ST1"/>
        <egi:refillPointStatus>
          <egi:reference id="RP1"/>
          <egi:status>available</egi:status>
          <egi:energyRateUpdate>
            <egi:energyPrice>
              <egi:priceType>pricePerKWh</egi:priceType>
              <egi:value>0.50</egi:value>
              <egi:taxIncluded>true</egi:taxIncluded>
            </egi:energyPrice>
            <egi:energyPrice>
              <egi:priceType>pricePerMinute</egi:priceType>
              <egi:value>0.10</egi:value>
              <egi:taxIncluded>true</egi:taxIncluded>
              <egi:timeBasedApplicability><egi:fromMinute>45</egi:fromMinute></egi:timeBasedApplicability>
            </egi:energyPrice>
          </egi:energyRateUpdate>
        </egi:refillPointStatus>
      </egi:energyInfrastructureStationStatus>
    </egi:energyInfrastructureStatus>
  </payload>
</messageContainer>`

	st, err := ParseAFIRStatus([]byte(statusXML))
	if err != nil {
		t.Fatalf("ParseAFIRStatus: %v", err)
	}
	u, ok := st["RP1"]
	if !ok {
		t.Fatalf("no status for RP1: %v", st)
	}
	if u.Tariff == nil {
		t.Fatal("no tariff")
	}
	var timeC *model.PriceComponent
	for _, el := range u.Tariff.Elements {
		for i, pc := range el.PriceComponents {
			if pc.Type == "TIME" {
				timeC = &el.PriceComponents[i]
			}
		}
	}
	if timeC == nil {
		t.Fatal("no TIME component")
	}
	if timeC.AfterMinutes != 45 {
		t.Errorf("AfterMinutes = %d, want 45", timeC.AfterMinutes)
	}
}

// A status publication that sends a price update which prices nothing is
// evidence the point has no usable price — distinct from sending no update at
// all, which is what push feeds do for points that simply haven't changed.
func TestAFIRJSON_PriceWithdrawnDistinguishesZeroFromSilence(t *testing.T) {
	doc := parseTbl(t, napStatus(strings.Join([]string{
		point("placeholder", `{"priceGroupIndex":0,"priceType":{"value":"flatRate"},"value":0,"taxIncluded":false,"taxRate":21}`),
		point("priced", kwh),
		// no energyRateUpdate at all
		`{"aegiElectricChargingPointStatus":{"reference":{"idG":"silent"},"status":{"value":"available"}}}`,
	}, ",")))

	by := statusByPoint(t, doc)
	for _, tc := range []struct {
		id            string
		wantTariff    bool
		wantWithdrawn bool
	}{
		{"placeholder", false, true},
		{"priced", true, false},
		{"silent", false, false},
	} {
		u := by[tc.id]
		if (u.Tariff != nil) != tc.wantTariff {
			t.Errorf("%s: tariff present = %v, want %v", tc.id, u.Tariff != nil, tc.wantTariff)
		}
		if u.PriceWithdrawn != tc.wantWithdrawn {
			t.Errorf("%s: PriceWithdrawn = %v, want %v", tc.id, u.PriceWithdrawn, tc.wantWithdrawn)
		}
	}
}
