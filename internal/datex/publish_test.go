package datex

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/appmire/charging/internal/model"
)

func sampleSites() []PublishSite {
	return []PublishSite{{
		ID:   "1",
		Name: "ACME · Brussels",
		Lat:  50.85, Lon: 4.35,
		PostalCode: "1000", City: "Brussels", Street: "Rue Test 1",
		Stations: []PublishStation{{
			ID: "1",
			RefillPoints: []PublishRefillPoint{{
				ID:            "1",
				CurrentType:   "AC",
				ConnectorType: "IEC_62196_T2",
				PowerKW:       22,
				Rate: &PublishRate{
					Currency: "EUR",
					Prices:   []PublishPrice{{Type: "ENERGY", Value: 0.49}},
				},
			}},
		}},
	}, {
		ID:   "2",
		Name: "ACME · Antwerp",
		Lat:  51.21, Lon: 4.40,
		Stations: []PublishStation{{
			ID: "2",
			RefillPoints: []PublishRefillPoint{{
				ID:            "2",
				CurrentType:   "DC",
				ConnectorType: "IEC_62196_T2_COMBO",
				PowerKW:       150,
				Rate: &PublishRate{
					Currency: "EUR",
					Prices:   []PublishPrice{{Type: "ENERGY", Value: 0.69}, {Type: "TIME", Value: 6.0}},
				},
			}},
		}},
	}}
}

// TestWriteAFIRTableRoundTrip emits a table publication and parses it back with
// our own AFIR consumer, proving what we publish is what we can read.
func TestWriteAFIRTableRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	creator := Creator{Country: "BE", NationalIdentifier: "chargeprice"}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := WriteAFIRTable(&buf, sampleSites(), creator, now); err != nil {
		t.Fatalf("WriteAFIRTable: %v", err)
	}

	// Dump for the XSD-validation step (scripts/validate-datex.sh) when asked.
	if out := os.Getenv("DATEX_DUMP_DIR"); out != "" {
		_ = os.WriteFile(out+"/table.xml", buf.Bytes(), 0o644)
		var jb bytes.Buffer
		_ = WriteAFIRTableJSON(&jb, sampleSites(), creator, now)
		_ = os.WriteFile(out+"/table.json", jb.Bytes(), 0o644)
	}

	conns, tariffs, err := ParseAFIRStatic("cpo-x", buf.Bytes())
	if err != nil {
		t.Fatalf("ParseAFIRStatic on our own output: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("round-trip connectors = %d, want 2\nXML:\n%s", len(conns), buf.String())
	}
	byID := map[string]model_ConnLite{}
	for _, c := range conns {
		byID[c.EVSEUID] = model_ConnLite{c.PlugType, c.CurrentType, c.PowerKW, c.TariffID}
	}
	ac := byID["1"]
	if ac.plug != "IEC_62196_T2" || ac.current != "AC" || ac.power != 22 {
		t.Errorf("AC round-trip = %+v", ac)
	}
	dc := byID["2"]
	if dc.plug != "IEC_62196_T2_COMBO" || dc.current != "DC" || dc.power != 150 {
		t.Errorf("DC round-trip = %+v", dc)
	}
	// Pricing nested under energyProduct must round-trip.
	if len(tariffs) != 2 {
		t.Errorf("round-trip tariffs = %d, want 2", len(tariffs))
	}
}

type model_ConnLite struct {
	plug, current string
	power         float64
	tariffID      string
}

