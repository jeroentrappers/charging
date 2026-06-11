package irve

import (
	"strings"
	"testing"

	"github.com/appmire/charging/internal/model"
)

// sample has 4 features: a CCS DC point, a Type 2 AC point, a point whose power
// is expressed in watts ("50000"), and one with null geometry (to be skipped).
const sample = `{
  "type": "FeatureCollection",
  "metadata": {"ignored": true, "nested": {"a": [1,2,3]}},
  "features": [
    {
      "type": "Feature",
      "geometry": {"type": "Point", "coordinates": [2.3522, 48.8566]},
      "properties": {
        "nom_operateur": "Izivia",
        "nom_station": "Paris Centre",
        "id_pdc_itinerance": "FR123P1",
        "puissance_nominale": "150.0",
        "prise_type_2": "true",
        "prise_type_combo_ccs": "true",
        "adresse_station": "1 Rue de Rivoli",
        "consolidated_code_postal": "75001",
        "consolidated_commune": "Paris"
      }
    },
    {
      "type": "Feature",
      "geometry": {"type": "Point", "coordinates": [4.8357, 45.7640]},
      "properties": {
        "nom_operateur": "Freshmile",
        "nom_enseigne": "Lyon Park",
        "id_pdc_local": "LOCAL-42",
        "puissance_nominale": "22,0",
        "prise_type_2": "1",
        "consolidated_code_postal": "69001",
        "consolidated_commune": "Lyon"
      }
    },
    {
      "type": "Feature",
      "geometry": {"type": "Point", "coordinates": [5.3698, 43.2965]},
      "properties": {
        "nom_operateur": "TotalEnergies",
        "nom_station": "Marseille Vieux-Port",
        "id_station_itinerance": "FRSTAT99",
        "puissance_nominale": "50000",
        "prise_type_chademo": "true",
        "consolidated_code_postal": "13001",
        "consolidated_commune": "Marseille"
      }
    },
    {
      "type": "Feature",
      "geometry": null,
      "properties": {
        "nom_operateur": "Ghost",
        "id_pdc_itinerance": "FRNULL",
        "puissance_nominale": "11.0",
        "prise_type_2": "true"
      }
    }
  ]
}`

func TestParseStream(t *testing.T) {
	conns, tariffs, err := ParseStream("fr-irve", strings.NewReader(sample))
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}

	if len(tariffs) != 0 {
		t.Errorf("tariffs: got %d, want 0 (location-only source)", len(tariffs))
	}

	// Null-geometry feature must be skipped: 4 features -> 3 connectors.
	if len(conns) != 3 {
		t.Fatalf("connectors: got %d, want 3 (null geometry skipped)", len(conns))
	}

	// Feature 1: CCS takes priority over Type 2 -> DC combo, 150 kW.
	c := conns[0]
	if c.CPOID != "fr-irve" {
		t.Errorf("c0 CPOID: got %q", c.CPOID)
	}
	if c.EVSEUID != "FR123P1" {
		t.Errorf("c0 EVSEUID: got %q, want FR123P1", c.EVSEUID)
	}
	if c.ConnectorID != "1" {
		t.Errorf("c0 ConnectorID: got %q, want 1", c.ConnectorID)
	}
	if c.PlugType != "IEC_62196_T2_COMBO" {
		t.Errorf("c0 PlugType: got %q, want IEC_62196_T2_COMBO", c.PlugType)
	}
	if c.CurrentType != model.CurrentDC {
		t.Errorf("c0 CurrentType: got %q, want DC", c.CurrentType)
	}
	if c.PowerKW != 150.0 {
		t.Errorf("c0 PowerKW: got %v, want 150", c.PowerKW)
	}
	// lon FIRST in GeoJSON -> Lat must be the second coordinate.
	if c.Lat != 48.8566 || c.Lon != 2.3522 {
		t.Errorf("c0 coords: got lat=%v lon=%v, want lat=48.8566 lon=2.3522", c.Lat, c.Lon)
	}
	if c.Name != "Izivia · Paris Centre" {
		t.Errorf("c0 Name: got %q", c.Name)
	}
	if c.Address != "1 Rue de Rivoli" || c.PostalCode != "75001" || c.City != "Paris" {
		t.Errorf("c0 address fields: %q %q %q", c.Address, c.PostalCode, c.City)
	}
	if c.EVSEStatus != "" || c.TariffID != "" {
		t.Errorf("c0 status/tariff should be empty: %q %q", c.EVSEStatus, c.TariffID)
	}

	// Feature 2: Type 2 AC, 22 kW (comma decimal), EVSEUID from id_pdc_local,
	// name falls back to enseigne.
	c = conns[1]
	if c.EVSEUID != "LOCAL-42" {
		t.Errorf("c1 EVSEUID: got %q, want LOCAL-42", c.EVSEUID)
	}
	if c.PlugType != "IEC_62196_T2" {
		t.Errorf("c1 PlugType: got %q, want IEC_62196_T2", c.PlugType)
	}
	if c.CurrentType != model.CurrentAC {
		t.Errorf("c1 CurrentType: got %q, want AC", c.CurrentType)
	}
	if c.PowerKW != 22.0 {
		t.Errorf("c1 PowerKW: got %v, want 22 (comma decimal)", c.PowerKW)
	}
	if c.Name != "Freshmile · Lyon Park" {
		t.Errorf("c1 Name: got %q", c.Name)
	}

	// Feature 3: watts -> kW conversion (50000 -> 50), CHAdeMO DC, EVSEUID from
	// station id + row index.
	c = conns[2]
	if c.PowerKW != 50.0 {
		t.Errorf("c2 PowerKW: got %v, want 50 (50000 W -> 50 kW)", c.PowerKW)
	}
	if c.PlugType != "CHADEMO" {
		t.Errorf("c2 PlugType: got %q, want CHADEMO", c.PlugType)
	}
	if c.CurrentType != model.CurrentDC {
		t.Errorf("c2 CurrentType: got %q, want DC", c.CurrentType)
	}
	if c.EVSEUID != "FRSTAT99-3" {
		t.Errorf("c2 EVSEUID: got %q, want FRSTAT99-3", c.EVSEUID)
	}
}
