package ingest

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// dsn returns the test database URL, defaulting to the local docker-compose DB.
func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://charging:charging@localhost:5433/charging?sslmode=disable"
}

// setup connects to the DB (skipping the test if unreachable) and truncates the
// working tables so the run starts from a known-empty state.
func setup(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.New(ctx, dsn())
	if err != nil {
		t.Skipf("no database available (%v); run `make db-up migrate`", err)
	}
	_, err = st.Pool.Exec(ctx,
		`TRUNCATE tariff_version, charger_status, charger, ingest_run, cpo RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Skipf("schema not migrated (%v); run `make migrate`", err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestIngest_EndToEnd_SCD2(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	const token = "test-token"
	feed := newMockFeed(token)
	srv := feed.server()
	defer srv.Close()

	// Register the source pointing at the mock server.
	cpo := store.CPO{
		ID: "mockcpo", Name: "Mock CPO",
		OCPIBaseURL: srv.URL + "/", OCPIVersion: "2.1.1",
		PollCron: "0 4 * * *", Enabled: true,
	}
	if err := st.UpsertCPO(ctx, cpo); err != nil {
		t.Fatal(err)
	}
	src := source.Source{CPO: cpo, Token: token}
	eng := NewEngine(st, log)

	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// --- Pass 1: first observation -> exactly one open tariff version.
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{sampleTariff(0.45, t0)})
	if err := eng.RunSource(ctx, src); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	assertCounts(t, st, 1, 1, 1) // 1 charger, 1 version total, 1 open

	chargerID := singleChargerID(t, st)
	if price := currentPrice(t, st, chargerID); price == nil || *price != 13.75 {
		// 30 kWh * 0.45 + 0.25 flat = 13.75
		t.Fatalf("pass 1 comparable price: want 13.75, got %v", price)
	}

	// --- Pass 2: identical feed -> NO new version (change detection holds).
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{sampleTariff(0.45, t0)})
	if err := eng.RunSource(ctx, src); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	assertCounts(t, st, 1, 1, 1) // still exactly one version

	// --- Pass 3: price change -> new version, previous one closed.
	t1 := t0.Add(48 * time.Hour)
	feed.set([]ocpi.Location{sampleLocation("CHARGING")}, []ocpi.Tariff{sampleTariff(0.55, t1)})
	if err := eng.RunSource(ctx, src); err != nil {
		t.Fatalf("pass 3: %v", err)
	}
	assertCounts(t, st, 1, 2, 1) // 2 versions total, 1 still open

	if price := currentPrice(t, st, chargerID); price == nil || *price != 16.75 {
		// 30 * 0.55 + 0.25 = 16.75
		t.Fatalf("pass 3 comparable price: want 16.75, got %v", price)
	}

	// The closed version must have observed_to set and the previous price.
	assertClosedVersionPrice(t, st, chargerID, 13.75)

	// Availability flipped to CHARGING -> not available.
	if avail := availableCount(t, st, chargerID); avail != 0 {
		t.Fatalf("expected available_count 0 after CHARGING, got %d", avail)
	}
}

func TestIngest_CheapestNearbyQuery(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	const token = "test-token"
	feed := newMockFeed(token)
	srv := feed.server()
	defer srv.Close()

	cpo := store.CPO{ID: "mockcpo", Name: "Mock CPO", OCPIBaseURL: srv.URL + "/", OCPIVersion: "2.1.1", PollCron: "0 4 * * *", Enabled: true}
	if err := st.UpsertCPO(ctx, cpo); err != nil {
		t.Fatal(err)
	}
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{sampleTariff(0.45, time.Now())})
	if err := NewEngine(st, log).RunSource(ctx, source.Source{CPO: cpo, Token: token}); err != nil {
		t.Fatal(err)
	}

	// Query near the fixture's coordinates (Gent, 51.05432 / 3.72500).
	res, err := st.CheapestNearby(ctx, store.NearbyQuery{
		Lat: 51.0544, Lon: 3.7251, RadiusM: 2000, OnlyAvail: true, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("want 1 nearby charger, got %d", len(res))
	}
	got := res[0]
	if got.PriceEUR == nil || *got.PriceEUR != 13.75 {
		t.Fatalf("want price 13.75, got %v", got.PriceEUR)
	}
	if got.DistanceM <= 0 || got.DistanceM > 50 {
		t.Fatalf("distance looks wrong: %v m", got.DistanceM)
	}
	if got.Available != 1 {
		t.Fatalf("want available 1, got %d", got.Available)
	}

	// A far-away query must return nothing.
	far, err := st.CheapestNearby(ctx, store.NearbyQuery{Lat: 50.85, Lon: 4.35, RadiusM: 1000, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(far) != 0 {
		t.Fatalf("want 0 far chargers, got %d", len(far))
	}
}

// ---- assertion helpers ----

func assertCounts(t *testing.T, st *store.Store, wantChargers, wantVersions, wantOpen int) {
	t.Helper()
	ctx := context.Background()
	var chargers, versions, open int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM charger`).Scan(&chargers); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM tariff_version`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM tariff_version WHERE observed_to IS NULL`).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if chargers != wantChargers || versions != wantVersions || open != wantOpen {
		t.Fatalf("counts: chargers=%d (want %d), versions=%d (want %d), open=%d (want %d)",
			chargers, wantChargers, versions, wantVersions, open, wantOpen)
	}
}

func singleChargerID(t *testing.T, st *store.Store) int64 {
	t.Helper()
	var id int64
	if err := st.Pool.QueryRow(context.Background(), `SELECT id FROM charger LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func currentPrice(t *testing.T, st *store.Store, chargerID int64) *float64 {
	t.Helper()
	var p *float64
	err := st.Pool.QueryRow(context.Background(),
		`SELECT comparable_price_eur::float8 FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`,
		chargerID).Scan(&p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertClosedVersionPrice(t *testing.T, st *store.Store, chargerID int64, want float64) {
	t.Helper()
	var p *float64
	var closedAt *time.Time
	err := st.Pool.QueryRow(context.Background(),
		`SELECT comparable_price_eur::float8, observed_to FROM tariff_version
		 WHERE charger_id=$1 AND observed_to IS NOT NULL ORDER BY observed_from LIMIT 1`,
		chargerID).Scan(&p, &closedAt)
	if err != nil {
		t.Fatal(err)
	}
	if closedAt == nil {
		t.Fatal("closed version has no observed_to")
	}
	if p == nil || *p != want {
		t.Fatalf("closed version price: want %v, got %v", want, p)
	}
}

func availableCount(t *testing.T, st *store.Store, chargerID int64) int {
	t.Helper()
	var n int
	if err := st.Pool.QueryRow(context.Background(),
		`SELECT available_count FROM charger_status WHERE charger_id=$1`, chargerID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
