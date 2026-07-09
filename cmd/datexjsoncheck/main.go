// Command datexjsoncheck validates DATEX II AFIR *JSON* publications. The JSON
// encoding has no official XSD, so the reference check — mirroring how the XML
// gate (scripts/validate-datex.sh) uses xmllint — is that our canonical
// consumer (the same parser that reads the live German Mobilithek JSON feeds)
// decodes the file into a recognized publication with usable content.
//
//	go run ./cmd/datexjsoncheck file.json [more.json ...]
//
// Exits non-zero if any file fails.
package main

import (
	"fmt"
	"os"

	"github.com/appmire/charging/internal/datex"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: datexjsoncheck <file.json> [file.json ...]")
		os.Exit(2)
	}
	fail := false
	for _, path := range os.Args[1:] {
		if err := checkFile(path); err != nil {
			fmt.Printf("FAIL  %s: %v\n", path, err)
			fail = true
			continue
		}
	}
	if fail {
		os.Exit(1)
	}
}

func checkFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := datex.ParseAFIRJSON(data)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	switch doc.Kind {
	case "table":
		if len(doc.Connectors) == 0 {
			return fmt.Errorf("table publication has no connectors")
		}
		fmt.Printf("OK    %s (%d bytes) — AFIR JSON table, %d connectors, %d tariffs\n",
			path, len(data), len(doc.Connectors), len(doc.Tariffs))
	case "status":
		fmt.Printf("OK    %s (%d bytes) — AFIR JSON status, %d refill-point statuses\n",
			path, len(data), len(doc.Statuses))
	default:
		return fmt.Errorf("not a recognized AFIR publication (kind=%q)", doc.Kind)
	}
	return nil
}
