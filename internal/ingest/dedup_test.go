package ingest

import (
	"context"
	"testing"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/store"
)

// Cross-source dedup: a BNetzA (location-only register) charger co-located with
// a richer Mobilithek (priced) charger is hidden from nearby + export, while a
// BNetzA charger with no richer neighbour is kept.
func TestCrossSourceDedup(t *testing.T) {
	ctx := context.Background()
	st := setup(t)

	for _, c := range []store.CPO{
		{ID: "bnetza", Name: "Bundesnetzagentur (DE)", OCPIBaseURL: "file://bnetza", Country: "DE", SourceType: "bnetza", Enabled: false},
		{ID: "mob-gpjouleconnect", Name: "GP JOULE CONNECT", OCPIBaseURL: "push://mob", Country: "DE", SourceType: "mobilithek", Enabled: false},
	} {
		if err := st.SeedCPO(ctx, c); err != nil {
			t.Fatalf("seed cpo %s: %v", c.ID, err)
		}
	}

	const plug = "IEC_62196_T2"
	mk := func(cpo, evse string, lat, lon float64, name string) {
		if _, err := st.UpsertCharger(ctx, model.Connector{
			CPOID: cpo, EVSEUID: evse, ConnectorID: "1", Lat: lat, Lon: lon,
			PowerKW: 11, PlugType: plug, CurrentType: "AC", Name: name,
		}); err != nil {
			t.Fatalf("upsert %s/%s: %v", cpo, evse, err)
		}
	}
	// Rich (priced) charger + a BNetzA duplicate ~11 m away (same plug) + a lone
	// BNetzA charger ~445 m away with no richer neighbour.
	mk("mob-gpjouleconnect", "cp-1", 54.5860, 8.9900, "GP JOULE CONNECT · Struckum")
	mk("bnetza", "bn-dup", 54.5861, 8.9900, "GP Joule Connect GmbH · Struckum")
	mk("bnetza", "bn-lone", 54.5900, 8.9900, "Some Other Operator · Solo")

	names := func(q store.NearbyQuery) map[string]bool {
		res, err := st.CheapestNearby(ctx, q)
		if err != nil {
			t.Fatalf("nearby: %v", err)
		}
		m := map[string]bool{}
		for _, r := range res {
			m[r.Name] = true
		}
		return m
	}

	base := store.NearbyQuery{Lat: 54.586, Lon: 8.99, RadiusM: 2000, Limit: 50, IncludePrivate: true}

	// Default: the co-located BNetzA dup is suppressed; rich + lone kept.
	def := names(base)
	if !def["GP JOULE CONNECT · Struckum"] {
		t.Error("rich (mobilithek) charger missing from nearby")
	}
	if def["GP Joule Connect GmbH · Struckum"] {
		t.Error("co-located BNetzA duplicate was NOT suppressed")
	}
	if !def["Some Other Operator · Solo"] {
		t.Error("lone BNetzA charger (no richer neighbour) was wrongly suppressed")
	}

	// IncludeDuplicates: nothing suppressed.
	dq := base
	dq.IncludeDuplicates = true
	all := names(dq)
	if !all["GP Joule Connect GmbH · Struckum"] {
		t.Error("IncludeDuplicates should keep the co-located BNetzA dup")
	}

	// Export applies the same suppression (always on).
	seen := map[string]bool{}
	if err := st.ExportStream(ctx, func(e store.ExportCharger) error {
		seen[e.EVSEUID] = true
		return nil
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if seen["bn-dup"] {
		t.Error("export included the co-located BNetzA duplicate")
	}
	if !seen["cp-1"] || !seen["bn-lone"] {
		t.Errorf("export missing expected chargers: cp-1=%v bn-lone=%v", seen["cp-1"], seen["bn-lone"])
	}
}