// TestPriceRoundedToTwoDigits guards the DATEX AmountOfMoney fractionDigits=2
// facet: a real-world price like 0.368 €/kWh must be emitted as 0.37, else the
// XML fails XSD validation (found in production).
func TestPriceRoundedToTwoDigits(t *testing.T) {
	enum, v, ok := priceTypeEnum("ENERGY", 0.368)
	if !ok || enum != "pricePerKWh" || v != 0.37 {
		t.Fatalf("ENERGY 0.368 -> (%q,%v,%v), want (pricePerKWh,0.37,true)", enum, v, ok)
	}

	sites := []PublishSite{{
		ID: "1", Lat: 50.85, Lon: 4.35,
		Stations: []PublishStation{{ID: "1", RefillPoints: []PublishRefillPoint{{
			ID: "1", CurrentType: "AC", ConnectorType: "IEC_62196_T2", PowerKW: 22,
			Rate: &PublishRate{Currency: "EUR", Prices: []PublishPrice{{Type: "ENERGY", Value: 0.368}}},
		}}}},
	}}
	var buf bytes.Buffer
	if err := WriteAFIRTable(&buf, sites, Creator{Country: "BE", NationalIdentifier: "APM"},
		time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("0.368")) {
		t.Errorf("emitted an over-precise price; XSD allows only 2 fraction digits\n%s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("<aegi:value>0.37</aegi:value>")) {
		t.Errorf("expected rounded 0.37 value\n%s", buf.String())
	}
}

// TestWriteAFIRTableJSONRoundTrip emits the JSON encoding and parses it back
// with the JSON consumer (ParseAFIRJSON) — the compliance check for the JSON
// encoding, which has no XSD.
func TestWriteAFIRTableJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	creator := Creator{Country: "be", NationalIdentifier: "chargeprice"}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := WriteAFIRTableJSON(&buf, sampleSites(), creator, now); err != nil {
		t.Fatalf("WriteAFIRTableJSON: %v", err)
	}
	doc, err := ParseAFIRJSON(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseAFIRJSON on our own output: %v", err)
	}
	if doc.Kind != "table" {
		t.Fatalf("kind = %q, want table\nJSON:\n%s", doc.Kind, buf.String())
	}
	if len(doc.Connectors) != 2 {
		t.Fatalf("connectors = %d, want 2\nJSON:\n%s", len(doc.Connectors), buf.String())
	}
	if len(doc.Tariffs) != 2 {
		t.Errorf("tariffs = %d, want 2", len(doc.Tariffs))
	}
	byID := map[string]model.Connector{}
	for _, c := range doc.Connectors {
		byID[c.EVSEUID] = c
	}
	if c := byID["1"]; c.PlugType != "IEC_62196_T2" || c.CurrentType != "AC" || c.PowerKW != 22 {
		t.Errorf("AC round-trip = %+v", c)
	}
	if c := byID["2"]; c.PlugType != "IEC_62196_T2_COMBO" || c.CurrentType != "DC" || c.PowerKW != 150 {
		t.Errorf("DC round-trip = %+v", c)
	}
}

func TestWriteAFIRStatusJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	creator := Creator{Country: "be", NationalIdentifier: "chargeprice"}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	statuses := []PublishStatus{
		{RefillPointID: "1", Status: "AVAILABLE"},
		{RefillPointID: "2", Status: "OUTOFORDER"},
	}
	if err := WriteAFIRStatusJSON(&buf, statuses, creator, now); err != nil {
		t.Fatalf("WriteAFIRStatusJSON: %v", err)
	}
	doc, err := ParseAFIRJSON(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseAFIRJSON: %v", err)
	}
	if doc.Kind != "status" {
		t.Fatalf("kind = %q, want status\nJSON:\n%s", doc.Kind, buf.String())
	}
	got := map[string]string{}
	for _, s := range doc.Statuses {
		got[s.EVSEUID] = s.Status
	}
	if got["1"] != "AVAILABLE" || got["2"] != "OUTOFORDER" {
		t.Errorf("status round-trip = %v", got)
	}
}

func TestWriteAFIRStatusRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	creator := Creator{Country: "BE", NationalIdentifier: "chargeprice"}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	statuses := []PublishStatus{
		{RefillPointID: "1", Status: "AVAILABLE"},
		{RefillPointID: "2", Status: "CHARGING"},
	}
	if err := WriteAFIRStatus(&buf, statuses, creator, now); err != nil {
		t.Fatalf("WriteAFIRStatus: %v", err)
	}
	if out := os.Getenv("DATEX_DUMP_DIR"); out != "" {
		_ = os.WriteFile(out+"/status.xml", buf.Bytes(), 0o644)
	}

	got, err := ParseAFIRStatus(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseAFIRStatus on our own output: %v", err)
	}
	if got["1"].Status != "AVAILABLE" {
		t.Errorf("RP1 status = %q, want AVAILABLE\nXML:\n%s", got["1"].Status, buf.String())
	}
	if got["2"].Status != "CHARGING" {
		t.Errorf("RP2 status = %q, want CHARGING", got["2"].Status)
	}
}
