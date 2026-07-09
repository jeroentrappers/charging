package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpool_EnqueueClaimFIFO(t *testing.T) {
	dir := t.TempDir()
	bodies := [][]byte{[]byte(`{"a":1}`), []byte("  <x/>"), []byte(`{"b":2}`)}
	for i, b := range bodies {
		if err := SpoolPush(dir, b); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
		time.Sleep(time.Millisecond) // distinct unixnano prefixes for stable FIFO
	}

	inc := filepath.Join(dir, "incoming")
	ents, _ := os.ReadDir(inc)
	if len(ents) != 3 {
		t.Fatalf("incoming=%d want 3", len(ents))
	}
	// XML is sniffed past leading whitespace → one .xml file.
	xml := 0
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".xml") {
			xml++
		}
	}
	if xml != 1 {
		t.Errorf("xml files=%d want 1", xml)
	}

	// listOldest returns oldest-first (FIFO by unixnano prefix), capped.
	all := listOldest(inc, spoolBatch)
	if len(all) != 3 {
		t.Fatalf("listOldest=%d want 3", len(all))
	}
	if !(all[0] < all[1] && all[1] < all[2]) {
		t.Errorf("not FIFO: %v", all)
	}
	// The batch cap is honoured.
	if got := listOldest(inc, 2); len(got) != 2 || got[0] != all[0] || got[1] != all[1] {
		t.Errorf("capped listOldest=%v want first two of %v", got, all)
	}

	// Claiming (rename into processing/) removes it from incoming — the atomic
	// hand-off the scanner relies on.
	proc := filepath.Join(dir, "processing")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(inc, all[0]), filepath.Join(proc, all[0])); err != nil {
		t.Fatalf("claim rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inc, all[0])); !os.IsNotExist(err) {
		t.Errorf("claimed file still in incoming")
	}
}
