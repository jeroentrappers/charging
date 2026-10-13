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

func TestStats_AfterIngest(t *testing.T) {
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
	if err := NewEngine(st, log).RunPrices(ctx, source.Source{CPO: cpo, Token: token}); err != nil {
		t.Fatal(err)
	}

	// Overview: one priced AC charger, with an "all" rollup row.
	ov, err := st.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Chargers != 1 || ov.PricedChargers != 1 {
		t.Fatalf("overview counts: %+v", ov)
	}
	var hasAll, hasAC bool
	for _, a := range ov.ByCurrentType {
		switch a.Group {
		case "all":
			hasAll = true
		case "AC":
			hasAC = true
			if a.AvgEUR == nil || *a.AvgEUR <= 0 {
				t.Fatalf("AC avg should be positive: %+v", a)
			}
		}
	}
	if !hasAll || !hasAC {
		t.Fatalf("expected AC + all rollup groups, got %+v", ov.ByCurrentType)
	}

	// Sessions: the AC matrix should be present.
	sess, err := st.SessionStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range sess {
		seen[s.Session] = true
	}
	if !seen["charge1080_ac22"] || !seen["urban_ac22"] {
		t.Fatalf("expected AC session profiles in stats, got %v", seen)
	}

	// Regions: the fixture is in Gent.
	regions, err := st.RegionStats(ctx, "city", 10)
	if err != nil {
		t.Fatal(err)
	}
	var foundGent bool
	for _, rg := range regions {
		if rg.Group == "Gent" && rg.Count == 1 {
			foundGent = true
		}
	}
	if !foundGent {
		t.Fatalf("expected Gent region, got %+v", regions)
	}

	// Trend: 12 monthly buckets; the current month has the active version once
	// the rollup has been refreshed.
	if _, _, err := st.RefreshPriceMonths(ctx); err != nil {
		t.Fatal(err)
	}
	trend, err := st.PriceTrend(ctx, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 12 {
		t.Fatalf("want 12 trend points, got %d", len(trend))
	}
	last := trend[len(trend)-1]
	if last.Count < 1 || last.AvgEUR == nil {
		t.Fatalf("current month should have an active priced version: %+v", last)
	}
}

// TestPriceRollup_Incremental checks the tariff_price_month rollup: an empty
// rollup backfills the whole history, a month computed after it ended is final
// and never recomputed, and a month still open at its last refresh is.
func TestPriceRollup_Incremental(t *testing.T) {
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
	if err := NewEngine(st, log).RunPrices(ctx, source.Source{CPO: cpo, Token: token}); err != nil {
		t.Fatal(err)
	}
	// History: a closed €10 version spanning the months three and two back, next
	// to the open version the ingest just wrote (current month only).
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO tariff_version (charger_id, tariff_hash, price_components, comparable_price_eur, observed_from, observed_to)
		SELECT charger_id, 'old', '[]', 10,
		       date_trunc('month', now()) - interval '3 months' + interval '1 day',
		       date_trunc('month', now()) - interval '2 months' + interval '1 day'
		FROM tariff_version WHERE observed_to IS NULL`); err != nil {
		t.Fatal(err)
	}

	from, _, err := st.RefreshPriceMonths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if from.Year() != 1 {
		t.Fatalf("empty rollup should backfill from the beginning, got from=%v", from)
	}
	trend, err := st.PriceTrend(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 4 {
		t.Fatalf("want 4 trend points, got %d", len(trend))
	}
	for i, want := range []int{1, 1, 0, 1} {
		if trend[i].Count != want {
			t.Fatalf("month %s: count %d, want %d (%+v)", trend[i].Month, trend[i].Count, want, trend)
		}
	}
	if trend[0].AvgEUR == nil || *trend[0].AvgEUR != 10 {
		t.Fatalf("month %s: avg %v, want 10", trend[0].Month, trend[0].AvgEUR)
	}
	if trend[2].AvgEUR != nil {
		t.Fatalf("month %s has no versions, avg should be null: %v", trend[2].Month, *trend[2].AvgEUR)
	}

	// Finished months are final: corrupt one and a refresh must leave it alone,
	// recomputing only the current month.
	if _, err := st.Pool.Exec(ctx, `
		UPDATE tariff_price_month SET n = 999
		WHERE month = (date_trunc('month', now()) - interval '3 months')::date`); err != nil {
		t.Fatal(err)
	}
	from, months, err := st.RefreshPriceMonths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := monthStart(time.Now()); !from.Equal(want) || months != 1 {
		t.Fatalf("incremental refresh: from=%v months=%d, want from=%v months=1", from, months, want)
	}
	if trend, err = st.PriceTrend(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if trend[0].Count != 999 || trend[3].Count != 1 {
		t.Fatalf("final month recomputed or current month lost: %+v", trend)
	}

	// A month last computed before it ended (e.g. the API was down over the
	// month boundary) is not final, so the next refresh starts from it.
	if _, err := st.Pool.Exec(ctx, `
		UPDATE tariff_price_month SET computed_at = month
		WHERE month = (date_trunc('month', now()) - interval '3 months')::date`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.RefreshPriceMonths(ctx); err != nil {
		t.Fatal(err)
	}
	if trend, err = st.PriceTrend(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if trend[0].Count != 1 {
		t.Fatalf("non-final month not recomputed: %+v", trend)
	}
}

// monthStart is the first day of t's month as Postgres returns a date column.
func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
