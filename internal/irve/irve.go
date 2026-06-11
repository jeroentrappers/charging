// Package irve reads France's consolidated IRVE GeoJSON dataset (the national
// EV charging point dataset, "Infrastructures de Recharge pour Véhicules
// Électriques") into the canonical model.
//
// The dataset is a single GeoJSON FeatureCollection with ~230k features and is
// roughly 585 MB on the wire, so it MUST be stream-decoded one feature at a
// time rather than loaded whole. Each feature is one point de charge and maps
// to exactly one connector.
//
// This publication is LOCATION-ONLY: it has no structured price (only a
// free-text `tarification` field, which we ignore) and no live status. Parsed
// connectors therefore carry no tariff and unknown availability.
package irve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/appmire/charging/internal/model"
)

// ---- GeoJSON feature structs ----

type geometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"` // [lon, lat]
}

type properties struct {
	NomOperateur           string `json:"nom_operateur"`
	NomEnseigne            string `json:"nom_enseigne"`
	NomStation             string `json:"nom_station"`
	IDStationItinerance    string `json:"id_station_itinerance"`
	IDPdcItinerance        string `json:"id_pdc_itinerance"`
	IDPdcLocal             string `json:"id_pdc_local"`
	PuissanceNominale      string `json:"puissance_nominale"`
	PriseTypeEF            string `json:"prise_type_ef"`
	PriseType2             string `json:"prise_type_2"`
	PriseTypeComboCCS      string `json:"prise_type_combo_ccs"`
	PriseTypeChademo       string `json:"prise_type_chademo"`
	PriseTypeAutre         string `json:"prise_type_autre"`
	AdresseStation         string `json:"adresse_station"`
	ConsolidatedCodePostal string `json:"consolidated_code_postal"`
	ConsolidatedCommune    string `json:"consolidated_commune"`
	DateMaj                string `json:"date_maj"`
}

type feature struct {
	Geometry   *geometry  `json:"geometry"`
	Properties properties `json:"properties"`
}

// Fetch retrieves and stream-decodes the consolidated IRVE GeoJSON. The token,
// if supplied, is sent as a Bearer credential; the public dataset needs none.
// The tariff map is always empty (location-only source).
func Fetch(ctx context.Context, cpoID, url, token string) ([]model.Connector, map[string]model.Tariff, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/geo+json, application/json")
	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("irve http %d", resp.StatusCode)
	}
	return ParseStream(cpoID, io.LimitReader(resp.Body, 1<<30))
}

// ParseStream decodes a GeoJSON FeatureCollection one feature at a time from r,
// emitting one connector per feature. It is the unit-testable core of Fetch.
// Features with null or invalid geometry are skipped. The tariff map is empty.
func ParseStream(cpoID string, r io.Reader) ([]model.Connector, map[string]model.Tariff, error) {
	tariffs := map[string]model.Tariff{}
	dec := json.NewDecoder(r)

	// Walk the top-level object tokens until we reach the value of "features",
	// which must be an array. This avoids buffering the whole document.
	if err := seekFeaturesArray(dec); err != nil {
		return nil, nil, err
	}

	var conns []model.Connector
	row := 0
	for dec.More() {
		var f feature
		if err := dec.Decode(&f); err != nil {
			return nil, nil, fmt.Errorf("decode feature: %w", err)
		}
		row++

		lat, lon, ok := coords(f.Geometry)
		if !ok {
			continue
		}

		plug, current := plugAndCurrent(f.Properties)
		conns = append(conns, model.Connector{
			CPOID:       cpoID,
			EVSEUID:     evseUID(f.Properties, row),
			ConnectorID: "1",
			Lat:         lat,
			Lon:         lon,
			PowerKW:     powerKW(f.Properties.PuissanceNominale),
			PlugType:    plug,
			CurrentType: current,
			Name:        name(f.Properties),
			Address:     f.Properties.AdresseStation,
			PostalCode:  f.Properties.ConsolidatedCodePostal,
			City:        f.Properties.ConsolidatedCommune,
			EVSEStatus:  "",
			TariffID:    "",
		})
	}

	// Consume the closing array token; we don't need anything after it.
	if _, err := dec.Token(); err != nil && err != io.EOF {
		return nil, nil, fmt.Errorf("read closing token: %w", err)
	}

	return conns, tariffs, nil
}

// seekFeaturesArray advances the decoder to just inside the "features" array
// (i.e. positioned at the first array element), reading and discarding any
// other top-level keys.
func seekFeaturesArray(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read opening token: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("irve: expected JSON object, got %v", tok)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		key, _ := keyTok.(string)
		if key == "features" {
			arr, err := dec.Token()
			if err != nil {
				return fmt.Errorf("read features token: %w", err)
			}
			if d, ok := arr.(json.Delim); !ok || d != '[' {
				return fmt.Errorf("irve: features is not an array")
			}
			return nil
		}
		// Not "features": skip its value (handles nested objects/arrays).
		if err := skipValue(dec); err != nil {
			return err
		}
	}
	return fmt.Errorf("irve: no features array found")
}

// skipValue reads and discards one complete JSON value from the decoder.
func skipValue(dec *json.Decoder) error {
	var v json.RawMessage
	return dec.Decode(&v)
}

// coords extracts lat/lon from a Point geometry whose coordinates are
// [lon, lat]. It returns ok=false for null geometry or 0/invalid coordinates.
func coords(g *geometry) (lat, lon float64, ok bool) {
	if g == nil || len(g.Coordinates) < 2 {
		return 0, 0, false
	}
	lon = g.Coordinates[0]
	lat = g.Coordinates[1]
	if lat == 0 && lon == 0 {
		return 0, 0, false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	return lat, lon, true
}

func evseUID(p properties, row int) string {
	if p.IDPdcItinerance != "" {
		return p.IDPdcItinerance
	}
	if p.IDPdcLocal != "" {
		return p.IDPdcLocal
	}
	return p.IDStationItinerance + "-" + strconv.Itoa(row)
}

// name prefers "Operator · Station"; falls back through enseigne and either
// part alone so cards stay recognisable (all features share one cpo_id).
func name(p properties) string {
	site := p.NomStation
	if site == "" {
		site = p.NomEnseigne
	}
	if p.NomOperateur != "" && site != "" {
		return p.NomOperateur + " · " + site
	}
	if site != "" {
		return site
	}
	return p.NomOperateur
}

// powerKW parses puissance_nominale, accepting comma or dot decimals. Values
// over 1000 are assumed to be watts and converted to kW. Rounded to 1 decimal.
func powerKW(s string) float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if v > 1000 {
		v = v / 1000
	}
	return round1(v)
}

// plugAndCurrent picks one primary plug by priority. A CCS point is the
// meaningful fast option even when Type 2 is also present.
func plugAndCurrent(p properties) (plug, current string) {
	switch {
	case truthy(p.PriseTypeComboCCS):
		return "IEC_62196_T2_COMBO", model.CurrentDC
	case truthy(p.PriseTypeChademo):
		return "CHADEMO", model.CurrentDC
	case truthy(p.PriseType2):
		return "IEC_62196_T2", model.CurrentAC
	case truthy(p.PriseTypeEF):
		return "DOMESTIC_F", model.CurrentAC
	default:
		return "", model.CurrentAC
	}
}

// truthy treats "true"/"1" (case-insensitive) as set; everything else unset.
func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
