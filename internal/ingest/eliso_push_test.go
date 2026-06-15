package ingest

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestParseElisoPush_Detection(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"real eliso", `{"evses":[{"evseId":"DE*ELI*E1","operational_status":"Operational","availability_status":"Not in use","adhoc_price":0.49}]}`, true},
		{"afir json envelope", mobTablePush, false},
		{"afir status envelope", mobStatusPush, false},
		{"empty evses", `{"evses":[]}`, false},
		{"evses but no id", `{"evses":[{"operational_status":"Operational"}]}`, false},
		{"evses but no operational_status", `{"evses":[{"evseId":"DE*ELI*E1"}]}`, false},
		{"not json", `<xml/>`, false},
	}
	for _, c := range cases {
		if _, ok := parseElisoPush([]byte(c.body)); ok != c.ok {
			t.Errorf("%s: parseElisoPush ok=%v want %v", c.name, ok, c.ok)
		}
	}
}

func TestElisoStatus(t *testing.T) {
	cases := []struct{ op, av, want string }{
		{"Operational", "Not in use", "AVAILABLE"},
		{"Operational", "In use", "CHARGING"},
		{"Operational", "None", "UNKNOWN"},
		{"Non-operational", "Not in use", "OUTOFORDER"},
		{"Non-operational", "In use", "OUTOFORDER"},
	}
	for _, c := range cases {
		if got := elisoStatus(c.op, c.av); got != c.want {
			t.Errorf("elisoStatus(%q,%q)=%q want %q", c.op, c.av, got, c.want)
		}
	}
}

func TestElisoTariff(t *testing.T) {
	p := func(f float64) *float64 { return &f }

	// Priced: ENERGY from adhoc_price, PARKING_TIME from blocking_fee (€/min→€/h).
	tar := elisoTariff(elisoEVSE{EVSEID: "DE*ELI*E1", AdhocPrice: p(0.49), BlockingFee: p(0.10)})
	if tar == nil || len(tar.Elements) != 1 {
		t.Fatalf("tariff = %+v; want one element", tar)
	}
	comps := tar.Elements[0].PriceComponents
	if len(comps) != 2 {
		t.Fatalf("components = %d; want 2", len(comps))
	}
	if comps[0].Type != "ENERGY" || comps[0].Price != 0.49 {
		t.Errorf("energy = %+v; want ENERGY 0.49", comps[0])
	}
	if comps[1].Type != "PARKING_TIME" || comps[1].Price != 6 {
		t.Errorf("parking = %+v; want PARKING_TIME 6.0 (0.10/min * 60)", comps[1])
	}

	// No prices → nil (status-only update, no tariff churn).
	if elisoTariff(elisoEVSE{EVSEID: "DE*ELI*E1"}) != nil {
		t.Error("unpriced eliso evse should yield nil tariff")
	}
	// A zero blocking fee is dropped, energy kept.
	tar = elisoTariff(elisoEVSE{EVSEID: "DE*ELI*E1", AdhocPrice: p(0.59), BlockingFee: p(0)})
	if tar == nil || len(tar.Elements[0].PriceComponents) != 1 {
		t.Fatalf("tariff = %+v; want one ENERGY component only", tar)
	}
}

// A broker AFIR table push seeds the location under one CPO; the eliso overlay
// then updates status + price matching by EVSE id alone, across that CPO.
const brokerTablePush = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
"aegiEnergyInfrastructureTablePublication":{"publicationCreator":{"country":"DE","nationalIdentifier":"DE-NAP-BROKER"},
"energyInfrastructureTable":[{"idG":"t1","tableName":"Broker","energyInfrastructureSite":[
{"idG":"site-1","locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":48.81,"longitude":9.27},
"locLocationExtensionG":{"FacilityLocation":{"address":{"postcode":"70736","city":{"values":[{"lang":"en","value":"Fellbach"}]},
"addressLine":[{"type":{"value":"street"},"text":{"values":[{"value":"Im Dietbach 3"}]}}]}}}}},
"operator":{"afacAnOrganisation":{"name":{"values":[{"value":"eliso GmbH"}]}}},
"energyInfrastructureStation":[{"idG":"st-1","totalMaximumPower":22000,"refillPoint":[{"aegiElectricChargingPoint":{"idG":"DE*ELI*E1",
"currentType":{"value":"ac"},"connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":22000}],
"electricEnergy":[{"energyRate":[{"idG":"rate-1","ratePolicy":{"value":"adHoc"},"applicableCurrency":["EUR"],
"energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.30}]}]}]}}]}]}]}]}}}`

const elisoOverlayPush = `{"evses":[
{"evseId":"DE*ELI*E1","operator_name":"eliso GmbH","adhoc_price":0.49,"blocking_fee":0.10,"operational_status":"Operational","availability_status":"In use"},
{"evseId":"DE*ELI*E999","operator_name":"eliso GmbH","adhoc_price":0.59,"operational_status":"Operational","availability_status":"Not in use"}]}`

func TestIngestElisoPush_OverlaysBrokerSeededCharger(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	// Seed the location under the broker CPO.
	if kind, _, err := e.IngestMobilithekPush(ctx, []byte(brokerTablePush)); err != nil || kind != "table" {
		t.Fatalf("broker table ingest: kind=%q err=%v", kind, err)
	}
	conns, err := st.ChargersForEVSE(ctx, "mob-broker", "DE*ELI*E1")
	if err != nil || len(conns) != 1 {
		t.Fatalf("seeded charger = %d (%v); want 1 under mob-broker", len(conns), err)
	}

	// Eliso overlay: one EVSE we have (DE*ELI*E1), one we don't (E999) → 1 row.
	kind, n, err := e.IngestMobilithekPush(ctx, []byte(elisoOverlayPush))
	if err != nil || kind != "eliso" {
		t.Fatalf("eliso ingest: kind=%q err=%v", kind, err)
	}
	if n != 1 {
		t.Errorf("rows touched = %d; want 1 (E999 has no location)", n)
	}

	// Status overlaid: "In use" while Operational → CHARGING.
	var status string
	if err := st.Pool.QueryRow(ctx, `SELECT status FROM charger_status WHERE charger_id=$1`, conns[0].ID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "CHARGING" {
		t.Errorf("status = %q; want CHARGING", status)
	}

	// Price overlaid: comparable reflects the eliso €0.49/kWh, not the broker €0.30.
	var comparable *float64
	if err := st.Pool.QueryRow(ctx,
		`SELECT comparable_price_eur FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`, conns[0].ID).Scan(&comparable); err != nil {
		t.Fatalf("read tariff: %v", err)
	}
	if comparable == nil {
		t.Fatalf("charger not priced after eliso overlay")
	}
	// Standard session is energy-dominated; €0.49/kWh must price above the €0.30 broker rate.
	if *comparable <= 0 {
		t.Errorf("comparable = %v; want > 0", *comparable)
	}
}
