package ingest

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// Two eliso stations: a 135 kW CCS/DC point and a 22 kW Type-2/AC point.
const elisoTablePush = `[
{"city":"Haßfurt","address":"Am Wasserwerk 2","postalCode":"97437","country_iso_3166_alpha_2":"DE",
 "operator_name":"eliso GmbH","coordinates":{"latitude":50.030589,"longitude":10.520250},
 "evses":[{"evseId":"DE*ELI*E5207045","charge_points_type":"dc",
   "connectors":[{"maxPower":135,"powerType":"DC","type_of_connector":"Combo2/CCS (DC)"}]}]},
{"city":"Berlin","address":"Musterstraße 1","postalCode":"10115","country_iso_3166_alpha_2":"DE",
 "operator_name":"eliso GmbH","coordinates":{"latitude":52.5200,"longitude":13.4050},
 "evses":[{"evseId":"DE*ELI*ACBERLIN","charge_points_type":"ac",
   "connectors":[{"maxPower":22,"powerType":"AC","type_of_connector":"Type 2 (AC)"}]}]}]`

func TestParseElisoTable_Detection(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"real eliso table", elisoTablePush, true},
		{"afir json envelope (object)", mobTablePush, false},
		{"eliso overlay (object)", elisoOverlayPush, false},
		{"empty array", `[]`, false},
		{"array without coords", `[{"evses":[{"evseId":"DE*ELI*E1"}]}]`, false},
		{"array without evse id", `[{"coordinates":{"latitude":1,"longitude":2},"evses":[{}]}]`, false},
		{"unrelated array", `[1,2,3]`, false},
		{"not json", `<xml/>`, false},
	}
	for _, c := range cases {
		if _, ok := parseElisoTable([]byte(c.body)); ok != c.ok {
			t.Errorf("%s: parseElisoTable ok=%v want %v", c.name, ok, c.ok)
		}
	}
}

func TestElisoPlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Combo2/CCS (DC)", "IEC_62196_T2_COMBO"},
		{"CCS", "IEC_62196_T2_COMBO"},
		{"Type 2 (AC)", "IEC_62196_T2"},
		{"Type 2 Mennekes", "IEC_62196_T2"},
		{"CHAdeMO (DC)", "CHADEMO"},
		{"Type 1 (AC)", "IEC_62196_T1"},
		{"Schuko (AC)", "DOMESTIC_F"},
		{"iec62196T2", "IEC_62196_T2"}, // falls through to NormalizePlug
	}
	for _, c := range cases {
		if got := elisoPlug(c.in); got != c.want {
			t.Errorf("elisoPlug(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestElisoPowerAndCurrent(t *testing.T) {
	if got := elisoPowerKW(135); got != 135 {
		t.Errorf("elisoPowerKW(135)=%v want 135", got)
	}
	if got := elisoPowerKW(150000); got != 150 { // defensive watts→kW
		t.Errorf("elisoPowerKW(150000)=%v want 150", got)
	}
	if got := elisoCurrent("DC", "dc"); got != "DC" {
		t.Errorf("elisoCurrent DC = %q want DC", got)
	}
	if got := elisoCurrent("AC", "ac"); got != "AC" {
		t.Errorf("elisoCurrent AC = %q want AC", got)
	}
	if got := elisoCurrent("", "dc"); got != "DC" { // current from charge_points_type
		t.Errorf("elisoCurrent fallback = %q want DC", got)
	}
}

// End-to-end: the eliso location table seeds chargers under mob-eliso, then the
// eliso status/price overlay attaches to them by EVSE id.
func TestIngestElisoTable_SeedsThenOverlayAttaches(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	kind, n, err := e.IngestMobilithekPush(ctx, []byte(elisoTablePush))
	if err != nil || kind != "eliso-table" {
		t.Fatalf("eliso table ingest: kind=%q n=%d err=%v", kind, n, err)
	}
	if n != 2 {
		t.Errorf("connectors built = %d; want 2", n)
	}

	// The DC point: 135 kW, CCS combo, under mob-eliso.
	var (
		power   float64
		plug    string
		current string
	)
	if err := st.Pool.QueryRow(ctx,
		`SELECT power_kw, plug_type, current_type FROM charger WHERE cpo_id='mob-eliso' AND evse_uid=$1`,
		"DE*ELI*E5207045").Scan(&power, &plug, &current); err != nil {
		t.Fatalf("read seeded DC charger: %v", err)
	}
	if power != 135 || plug != "IEC_62196_T2_COMBO" || current != "DC" {
		t.Errorf("DC charger = %v/%s/%s; want 135/IEC_62196_T2_COMBO/DC", power, plug, current)
	}

	// The AC point mapped correctly too.
	if err := st.Pool.QueryRow(ctx,
		`SELECT power_kw, plug_type, current_type FROM charger WHERE cpo_id='mob-eliso' AND evse_uid=$1`,
		"DE*ELI*ACBERLIN").Scan(&power, &plug, &current); err != nil {
		t.Fatalf("read seeded AC charger: %v", err)
	}
	if power != 22 || plug != "IEC_62196_T2" || current != "AC" {
		t.Errorf("AC charger = %v/%s/%s; want 22/IEC_62196_T2/AC", power, plug, current)
	}

	// Now an overlay push for the DC EVSE attaches status + price.
	overlay := `{"evses":[{"evseId":"DE*ELI*E5207045","operator_name":"eliso GmbH",
		"adhoc_price":0.49,"operational_status":"Operational","availability_status":"In use"}]}`
	kind, n, err = e.IngestMobilithekPush(ctx, []byte(overlay))
	if err != nil || kind != "eliso" {
		t.Fatalf("eliso overlay ingest: kind=%q err=%v", kind, err)
	}
	if n != 1 {
		t.Errorf("overlay rows touched = %d; want 1", n)
	}

	rows, err := st.ChargersForEVSEAny(ctx, "DE*ELI*E5207045")
	if err != nil || len(rows) != 1 {
		t.Fatalf("chargers-for-evse = %d (%v); want 1", len(rows), err)
	}
	var status string
	if err := st.Pool.QueryRow(ctx, `SELECT status FROM charger_status WHERE charger_id=$1`, rows[0].ID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "CHARGING" {
		t.Errorf("status = %q; want CHARGING", status)
	}
}
