package ingest

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// Live end-to-end: ingest Monta locations, then run the crawl briefly and check
// that prices + statuses land. Set MONTA_CREDS="clientId:clientSecret".
func TestMontaCrawl_Live(t *testing.T) {
	creds := os.Getenv("MONTA_CREDS")
	if creds == "" {
		t.Skip("set MONTA_CREDS to run")
	}
	ctx := context.Background()
	st := setup(t)
	eng := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	src := source.Source{CPO: store.CPO{ID: "monta", Name: "Monta", OCPIBaseURL: "https://public-api.monta.com", SourceType: "monta"}, Token: creds}
	if err := st.UpsertCPO(ctx, src.CPO); err != nil {
		t.Fatal(err)
	}

	// 1) Ingest locations (bulk, fast).
	if err := eng.RunPrices(ctx, src); err != nil {
		t.Fatal(err)
	}
	var chargers int
	st.Pool.QueryRow(ctx, `SELECT count(*) FROM charger WHERE cpo_id='monta'`).Scan(&chargers)
	t.Logf("monta chargers ingested: %d", chargers)
	if chargers < 100 {
		t.Fatalf("expected many monta chargers, got %d", chargers)
	}

	// 2) Crawl for ~60s — should price + status a burst of Monta EVSEs.
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	eng.RunMontaCrawl(cctx, src)

	var statuses, priced int
	st.Pool.QueryRow(ctx, `SELECT count(*) FROM charger_status st JOIN charger c ON c.id=st.charger_id WHERE c.cpo_id='monta'`).Scan(&statuses)
	st.Pool.QueryRow(ctx, `SELECT count(*) FROM tariff_version tv JOIN charger c ON c.id=tv.charger_id WHERE c.cpo_id='monta' AND tv.observed_to IS NULL`).Scan(&priced)
	t.Logf("after crawl: statuses=%d priced=%d", statuses, priced)
	if priced < 1 {
		t.Fatalf("expected some Monta chargers to be priced after the crawl, got %d", priced)
	}
}
