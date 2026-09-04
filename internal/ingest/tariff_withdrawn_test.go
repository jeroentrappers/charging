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

// locWithTariff is sampleLocation with its own ids, so a test can publish two
// chargers and withdraw the price of one.
func locWithTariff(locID, evseUID, tariffID, lat, lon string) ocpi.Location {
	l := sampleLocation("AVAILABLE")
	l.ID = locID
	l.Coordinates = ocpi.GeoLocation{Latitude: lat, Longitude: lon}
	l.EVSEs[0].UID = evseUID
	l.EVSEs[0].EVSEID = "BE*EVI*" + evseUID
	l.EVSEs[0].Connectors[0].TariffID = tariffID
	return l
}

func tariffWithID(id string, price float64) ocpi.Tariff {
	t := sampleTariff(price, time.Now().UTC())
	t.ID = id
	return t
}

// openAndTotalVersions counts a charger's open tariff versions (0 or 1) and how
// many rows its history holds.
func openAndTotalVersions(t *testing.T, st *store.Store, evseUID string) (open, total int) {
	t.Helper()
	err := st.Pool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE tv.observed_to IS NULL), count(*)
		FROM tariff_version tv JOIN charger c ON c.id = tv.charger_id
		WHERE c.evse_uid = $1`, evseUID).Scan(&open, &total)
	if err != nil {
		t.Fatalf("count versions for %s: %v", evseUID, err)
	}
	return open, total
}

func priceRun(t *testing.T, st *store.Store, cpo store.CPO, feed *mockFeed, locs []ocpi.Location, tars []ocpi.Tariff) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	feed.set(locs, tars)
	if err := NewEngine(st, log).RunPrices(context.Background(), source.Source{CPO: cpo, Token: "test-token"}); err != nil {
		t.Fatalf("price pass: %v", err)
	}
}

// A full-snapshot source that still lists a charger but stops publishing a price
// for it has withdrawn that price: the open version is closed so the charger
// reads as unpriced, and its history is kept.
func TestIngest_ClosesTariffWhenSourceStopsPublishingAPrice(t *testing.T) {
	st := setup(t)
	cpo := store.CPO{
		ID: "mockcpo", Name: "Mock CPO", OCPIBaseURL: "", OCPIVersion: "2.1.1",
		SourceType: "ocpi", PollCron: "0 4 * * *", Enabled: true,
	}
	feed := newMockFeed("test-token")
	srv := feed.server()
	defer srv.Close()
	cpo.OCPIBaseURL = srv.URL + "/"
	if err := st.UpsertCPO(context.Background(), cpo); err != nil {
		t.Fatal(err)
	}

	locs := []ocpi.Location{
		locWithTariff("LOC1", "EVSE1", "TAR1", "51.05432", "3.72500"),
		locWithTariff("LOC2", "EVSE2", "TAR2", "51.05500", "3.72600"),
	}
	priceRun(t, st, cpo, feed, locs, []ocpi.Tariff{tariffWithID("TAR1", 0.45), tariffWithID("TAR2", 0.55)})
	for _, evse := range []string{"EVSE1", "EVSE2"} {
		if open, _ := openAndTotalVersions(t, st, evse); open != 1 {
			t.Fatalf("%s: open versions = %d after the first pass, want 1", evse, open)
		}
	}

	// EVSE1 is still listed, but its tariff is gone from the feed.
	priceRun(t, st, cpo, feed, locs, []ocpi.Tariff{tariffWithID("TAR2", 0.55)})

	open, total := openAndTotalVersions(t, st, "EVSE1")
	if open != 0 {
		t.Errorf("EVSE1: open versions = %d, want 0 (the price was withdrawn)", open)
	}
	if total != 1 {
		t.Errorf("EVSE1: history rows = %d, want the closed version kept", total)
	}
	if open, _ := openAndTotalVersions(t, st, "EVSE2"); open != 1 {
		t.Errorf("EVSE2: open versions = %d, want 1 (its price still stands)", open)
	}

	// A later pass that publishes the price again reopens it.
	priceRun(t, st, cpo, feed, locs, []ocpi.Tariff{tariffWithID("TAR1", 0.45), tariffWithID("TAR2", 0.55)})
	if open, total := openAndTotalVersions(t, st, "EVSE1"); open != 1 || total != 2 {
		t.Errorf("EVSE1 after republication: open = %d (want 1), history rows = %d (want 2)", open, total)
	}
}

// A pass that returns no tariffs at all is a publisher outage, not every
// operator withdrawing at once — several feeds fetch prices best-effort from a
// second file. Prices must survive it.
func TestIngest_KeepsPricesWhenAPassReturnsNoTariffsAtAll(t *testing.T) {
	st := setup(t)
	cpo := store.CPO{
		ID: "mockcpo", Name: "Mock CPO", OCPIVersion: "2.1.1",
		// Any full-snapshot type exercises the guard; the feeds it protects are
		// the ones that fetch prices best-effort (ocpi_file, a "module not
		// supported" tariffs endpoint), which need a server this mock doesn't play.
		SourceType: "ocpi", PollCron: "0 4 * * *", Enabled: true,
	}
	feed := newMockFeed("test-token")
	srv := feed.server()
	defer srv.Close()
	cpo.OCPIBaseURL = srv.URL + "/"
	if err := st.UpsertCPO(context.Background(), cpo); err != nil {
		t.Fatal(err)
	}

	locs := []ocpi.Location{locWithTariff("LOC1", "EVSE1", "TAR1", "51.05432", "3.72500")}
	priceRun(t, st, cpo, feed, locs, []ocpi.Tariff{tariffWithID("TAR1", 0.45)})
	if open, _ := openAndTotalVersions(t, st, "EVSE1"); open != 1 {
		t.Fatalf("open versions = %d after the first pass, want 1", open)
	}

	priceRun(t, st, cpo, feed, locs, nil) // the whole tariff file went missing

	if open, _ := openAndTotalVersions(t, st, "EVSE1"); open != 1 {
		t.Errorf("open versions = %d, want 1: a tariff-wide outage must not wipe prices", open)
	}
}
