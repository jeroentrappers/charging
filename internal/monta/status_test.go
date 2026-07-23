package monta

import (
	"encoding/json"
	"testing"
)

// Captured verbatim from the live per-EVSE status endpoint (2026-07), after
// Monta migrated it to the DATEX/AFIR JSON encoding: ratePolicy and priceType
// are now {"value":…} objects, applicableCurrency is an array, and the price
// list is "energyPrice" (was "energyRate" with bare-string fields). The old
// struct rejected this with "cannot unmarshal object into … of type string",
// which silently froze every Monta ad-hoc price.
const montaStatusJSON = `{
  "lang": "en",
  "electricChargingPointStatus": {
    "evseId": "BE*MON*E1000020370*2",
    "availabilityStatus": "available",
    "energyRateUpdate": [
      {
        "idG": "BE*MON*E1000020370*2-adhoc",
        "ratePolicy": { "value": "adHoc", "extendedValueG": null },
        "applicableCurrency": ["eur"],
        "lastUpdated": "2026-05-27T13:00:36Z",
        "energyPrice": [
          {
            "priceType": { "value": "pricePerKWh", "extendedValueG": null },
            "value": 0.4082,
            "taxIncluded": true,
            "taxRate": 21.0
          }
        ]
      }
    ]
  }
}`

func TestStatusRespUnmarshalNewShape(t *testing.T) {
	var sr statusResp
	if err := json.Unmarshal([]byte(montaStatusJSON), &sr); err != nil {
		t.Fatalf("unmarshal new Monta status shape: %v", err)
	}
	if got := mapStatus(sr.Status.AvailabilityStatus); got != "AVAILABLE" {
		t.Errorf("status = %q, want AVAILABLE", got)
	}

	tar := mapTariff(sr.Status.EvseID, sr)
	if tar == nil {
		t.Fatal("mapTariff returned nil — adHoc rate not recognised")
	}
	if tar.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR (upcased from \"eur\")", tar.Currency)
	}
	comps := tar.Elements[0].PriceComponents
	if len(comps) != 1 {
		t.Fatalf("got %d components, want 1", len(comps))
	}
	if comps[0].Type != "ENERGY" || comps[0].Price != 0.4082 {
		t.Errorf("component = %+v, want ENERGY 0.4082", comps[0])
	}
}

// A per-minute time price must be scaled to €/hour, a flat session fee kept as
// FLAT, and a non-adHoc rate ignored. Tax-inclusive wins over a duplicate excl.
func TestMapTariffComponentsAndPolicy(t *testing.T) {
	const j = `{
      "electricChargingPointStatus": {
        "evseId": "BE*MON*E1*1",
        "availabilityStatus": "occupied",
        "energyRateUpdate": [
          {
            "ratePolicy": { "value": "contract" },
            "energyPrice": [ { "priceType": { "value": "pricePerKWh" }, "value": 0.20, "taxIncluded": true } ]
          },
          {
            "ratePolicy": { "value": "adHoc" },
            "applicableCurrency": ["eur"],
            "energyPrice": [
              { "priceType": { "value": "pricePerKWh" }, "value": 0.10, "taxIncluded": false },
              { "priceType": { "value": "pricePerKWh" }, "value": 0.50, "taxIncluded": true },
              { "priceType": { "value": "pricePerMinute" }, "value": 0.05 },
              { "priceType": { "value": "flatRate" }, "value": 0.35 }
            ]
          }
        ]
      }
    }`
	var sr statusResp
	if err := json.Unmarshal([]byte(j), &sr); err != nil {
		t.Fatal(err)
	}
	tar := mapTariff(sr.Status.EvseID, sr)
	if tar == nil {
		t.Fatal("mapTariff nil — should pick the adHoc rate, not the contract one")
	}
	got := map[string]float64{}
	for _, c := range tar.Elements[0].PriceComponents {
		got[c.Type] = c.Price
	}
	if got["ENERGY"] != 0.50 {
		t.Errorf("ENERGY = %v, want 0.50 (tax-incl preferred over 0.10 excl)", got["ENERGY"])
	}
	if got["TIME"] != 3.0 {
		t.Errorf("TIME = %v, want 3.0 (0.05/min × 60)", got["TIME"])
	}
	if got["FLAT"] != 0.35 {
		t.Errorf("FLAT = %v, want 0.35", got["FLAT"])
	}
}
