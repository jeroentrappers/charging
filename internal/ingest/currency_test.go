package ingest

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/appmire/charging/internal/fx"
	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// Euro tariffs must never depend on the FX feed, and without a rate source a
// foreign-currency tariff must report "not comparable" rather than assume parity.
func TestToEUR_WithoutFX(t *testing.T) {
	e := &Engine{} // no FX configured
	ctx := context.Background()
	for _, c := range []string{"", "EUR", "eur"} {
		if f, ok := e.toEUR(ctx, 10, c); !ok || f != 10 {
			t.Errorf("toEUR(10, %q) = %v, %v; want 10, true", c, f, ok)
		}
	}
	for _, c := range []string{"PLN", "DKK", "CHF", "SEK"} {
		if _, ok := e.toEUR(ctx, 10, c); ok {
			t.Errorf("toEUR(10, %q) reported comparable with no FX source", c)
		}
	}
}

// With rates available, a foreign-currency amount converts at the ECB rate.
func TestToEUR_WithFX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
 <Cube><Cube time='` + time.Now().Format("2006-01-02") + `'>
   <Cube currency='PLN' rate='4.3190'/><Cube currency='CHF' rate='0.9406'/>
 </Cube></Cube></gesmes:Envelope>`))
	}))
	defer srv.Close()

	e := &Engine{FX: &fx.Cache{URL: srv.URL}}
	got, ok := e.toEUR(context.Background(), 4.319, "PLN")
	if !ok {
		t.Fatal("want PLN to be comparable with rates available")
	}
	if diff := got - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("4.319 PLN = %v EUR, want 1.0", got)
	}
	// An unknown currency stays not-comparable even when rates loaded.
	if _, ok := e.toEUR(context.Background(), 10, "XYZ"); ok {
		t.Error("unknown currency should not be comparable")
	}
}

// With no FX source configured, a non-euro tariff must be stored with its
// components and currency but WITHOUT a comparable price: ranking a 2.40 PLN/kWh
// tariff as 2.40 EUR/kWh would misprice it more than fourfold. Such chargers show
// their published tariff and sort as unpriced.
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

// End to end: with rates available, a PLN tariff gets a euro comparable price
// (so it ranks against euro chargers) while its components stay in PLN.
func TestIngest_NonEuroTariffConvertedWithFX(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	const token = "test-token"
	feed := newMockFeed(token)
	srv := feed.server()
	defer srv.Close()

	ecb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
 <Cube><Cube time='` + time.Now().Format("2006-01-02") + `'>
   <Cube currency='PLN' rate='4.0000'/>
 </Cube></Cube></gesmes:Envelope>`))
	}))
	defer ecb.Close()

	cpo := store.CPO{
		ID: "mockfx", Name: "Mock PLN CPO",
		OCPIBaseURL: srv.URL + "/", OCPIVersion: "2.1.1",
		PollCron: "0 4 * * *", Enabled: true, Country: "PL",
	}
	if err := st.UpsertCPO(ctx, cpo); err != nil {
		t.Fatal(err)
	}
	src := source.Source{CPO: cpo, Token: token}

	// The same tariff twice, once in EUR and once in PLN at a 4.0 rate: the PLN
	// charger's comparable price must come out at a quarter of the euro one.
	euro := sampleTariff(2.40, time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC))
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{euro})
	engEUR := NewEngine(st, log)
	if err := engEUR.RunPrices(ctx, src); err != nil {
		t.Fatal(err)
	}
	idEUR := singleChargerID(t, st)
	pEUR := currentPrice(t, st, idEUR)
	if pEUR == nil || *pEUR <= 0 {
		t.Fatalf("euro baseline price = %v", pEUR)
	}

	// Re-ingest the identical tariff as PLN with FX enabled.
	zloty := euro
	zloty.Currency = "PLN"
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{zloty})
	eng := NewEngine(st, log)
	eng.FX = &fx.Cache{URL: ecb.URL}
	if err := eng.RunPrices(ctx, src); err != nil {
		t.Fatal(err)
	}
	pPLN := currentPrice(t, st, idEUR)
	if pPLN == nil {
		t.Fatal("want a comparable price for a PLN tariff when rates are available")
	}
	if want := *pEUR / 4; *pPLN < want-0.01 || *pPLN > want+0.01 {
		t.Errorf("PLN comparable = %.4f, want ~%.4f (euro %.4f / rate 4.0)", *pPLN, want, *pEUR)
	}
	// The per-profile matrix is converted too, not just the headline.
	for k, v := range currentPrices(t, st, idEUR) {
		if v <= 0 || v > *pEUR {
			t.Errorf("profile %s = %v, expected a converted (smaller) euro amount vs %v", k, v, *pEUR)
		}
	}
	// The published components stay in the source currency.
	var currency string
	if err := st.Pool.QueryRow(ctx,
		`SELECT currency FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`, idEUR).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if currency != "PLN" {
		t.Errorf("stored currency = %q, want PLN", currency)
	}
}

// A foreign-currency tariff first ingested with no rates available must pick up
// its euro comparable on a later pass, even though the published tariff — and so
// its hash — never changes. Without this it would sort as unpriced forever.
func TestIngest_BackfillsEuroPricesWhenRatesArriveLater(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	const token = "test-token"
	feed := newMockFeed(token)
	srv := feed.server()
	defer srv.Close()

	ecb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
 <Cube><Cube time='` + time.Now().Format("2006-01-02") + `'>
   <Cube currency='PLN' rate='4.0000'/>
 </Cube></Cube></gesmes:Envelope>`))
	}))
	defer ecb.Close()

	cpo := store.CPO{
		ID: "mockbackfill", Name: "Mock PLN CPO",
		OCPIBaseURL: srv.URL + "/", OCPIVersion: "2.1.1",
		PollCron: "0 4 * * *", Enabled: true, Country: "PL",
	}
	if err := st.UpsertCPO(ctx, cpo); err != nil {
		t.Fatal(err)
	}
	src := source.Source{CPO: cpo, Token: token}

	zloty := sampleTariff(2.40, time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC))
	zloty.Currency = "PLN"
	feed.set([]ocpi.Location{sampleLocation("AVAILABLE")}, []ocpi.Tariff{zloty})

	// Pass 1: no FX configured -> stored, but not comparable.
	if err := NewEngine(st, log).RunPrices(ctx, src); err != nil {
		t.Fatal(err)
	}
	id := singleChargerID(t, st)
	if p := currentPrice(t, st, id); p != nil {
		t.Fatalf("expected no comparable price without rates, got %v", *p)
	}

	// Pass 2: same tariff (same hash), now with rates -> backfilled in place.
	eng := NewEngine(st, log)
	eng.FX = &fx.Cache{URL: ecb.URL}
	if err := eng.RunPrices(ctx, src); err != nil {
		t.Fatal(err)
	}
	p := currentPrice(t, st, id)
	if p == nil {
		t.Fatal("want the euro comparable to be backfilled once rates are available")
	}
	if *p <= 0 {
		t.Errorf("backfilled comparable = %v", *p)
	}
	if prices := currentPrices(t, st, id); len(prices) == 0 {
		t.Error("want the per-profile matrix backfilled too")
	}
	// Still one version: a backfill must not open a new SCD2 row.
	var versions int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM tariff_version WHERE charger_id=$1`, id).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Errorf("tariff versions = %d, want 1 (backfill must not version)", versions)
	}
}
