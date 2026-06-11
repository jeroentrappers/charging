package export

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/appmire/charging/internal/store"
)

// sliceStream returns a stream func over an in-memory slice (no DB), driving
// writeRegions exactly the way ExportStream would.
func sliceStream(rows []store.ExportCharger) func(func(store.ExportCharger) error) error {
	return func(yield func(store.ExportCharger) error) error {
		for _, r := range rows {
			if err := yield(r); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestWriteRegionsChunksBySize(t *testing.T) {
	dir := t.TempDir()
	// Tiny chunk target so each row except the last of a country forces a flush:
	// with ChunkBytes=1 every row exceeds the target → one row per chunk.
	s := &Snapshotter{Dir: dir, FullEvery: time.Hour, ChunkBytes: 1}
	now := time.Unix(1700000000, 0).UTC()
	if err := s.ensureDir(); err != nil {
		t.Fatal(err)
	}

	price := 0.42
	a := tariffRaw(t, 0.40)
	// 3 NL rows then 2 FR rows, ordered by (country, postal) like ExportStream.
	rows := []store.ExportCharger{
		{ID: 1, CPOID: "dotnl", Country: "NL", PostalCode: "1011 AB", EVSEUID: "E1", ConnectorID: "1", Lat: 52.3, Lon: 4.9, PowerKW: 22, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "AVAILABLE", Currency: "EUR", PriceEUR: &price, Components: a},
		{ID: 2, CPOID: "dotnl", Country: "NL", PostalCode: "1011 CD", EVSEUID: "E2", ConnectorID: "1", Lat: 52.3, Lon: 4.9, PowerKW: 22, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "CHARGING", Currency: "EUR", Components: a},
		{ID: 3, CPOID: "dotnl", Country: "NL", PostalCode: "2000", EVSEUID: "E3", ConnectorID: "1", Lat: 52.0, Lon: 4.3, PowerKW: 50, CurrentType: "DC", PlugType: "IEC_62196_T2_COMBO", Status: "AVAILABLE", Currency: "EUR"},
		{ID: 4, CPOID: "irve", Country: "FR", PostalCode: "75001", EVSEUID: "E4", ConnectorID: "1", Lat: 48.8, Lon: 2.3, PowerKW: 11, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "UNKNOWN"},
		{ID: 5, CPOID: "irve", Country: "FR", PostalCode: "75008", EVSEUID: "E5", ConnectorID: "1", Lat: 48.9, Lon: 2.3, PowerKW: 11, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "UNKNOWN"},
	}

	files, regions, total, priced, err := s.writeRegions(now, sliceStream(rows))
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || priced != 1 {
		t.Errorf("total=%d priced=%d want 5/1", total, priced)
	}
	// 3 NL chunks + 2 FR chunks, sequence numbered per country.
	wantRegions := []string{"FR-001", "FR-002", "NL-001", "NL-002", "NL-003"}
	if !reflect.DeepEqual(regions, wantRegions) {
		t.Errorf("regions=%v want %v", regions, wantRegions)
	}
	for _, r := range wantRegions {
		for _, rel := range []string{
			"ndjson/" + r + ".ndjson", "geojson/" + r + ".geojson",
			"ocpi/" + r + "-locations.json", "ocpi/" + r + "-tariffs.json",
		} {
			if _, ok := files[rel]; !ok {
				t.Errorf("missing FileInfo for %s", rel)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				t.Errorf("missing file on disk %s: %v", rel, err)
			}
		}
		if n := countLines(t, filepath.Join(dir, "ndjson", r+".ndjson")); n != 1 {
			t.Errorf("%s ndjson lines=%d want 1", r, n)
		}
	}
}

func TestWriteRegionsGroupsByCountry(t *testing.T) {
	dir := t.TempDir()
	// Large target so a whole country fits in one chunk.
	s := &Snapshotter{Dir: dir, FullEvery: time.Hour, ChunkBytes: 1 << 30}
	now := time.Unix(1700000000, 0).UTC()
	if err := s.ensureDir(); err != nil {
		t.Fatal(err)
	}
	rows := []store.ExportCharger{
		{ID: 1, CPOID: "dotnl", Country: "NL", PostalCode: "1011", EVSEUID: "E1", ConnectorID: "1", Currency: "EUR"},
		{ID: 2, CPOID: "dotnl", Country: "NL", PostalCode: "2000", EVSEUID: "E2", ConnectorID: "1", Currency: "EUR"},
		{ID: 3, CPOID: "irve", Country: "FR", PostalCode: "75001", EVSEUID: "E3", ConnectorID: "1", Currency: "EUR"},
	}
	_, regions, _, _, err := s.writeRegions(now, sliceStream(rows))
	if err != nil {
		t.Fatal(err)
	}
	wantRegions := []string{"FR-001", "NL-001"}
	if !reflect.DeepEqual(regions, wantRegions) {
		t.Errorf("regions=%v want %v", regions, wantRegions)
	}
	if n := countLines(t, filepath.Join(dir, "ndjson", "NL-001.ndjson")); n != 2 {
		t.Errorf("NL-001 lines=%d want 2 (whole country in one chunk)", n)
	}
}

func TestGenerateFullPrunesStaleRegions(t *testing.T) {
	dir := t.TempDir()
	s := &Snapshotter{Dir: dir, FullEvery: time.Hour}
	now := time.Unix(1700000000, 0).UTC()
	if err := s.ensureDir(); err != nil {
		t.Fatal(err)
	}

	// First run: two regions.
	rows := []store.ExportCharger{
		{ID: 1, CPOID: "dotnl", Country: "NL", PostalCode: "1011", EVSEUID: "E1", ConnectorID: "1", Currency: "EUR"},
		{ID: 2, CPOID: "irve", Country: "FR", PostalCode: "75001", EVSEUID: "E2", ConnectorID: "1", Currency: "EUR"},
	}
	files, _, _, _, err := s.writeRegions(now, sliceStream(rows))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.pruneRegions(files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ndjson", "FR-001.ndjson")); err != nil {
		t.Fatalf("FR-001 should exist after first run: %v", err)
	}

	// Second run: only NL remains; FR-001 must be pruned across all subdirs.
	rows2 := []store.ExportCharger{
		{ID: 1, CPOID: "dotnl", Country: "NL", PostalCode: "1011", EVSEUID: "E1", ConnectorID: "1", Currency: "EUR"},
	}
	files2, regions2, _, _, err := s.writeRegions(now, sliceStream(rows2))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.pruneRegions(files2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(regions2, []string{"NL-001"}) {
		t.Errorf("regions2=%v want [NL-001]", regions2)
	}
	for _, rel := range []string{
		"ndjson/FR-001.ndjson",
		"geojson/FR-001.geojson",
		"ocpi/FR-001-locations.json",
		"ocpi/FR-001-tariffs.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("stale file %s should have been pruned (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ndjson", "NL-001.ndjson")); err != nil {
		t.Errorf("NL-001 should still exist: %v", err)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimRight(string(b), "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}
