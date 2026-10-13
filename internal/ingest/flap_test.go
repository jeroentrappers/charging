package ingest

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"reflect"
	"testing"

	"github.com/appmire/charging/internal/model"
)

func TestDedupeElisoEVSEs(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	got := dedupeElisoEVSEs([]elisoEVSE{
		{EVSEID: "A", AdhocPrice: p(0.49), BlockingFee: p(0.10), OperationalStatus: "Operational"},
		{EVSEID: "B", AdhocPrice: p(0.39)},
		{EVSEID: "A", AdhocPrice: p(0.59), BlockingFee: nil},
		{EVSEID: ""},
	})
	if len(got) != 2 || got[0].EVSEID != "A" || got[1].EVSEID != "B" {
		t.Fatalf("got %+v; want A then B", got)
	}
	if *got[0].AdhocPrice != 0.59 || *got[0].BlockingFee != 0.10 || got[0].OperationalStatus != "Operational" {
		t.Errorf("A = price %v fee %v status %q; want 0.59 / 0.10 / Operational",
			*got[0].AdhocPrice, *got[0].BlockingFee, got[0].OperationalStatus)
	}
}

// versionCounts returns the charger's total and open tariff version counts.
func versionCounts(t *testing.T, ctx context.Context, e *Engine, id int64) (total, open int) {
	t.Helper()
	if err := e.Store.Pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE observed_to IS NULL) FROM tariff_version WHERE charger_id=$1`,
		id).Scan(&total, &open); err != nil {
		t.Fatal(err)
	}
	return total, open
}

// TestElisoDuplicateEVSE_NoFlap replays the production pattern: one snapshot
// lists an EVSE twice at two prices, and the next lists the same copies in the
// opposite order. That used to open a new version per copy on every push.
func TestElisoDuplicateEVSE_NoFlap(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if _, _, err := e.IngestMobilithekPush(ctx, []byte(brokerTablePush)); err != nil {
		t.Fatal(err)
	}
	conns, err := st.ChargersForEVSE(ctx, "mob-broker", "DE*ELI*E1")
	if err != nil || len(conns) != 1 {
		t.Fatalf("seeded charger = %d (%v); want 1", len(conns), err)
	}
	id := conns[0].ID
	before, _ := versionCounts(t, ctx, e, id)

	const low = `{"evseId":"DE*ELI*E1","operator_name":"eliso GmbH","adhoc_price":0.49,"blocking_fee":0.10,"operational_status":"Operational","availability_status":"In use"}`
	const high = `{"evseId":"DE*ELI*E1","operator_name":"eliso GmbH","adhoc_price":0.59,"blocking_fee":0.10,"operational_status":"Operational","availability_status":"In use"}`
	for _, body := range []string{
		`{"evses":[` + low + `,` + high + `]}`,
		`{"evses":[` + high + `,` + low + `]}`,
		`{"evses":[` + low + `,` + high + `]}`,
	} {
		if kind, _, err := e.IngestMobilithekPush(ctx, []byte(body)); err != nil || kind != "eliso" {
			t.Fatalf("eliso ingest: kind=%q err=%v", kind, err)
		}
	}
	total, open := versionCounts(t, ctx, e, id)
	if open != 1 || total != before+1 {
		t.Fatalf("versions: total %d (was %d), open %d; want exactly one new version", total, before, open)
	}
	var comps string
	if err := st.Pool.QueryRow(ctx,
		`SELECT price_components::text FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`, id).Scan(&comps); err != nil {
		t.Fatal(err)
	}
	var tar model.Tariff
	if err := json.Unmarshal([]byte(comps), &tar); err != nil {
		t.Fatal(err)
	}
	if got := tar.Elements[0].PriceComponents[0].Price; got != 0.59 {
		t.Errorf("stored energy price = %v; want the higher copy, 0.59", got)
	}
}

// TestStatusTariff_SameHeadlineIsConfirmation covers the UFC pattern: the table
// push carries a full schedule and status pushes carry only the tier in force,
// so the two differ on long-session fees while quoting the same headline price.
func TestStatusTariff_SameHeadlineIsConfirmation(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if _, _, err := e.IngestMobilithekPush(ctx, []byte(mobTablePush)); err != nil {
		t.Fatal(err)
	}
	conns, err := st.ChargersForEVSE(ctx, "mob-gpjouleconnect", "cp-1")
	if err != nil || len(conns) != 1 {
		t.Fatalf("ChargersForEVSE = %d (%v); want 1", len(conns), err)
	}
	row := conns[0]
	rateID, _, found, err := st.CurrentTariffRate(ctx, row.ID)
	if err != nil || !found || rateID == "" {
		t.Fatalf("table push left no priced version: id=%q found=%v err=%v", rateID, found, err)
	}
	var stored model.Tariff
	var comps string
	if err := st.Pool.QueryRow(ctx,
		`SELECT price_components::text FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`, row.ID).Scan(&comps); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(comps), &stored); err != nil {
		t.Fatal(err)
	}
	conn := model.Connector{EVSEUID: "cp-1", ConnectorID: row.ConnectorID, PowerKW: row.PowerKW, CurrentType: row.CurrentType, TariffID: rateID}

	// Same rate as the night tier UFC's status pushes carry: the table's idle fee
	// for long stays is missing, which changes long-session prices but not the
	// headline. That is a confirmation, not a version.
	h1, p1 := e.euroPrices(ctx, stored, conn)
	if h1 == nil {
		t.Fatal("stored tariff has no headline")
	}
	var reworded model.Tariff
	for _, after := range []int{120, 180, 240, 300, 360, 480, 600} {
		cand := stored
		cand.Elements = []model.TariffElement{{PriceComponents: append(
			append([]model.PriceComponent{}, stored.Elements[0].PriceComponents...),
			model.PriceComponent{Type: "TIME", Price: 6, AfterMinutes: after})}}
		if h2, p2 := e.euroPrices(ctx, cand, conn); h2 != nil && *h2 == *h1 && !reflect.DeepEqual(p1, p2) {
			reworded = cand
			break
		}
	}
	if reworded.OCPIID == "" {
		t.Fatal("fixture: no idle-fee threshold changes long sessions without moving the headline")
	}
	before, _ := versionCounts(t, ctx, e, row.ID)
	for i := 0; i < 3; i++ {
		if _, err := e.processStatusTariff(ctx, row.ID, conn, reworded); err != nil {
			t.Fatal(err)
		}
	}
	if total, _ := versionCounts(t, ctx, e, row.ID); total != before {
		t.Fatalf("same-priced status tariff opened %d new version(s)", total-before)
	}

	// A real price change on the status push still versions.
	dearer := stored
	dearer.Elements = []model.TariffElement{{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.99}}}}
	if changed, err := e.processStatusTariff(ctx, row.ID, conn, dearer); err != nil || !changed {
		t.Fatalf("price change on status push: changed=%v err=%v; want a new version", changed, err)
	}
}

// TestBrokerPushKeepsElisoOverlay covers the mob-mon pattern: a broker keeps
// publishing its own rate for an eliso EVSE after the eliso overlay priced it.
// The overlay's price must stay instead of the two alternating on every push.
func TestBrokerPushKeepsElisoOverlay(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if _, _, err := e.IngestMobilithekPush(ctx, []byte(brokerTablePush)); err != nil {
		t.Fatal(err)
	}
	conns, err := st.ChargersForEVSE(ctx, "mob-broker", "DE*ELI*E1")
	if err != nil || len(conns) != 1 {
		t.Fatalf("seeded charger = %d (%v); want 1", len(conns), err)
	}
	id := conns[0].ID
	if _, _, err := e.IngestMobilithekPush(ctx, []byte(elisoOverlayPush)); err != nil {
		t.Fatal(err)
	}
	before, _ := versionCounts(t, ctx, e, id)
	for i := 0; i < 3; i++ {
		if _, _, err := e.IngestMobilithekPush(ctx, []byte(brokerTablePush)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.IngestMobilithekPush(ctx, []byte(elisoOverlayPush)); err != nil {
			t.Fatal(err)
		}
	}
	total, _ := versionCounts(t, ctx, e, id)
	if total != before {
		t.Fatalf("broker and overlay alternated: %d new versions", total-before)
	}
	rateID, _, _, err := st.CurrentTariffRate(ctx, id)
	if err != nil || !overlayRate(rateID) {
		t.Fatalf("open rate = %q (%v); want the eliso overlay's", rateID, err)
	}
}
