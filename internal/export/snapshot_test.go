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

func TestRegionKey(t *testing.T) {
	cases := []struct{ country, postal, want string }{
		{"NL", "1011 AB", "NL-10"},
		{"FR", "75001", "FR-75"},
		{"BE", "", "BE-XX"},
		{"", "x", "XX-XX"},
		{"", "", "XX-XX"},
		{"DE", "10115", "DE-10"},
	}
	for _, c := range cases {
		if got := regionKey(c.country, c.postal); got != c.want {
			t.Errorf("regionKey(%q,%q)=%q want %q", c.country, c.postal, got, c.want)
		}
	}
}

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

func TestWriteRegions(t *testing.T) {
	dir := t.TempDir()
	s := &Snapshotter{Dir: dir, FullEvery: time.Hour}
	now := time.Unix(1700000000, 0).UTC()
	if err := s.ensureDir(); err != nil {
		t.Fatal(err)
	}

	price := 0.42
	a := tariffRaw(t, 0.40)
	// 5 rows spanning NL-10 (2 rows, one priced + one with a tariff), NL-20, FR-75.
	// Already ordered by (country, postal) the way ExportStream guarantees.
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

	if total != 5 {
		t.Errorf("total=%d want 5", total)
	}
	if priced != 1 {
		t.Errorf("priced=%d want 1", priced)
	}
	wantRegions := []string{"FR-75", "NL-10", "NL-20"}
	if !reflect.DeepEqual(regions, wantRegions) {
		t.Errorf("regions=%v want %v", regions, wantRegions)
	}

	// Every region must have all four file kinds present on disk.
	for _, r := range wantRegions {
		for _, rel := range []string{
			"ndjson/" + r + ".ndjson",
			"geojson/" + r + ".geojson",
			"ocpi/" + r + "-locations.json",
			"ocpi/" + r + "-tariffs.json",
		} {
			if _, ok := files[rel]; !ok {
				t.Errorf("missing FileInfo for %s", rel)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				t.Errorf("missing file on disk %s: %v", rel, err)
			}
		}
	}

	// NL-10 holds rows 1 and 2 -> 2 NDJSON lines.
	if n := countLines(t, filepath.Join(dir, "ndjson", "NL-10.ndjson")); n != 2 {
		t.Errorf("NL-10 ndjson lines=%d want 2", n)
	}
	// FR-75 holds rows 4 and 5 -> 2 lines.
	if n := countLines(t, filepath.Join(dir, "ndjson", "FR-75.ndjson")); n != 2 {
		t.Errorf("FR-75 ndjson lines=%d want 2", n)
	}
	// NL-20 holds row 3 -> 1 line.
	if n := countLines(t, filepath.Join(dir, "ndjson", "NL-20.ndjson")); n != 1 {
		t.Errorf("NL-20 ndjson lines=%d want 1", n)
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
	if _, err := os.Stat(filepath.Join(dir, "ndjson", "FR-75.ndjson")); err != nil {
		t.Fatalf("FR-75 should exist after first run: %v", err)
	}

	// Second run: only NL-10 remains; FR-75 must be pruned across all subdirs.
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
	if !reflect.DeepEqual(regions2, []string{"NL-10"}) {
		t.Errorf("regions2=%v want [NL-10]", regions2)
	}
	for _, rel := range []string{
		"ndjson/FR-75.ndjson",
		"geojson/FR-75.geojson",
		"ocpi/FR-75-locations.json",
		"ocpi/FR-75-tariffs.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("stale file %s should have been pruned (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ndjson", "NL-10.ndjson")); err != nil {
		t.Errorf("NL-10 should still exist: %v", err)
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
