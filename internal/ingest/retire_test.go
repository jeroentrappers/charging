package ingest

import "testing"

func TestFullSnapshotSource(t *testing.T) {
	full := []string{"ocpi", "ocpi_file", "ocpi_file_gz", "datex", "datex_afir", "bnetza", "irve"}
	for _, s := range full {
		if !fullSnapshotSource(s) {
			t.Errorf("%q should be prunable (full snapshot)", s)
		}
	}
	// Push/delta and crawl feeds must never be pruned.
	for _, s := range []string{"mobilithek", "monta", "", "unknown"} {
		if fullSnapshotSource(s) {
			t.Errorf("%q must NOT be prunable", s)
		}
	}
}
