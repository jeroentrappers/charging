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

	// claimOldest is FIFO and atomic (file moves out of incoming).
	proc := filepath.Join(dir, "processing")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	n1, ok := claimOldest(inc, proc)
	if !ok {
		t.Fatal("claim 1 failed")
	}
	n2, ok := claimOldest(inc, proc)
	if !ok {
		t.Fatal("claim 2 failed")
	}
	if !(n1 < n2) {
		t.Errorf("not FIFO: claimed %q before %q", n1, n2)
	}
	if _, err := os.Stat(filepath.Join(inc, n1)); !os.IsNotExist(err) {
		t.Errorf("claimed file still in incoming")
	}
}
