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

	// Trend: 12 monthly buckets; the current month has the active version.
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
