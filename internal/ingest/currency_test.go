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

func TestComparableCurrency(t *testing.T) {
	for _, c := range []string{"", "EUR", "eur"} {
		if !comparableCurrency(c) {
			t.Errorf("comparableCurrency(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"PLN", "DKK", "CHF", "SEK"} {
		if comparableCurrency(c) {
			t.Errorf("comparableCurrency(%q) = true, want false", c)
		}
	}
}

// A non-euro tariff must be stored with its components and currency but WITHOUT a
// comparable price: comparable_price_eur is a euro amount and nothing converts
// currencies, so ranking a 2.40 PLN/kWh tariff as 2.40 EUR/kWh would misprice it
// more than fourfold. Such chargers show their published tariff and sort as
// unpriced.
func TestIngest_NonEuroTariffGetsNoComparablePrice(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	const token = "test-token"
	feed := newMockFeed(token)
	srv := feed.server()
	defer srv.Close()

	cpo := store.CPO{
		ID: "mockpln", Name: "Mock PLN CPO",
		OCPIBaseURL: srv.URL + "/", OCPIVersion: "2.1.1",
		PollCron: "0 4 * * *", Enabled: true, Country: "PL",
	}
	if err := st.UpsertCPO(ctx, cpo); err != nil {
		t.Fatal(err)
	}
	src := source.Source{CPO: cpo, Token: token}
	eng := NewEngine(st, log)

	zloty := sampleTariff(2.40, time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC))
	zloty.Currency = "PLN"
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{zloty})
	if err := eng.RunPrices(ctx, src); err != nil {
		t.Fatal(err)
	}

	id := singleChargerID(t, st)
	if p := currentPrice(t, st, id); p != nil {
		t.Errorf("comparable_price_eur = %v, want NULL for a PLN tariff", *p)
	}
	if prices := currentPrices(t, st, id); len(prices) != 0 {
		t.Errorf("comparable_prices = %v, want empty for a PLN tariff", prices)
	}

	// The published tariff itself is still recorded, currency included, so the
	// charger can show its real price.
	var currency string
	var components []byte
	if err := st.Pool.QueryRow(ctx,
		`SELECT currency, price_components FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`,
		id).Scan(&currency, &components); err != nil {
		t.Fatal(err)
	}
	if currency != "PLN" {
		t.Errorf("stored currency = %q, want PLN", currency)
	}
	if len(components) == 0 || string(components) == "{}" {
		t.Errorf("price components should still be stored, got %q", components)
	}
}
