package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/store"
)

func tariffRaw(t *testing.T, energy float64) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: energy}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sampleRows(t *testing.T) []store.ExportCharger {
	a := tariffRaw(t, 0.40)
	b := tariffRaw(t, 0.55)
	return []store.ExportCharger{
		{ID: 1, CPOID: "road", EVSEUID: "E1", ConnectorID: "1", Lat: 51.05, Lon: 3.72, PowerKW: 22, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "CHARGING", Currency: "EUR", Components: a},
		{ID: 2, CPOID: "road", EVSEUID: "E1", ConnectorID: "2", Lat: 51.05, Lon: 3.72, PowerKW: 22, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "AVAILABLE", Currency: "EUR", Components: a}, // same EVSE + same tariff as row 1
		{ID: 3, CPOID: "road", EVSEUID: "E2", ConnectorID: "1", Lat: 51.20, Lon: 4.40, PowerKW: 150, CurrentType: "DC", PlugType: "IEC_62196_T2_COMBO", Status: "AVAILABLE", Currency: "EUR", Components: b},
		{ID: 4, CPOID: "road", EVSEUID: "E3", ConnectorID: "1", Lat: 50.85, Lon: 4.35, PowerKW: 11, CurrentType: "AC", PlugType: "IEC_62196_T2", Status: "UNKNOWN"}, // no tariff
	}
}

func TestWriteNDJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteNDJSON(&buf, sampleRows(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 NDJSON lines, got %d", len(lines))
	}
	var first ndjsonRecord
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if first.ID != 1 || first.Tariff == nil || len(first.Tariff.Elements) != 1 {
		t.Fatalf("first record missing nested tariff: %+v", first)
	}
	// The tariff-less charger must still serialize, with no tariff.
	var last ndjsonRecord
	if err := json.Unmarshal([]byte(lines[3]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Tariff != nil {
		t.Fatalf("charger 4 should have no tariff, got %+v", last.Tariff)
	}
}

func TestWriteGeoJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteGeoJSON(&buf, sampleRows(t)); err != nil {
		t.Fatal(err)
	}
	var fc geoCollection
	if err := json.Unmarshal(buf.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 4 {
		t.Fatalf("want FeatureCollection of 4, got %s/%d", fc.Type, len(fc.Features))
	}
	// GeoJSON coordinates are [lon, lat].
	if got := fc.Features[0].Geometry.Coordinates; got[0] != 3.72 || got[1] != 51.05 {
		t.Fatalf("coords should be [lon,lat], got %v", got)
	}
}

func TestBuildOCPI_GroupingAndTariffDedup(t *testing.T) {
	locs, tariffs := BuildOCPI(sampleRows(t), time.Unix(1700000000, 0).UTC())

	// E1 (2 connectors) + E2 + E3 = 3 locations, each with one EVSE.
	if len(locs) != 3 {
		t.Fatalf("want 3 locations, got %d", len(locs))
	}
	byKey := map[string]ocpi.Location{}
	for _, l := range locs {
		byKey[l.ID] = l
	}
	e1 := byKey["road|E1"]
	if len(e1.EVSEs) != 1 || len(e1.EVSEs[0].Connectors) != 2 {
		t.Fatalf("E1 should have 1 EVSE with 2 connectors, got %+v", e1.EVSEs)
	}
	// Mixed CHARGING + AVAILABLE -> EVSE reports AVAILABLE.
	if e1.EVSEs[0].Status != "AVAILABLE" {
		t.Fatalf("E1 EVSE status: want AVAILABLE, got %s", e1.EVSEs[0].Status)
	}

	// Two distinct tariffs (A shared by E1's connectors, B for E2); E3 has none.
	if len(tariffs) != 2 {
		t.Fatalf("want 2 deduped tariffs, got %d", len(tariffs))
	}
	// Both E1 connectors reference the same tariff id.
	c0, c1 := e1.EVSEs[0].Connectors[0], e1.EVSEs[0].Connectors[1]
	if len(c0.TariffIDs) != 1 || c0.TariffIDs[0] != c1.TariffIDs[0] {
		t.Fatalf("E1 connectors should share one tariff id, got %v / %v", c0.TariffIDs, c1.TariffIDs)
	}
	// E3 connector has no tariff.
	e3 := byKey["road|E3"]
	if len(e3.EVSEs[0].Connectors[0].TariffIDs) != 0 {
		t.Fatalf("E3 connector should have no tariff, got %v", e3.EVSEs[0].Connectors[0].TariffIDs)
	}
	// DC current maps to OCPI power_type DC.
	if byKey["road|E2"].EVSEs[0].Connectors[0].PowerType != "DC" {
		t.Fatal("E2 should be DC power_type")
	}
}
