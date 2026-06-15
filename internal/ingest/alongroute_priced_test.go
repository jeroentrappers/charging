package ingest

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/appmire/charging/internal/store"
)

// Along a route, an unpriced charger sitting right on the line must not crowd out
// a priced charger slightly off it — priced chargers are preferred, and unpriced
// ones surface only when the corridor has no priced charger at all.
func TestChargersAlongRoute_PrefersPricedWithFallback(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	// Priced charger (GP JOULE) at 54.586, 8.99.
	if _, _, err := e.IngestMobilithekPush(ctx, []byte(mobTablePush)); err != nil {
		t.Fatalf("seed priced: %v", err)
	}

	// Unpriced charger right on the route line (lon 8.995), closer than the priced one.
	if _, err := st.Pool.Exec(ctx, `INSERT INTO cpo (id,name,ocpi_base_url,source_type,enabled) VALUES ('unpriced','Unpriced Co','push://unpriced','monta',false) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed cpo: %v", err)
	}
	if _, err := st.Pool.Exec(ctx,
		`INSERT INTO charger (cpo_id, evse_uid, connector_id, geom, power_kw, current_type)
		 VALUES ('unpriced','U*1','1', ST_SetSRID(ST_MakePoint(8.995,54.586),4326)::geography, 50, 'DC')`); err != nil {
		t.Fatalf("seed unpriced charger: %v", err)
	}

	// Route line runs through the unpriced charger (off=0); the priced one is ~320m off.
	const line = "LINESTRING(8.995 54.587, 8.995 54.585)"
	q := store.NearbyQuery{StaleAfter: 0, Limit: 60, IncludePrivate: true}

	res, err := st.ChargersAlongRoute(ctx, line, 2500, q)
	if err != nil {
		t.Fatalf("along-route: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results; want 1 (only the priced charger)", len(res))
	}
	if res[0].PriceEUR == nil {
		t.Errorf("returned an unpriced charger; want the priced one")
	}
	if res[0].CPOID == "unpriced" {
		t.Errorf("unpriced charger surfaced despite a priced one on the corridor")
	}

	// Remove all prices → the corridor has none → unpriced is returned as fallback.
	if _, err := st.Pool.Exec(ctx, `DELETE FROM tariff_version`); err != nil {
		t.Fatalf("unprice: %v", err)
	}
	res, err = st.ChargersAlongRoute(ctx, line, 2500, q)
	if err != nil {
		t.Fatalf("along-route fallback: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("fallback returned nothing; want unpriced chargers when no priced ones exist")
	}
}
