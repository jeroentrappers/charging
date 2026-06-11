package bnetza

import (
	"strings"
	"testing"
)

// Latin-1 byte values for accented chars used in the German headers/data.
const (
	auml  = "\xe4" // ä
	uuml  = "\xfc" // ü
	szlig = "\xdf" // ß
)

// sampleCSV builds an ISO-8859-1 encoded fixture: a two-line preamble, the real
// header row (containing "Breitengrad" and "Betreiber"), and two data rows —
// one AC Typ 2 connector and one DC station with CCS (two plugs). Decimals use
// a comma; accented chars use raw Latin-1 bytes.
func sampleCSV() []byte {
	preamble := "Lades" + auml + "ulenregister der Bundesnetzagentur\r\n" +
		"Stand: 01.06.2026\r\n"
	// Latin-1: ä is 0xE4, ß is 0xDF. Build the header with raw bytes.
	header := "Betreiber;Anzeigename (Karte);Standortbezeichnung;Stra" + szlig + "e;Hausnummer;Adresszusatz;Postleitzahl;Ort;Breitengrad;L" + auml + "ngengrad;Ladeeinrichtungs-ID;Nennleistung Ladeeinrichtung [kW];Steckertypen1;Nennleistung Stecker1;Steckertypen2;Nennleistung Stecker2\r\n"
	row1 := "Stadtwerke Musterstadt;Marktplatz;Standort A;Hauptstra" + szlig + "e;12;;80331;M" + uuml + "nchen;48,442398;11,0;DE*ABC*E001;22,0;Typ 2;22,0;;\r\n"
	row2 := "FastCharge GmbH;;Autobahn Rastst" + auml + "tte;Am Kreuz;1;Etage 1;10115;Berlin;52,520008;13,404954;DE*XYZ*E777;150,0;CCS;150,0;CHAdeMO;50,0\r\n"
	s := preamble + header + row1 + row2

	// s is a Go (UTF-8) string but the accented chars were injected as raw
	// single bytes, so converting to []byte yields valid Latin-1 for those
	// while the plain-ASCII chars are identical in both encodings.
	return []byte(s)
}

func TestParse(t *testing.T) {
	conns, tariffs, err := Parse("bnetza", sampleCSV())
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(tariffs) != 0 {
		t.Errorf("expected empty tariff map, got %d entries", len(tariffs))
	}

	// row1: 1 connector (Typ 2). row2: 2 connectors (CCS + CHAdeMO). = 3.
	if len(conns) != 3 {
		t.Fatalf("expected 3 connectors, got %d: %+v", len(conns), conns)
	}

	// --- row1: AC Typ 2 ---
	c0 := conns[0]
	if c0.PlugType != "IEC_62196_T2" {
		t.Errorf("c0 plug: got %q want IEC_62196_T2", c0.PlugType)
	}
	if c0.CurrentType != "AC" {
		t.Errorf("c0 current: got %q want AC", c0.CurrentType)
	}
	if c0.PowerKW != 22.0 {
		t.Errorf("c0 power: got %v want 22.0", c0.PowerKW)
	}
	if c0.Lat != 48.442398 {
		t.Errorf("c0 lat: got %v want 48.442398", c0.Lat)
	}
	if c0.Lon != 11.0 {
		t.Errorf("c0 lon: got %v want 11.0", c0.Lon)
	}
	if c0.EVSEUID != "DE*ABC*E001" {
		t.Errorf("c0 evseuid: got %q", c0.EVSEUID)
	}
	if c0.ConnectorID != "1" {
		t.Errorf("c0 connectorID: got %q want 1", c0.ConnectorID)
	}
	if c0.EVSEStatus != "" || c0.TariffID != "" {
		t.Errorf("c0 expected empty status/tariff, got %q/%q", c0.EVSEStatus, c0.TariffID)
	}
	// Name should be "Operator · Site" with the operator and the map display name.
	if !strings.Contains(c0.Name, "Stadtwerke Musterstadt") || !strings.Contains(c0.Name, "Marktplatz") {
		t.Errorf("c0 name: got %q", c0.Name)
	}
	// City decoded from Latin-1 -> UTF-8 should be "München".
	if c0.City != "München" {
		t.Errorf("c0 city: got %q want München", c0.City)
	}
	// Address: "Hauptstraße 12".
	if c0.Address != "Hauptstraße 12" {
		t.Errorf("c0 address: got %q want Hauptstraße 12", c0.Address)
	}

	// --- row2: DC CCS + CHAdeMO ---
	c1, c2 := conns[1], conns[2]
	if c1.PlugType != "IEC_62196_T2_COMBO" || c1.CurrentType != "DC" {
		t.Errorf("c1: got %q/%q want IEC_62196_T2_COMBO/DC", c1.PlugType, c1.CurrentType)
	}
	if c1.PowerKW != 150.0 {
		t.Errorf("c1 power: got %v want 150.0", c1.PowerKW)
	}
	if c1.ConnectorID != "1" {
		t.Errorf("c1 connectorID: got %q want 1", c1.ConnectorID)
	}
	if c2.PlugType != "CHADEMO" || c2.CurrentType != "DC" {
		t.Errorf("c2: got %q/%q want CHADEMO/DC", c2.PlugType, c2.CurrentType)
	}
	if c2.PowerKW != 50.0 {
		t.Errorf("c2 power: got %v want 50.0", c2.PowerKW)
	}
	if c2.ConnectorID != "2" {
		t.Errorf("c2 connectorID: got %q want 2", c2.ConnectorID)
	}
	if c1.EVSEUID != "DE*XYZ*E777" || c2.EVSEUID != "DE*XYZ*E777" {
		t.Errorf("row2 evseuid mismatch: %q %q", c1.EVSEUID, c2.EVSEUID)
	}
	if c1.Lat != 52.520008 || c1.Lon != 13.404954 {
		t.Errorf("c1 coords: got %v,%v", c1.Lat, c1.Lon)
	}
}
