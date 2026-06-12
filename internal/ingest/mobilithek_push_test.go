package ingest

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/store"
)

func TestMobilithekCPOID(t *testing.T) {
	cases := []struct{ nid, country, want string }{
		{"DE-NAP-GPJOULECONNECT", "DE", "mob-gpjouleconnect"},
		{"DE-NAP-AUDI", "DE", "mob-audi"},
		{"NAP-FOO", "DE", "mob-foo"},
		{"", "DE", "mobilithek"},
	}
	for _, c := range cases {
		if got := mobilithekCPOID(datex.AFIRCreator{Country: c.country, NationalIdentifier: c.nid}); got != c.want {
			t.Errorf("mobilithekCPOID(%q)=%q want %q", c.nid, got, c.want)
		}
	}
}

// A compact but structurally-real GP JOULE table push (one AC Type 2 @11 kW,
// ad-hoc €0.55/kWh) and a matching status push (blocked).
const mobTablePush = `{"payload":{"profileNameG":"AFIR Energy Infrastructure",
"aegiEnergyInfrastructureTablePublication":{"publicationCreator":{"country":"DE","nationalIdentifier":"DE-NAP-GPJOULECONNECT"},
"energyInfrastructureTable":[{"idG":"t1","tableName":"GP JOULE CONNECT","energyInfrastructureSite":[
{"idG":"site-1","locationReference":{"locAreaLocation":{"coordinatesForDisplay":{"latitude":54.586,"longitude":8.99},
"locLocationExtensionG":{"FacilityLocation":{"address":{"postcode":"25821","city":{"values":[{"lang":"en","value":"Struckum"}]},
"addressLine":[{"type":{"value":"street"},"text":{"values":[{"value":"Kennedy Weg 4"}]}}]}}}}},
"operator":{"afacAnOrganisation":{"name":{"values":[{"value":"GP JOULE CONNECT"}]}}},
"energyInfrastructureStation":[{"idG":"st-1","totalMaximumPower":11000,"refillPoint":[{"aegiElectricChargingPoint":{"idG":"cp-1",
"currentType":{"value":"ac"},"connector":[{"connectorType":{"value":"iec62196T2"},"maxPowerAtSocket":11000}],
"electricEnergy":[{"energyRate":[{"idG":"rate-1","ratePolicy":{"value":"adHoc"},"applicableCurrency":["EUR"],
"energyPrice":[{"priceType":{"value":"pricePerKWh"},"value":0.55}]}]}]}}]}]}]}]}}}`

const mobStatusPush = `{"payload":{"aegiEnergyInfrastructureStatusPublication":{"publicationCreator":{"country":"DE","nationalIdentifier":"DE-NAP-GPJOULECONNECT"},
"energyInfrastructureSiteStatus":[{"reference":{"idG":"site-1"},"energyInfrastructureStationStatus":[{"reference":{"idG":"st-1"},
"refillPointStatus":[{"aegiElectricChargingPointStatus":{"reference":{"idG":"cp-1"},"status":{"value":"blocked"}}}]}]}]}}}`

func TestIngestMobilithekPush_TableThenStatus(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	e := NewEngine(st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	// --- table push: locations + ad-hoc price ---
	kind, _, err := e.IngestMobilithekPush(ctx, []byte(mobTablePush))
	if err != nil || kind != "table" {
		t.Fatalf("table ingest: kind=%q err=%v", kind, err)
	}
	// CPO row auto-created with the right country (so attribution/export works).
	cpo, found, err := st.GetCPO(ctx, "mob-gpjouleconnect")
	if err != nil || !found {
		t.Fatalf("cpo not seeded: found=%v err=%v", found, err)
	}
	if cpo.Country != "DE" || cpo.SourceType != "mobilithek" {
		t.Errorf("cpo = %+v; want country DE, source_type mobilithek", cpo)
	}
	// The connector exists and is priced.
	conns, err := st.ChargersForEVSE(ctx, "mob-gpjouleconnect", "cp-1")
	if err != nil || len(conns) != 1 {
		t.Fatalf("ChargersForEVSE = %d (%v); want 1", len(conns), err)
	}
	res, err := st.CheapestNearby(ctx, store.NearbyQuery{Lat: 54.586, Lon: 8.99, RadiusM: 2000, Limit: 5, IncludePrivate: true})
	if err != nil || len(res) == 0 {
		t.Fatalf("nearby = %d (%v); want >=1", len(res), err)
	}
	c := res[0]
	if c.PriceEUR == nil {
		t.Errorf("charger not priced (PriceEUR nil)")
	}
	if c.PlugType != "IEC_62196_T2" || c.PowerKW != 11 || c.CurrentType != "AC" {
		t.Errorf("charger = plug %q power %.1f current %q; want IEC_62196_T2/11/AC", c.PlugType, c.PowerKW, c.CurrentType)
	}

	// --- status push: blocked -> OUTOFORDER on the same refill point ---
	kind, n, err := e.IngestMobilithekPush(ctx, []byte(mobStatusPush))
	if err != nil || kind != "status" || n != 1 {
		t.Fatalf("status ingest: kind=%q n=%d err=%v", kind, n, err)
	}
	var got string
	if err := st.Pool.QueryRow(ctx, `SELECT status FROM charger_status WHERE charger_id=$1`, conns[0].ID).Scan(&got); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if got != "OUTOFORDER" {
		t.Errorf("status = %q; want OUTOFORDER (blocked)", got)
	}

	// A synthetic test packet ingests nothing.
	if k, _, _ := e.IngestMobilithekPush(ctx, []byte(`{"payload":[{"commonGenericPublication":{}}]}`)); k != "" {
		t.Errorf("synthetic test packet kind=%q; want empty", k)
	}
}
