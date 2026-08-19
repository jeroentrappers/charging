// Package irve reads France's consolidated IRVE GeoJSON dataset (the national
// EV charging point dataset, "Infrastructures de Recharge pour Véhicules
// Électriques") into the canonical model.
//
// The dataset is a single GeoJSON FeatureCollection with ~230k features and is
// roughly 585 MB on the wire, so it MUST be stream-decoded one feature at a
// time rather than loaded whole. Each feature is one point de charge and maps
// to exactly one connector.
//
// The static publication has NO structured price (only a free-text
// `tarification` field, which we ignore) and no status. Availability comes from
// a second national file — the consolidated *dynamic* base, one CSV row per
// point de charge keyed by the same `id_pdc_itinerance` — read by ParseDynamic
// below. So France is coverage + availability, still no price.
package irve

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/appmire/charging/internal/model"
)

// DynamicMaxAge bounds how old a row in the dynamic file may be and still be
// treated as a statement about availability. The consolidated dynamic file is
// rebuilt daily but carries every point any publisher ever reported, so a large
// share of its rows are months stale — reporting those as "libre" would invent
// availability. Rows older than this are ignored, leaving the point's status
// unknown.
const DynamicMaxAge = 36 * time.Hour

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

// ---- Dynamic availability (consolidated national file) ----
//
// France's NAP publishes a second consolidated base alongside the static one:
// one CSV row per point de charge with its operational state, keyed by
// `id_pdc_itinerance` — the same identifier the static publication uses, so the
// two join directly. The file carries no price (France still publishes ad-hoc
// price only as free text in the static schema).
//
// Two properties of the file shape the reader: rows repeat (several publishers
// report the same point, so the freshest row wins) and many rows are long stale
// (see DynamicMaxAge).

// FetchDynamic retrieves the consolidated dynamic CSV and returns one status per
// point de charge, in our EVSE status vocabulary, keyed by id_pdc_itinerance.
func FetchDynamic(ctx context.Context, url, token string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Deliberately permissive: the NAP's /resources/<id>/download endpoint answers
	// 500 to a narrow "Accept: text/csv" (it 302s to the proxy below, which is
	// what we point at anyway).
	req.Header.Set("Accept", "*/*")
	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("irve dynamic http %d", resp.StatusCode)
	}
	return ParseDynamic(io.LimitReader(resp.Body, 1<<30), time.Now())
}

// ParseDynamic reads the dynamic CSV, keeping for each point the freshest row
// that is not older than DynamicMaxAge relative to now.
func ParseDynamic(r io.Reader, now time.Time) (map[string]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate trailing/extra columns being added upstream
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("irve dynamic: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	idIx, okID := col["id_pdc_itinerance"]
	etatIx, okEtat := col["etat_pdc"]
	occIx, okOcc := col["occupation_pdc"]
	tsIx, okTS := col["horodatage"]
	if !okID || !okEtat || !okOcc || !okTS {
		return nil, fmt.Errorf("irve dynamic: unexpected columns %v", head)
	}

	type row struct {
		status string
		at     time.Time
	}
	best := map[string]row{}
	cutoff := now.Add(-DynamicMaxAge)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("irve dynamic: %w", err)
		}
		if len(rec) <= max4(idIx, etatIx, occIx, tsIx) {
			continue
		}
		id := strings.TrimSpace(rec[idIx])
		if id == "" {
			continue
		}
		at, ok := parseHorodatage(rec[tsIx])
		if !ok || at.Before(cutoff) {
			continue // no usable timestamp, or too old to claim anything
		}
		if prev, seen := best[id]; seen && !at.After(prev.at) {
			continue // a fresher row for this point already won
		}
		best[id] = row{status: dynamicStatus(rec[etatIx], rec[occIx]), at: at}
	}

	out := make(map[string]string, len(best))
	for id, r := range best {
		out[id] = r.status
	}
	return out, nil
}

// dynamicStatus maps the French state pair onto our EVSE status vocabulary.
// Operational state wins: a point out of service is unusable regardless of what
// the occupancy column claims.
func dynamicStatus(etat, occupation string) string {
	switch strings.ToLower(strings.TrimSpace(etat)) {
	case "hors_service":
		return "OUTOFORDER"
	}
	switch strings.ToLower(strings.TrimSpace(occupation)) {
	case "libre":
		return "AVAILABLE"
	case "occupe", "reserve":
		return "CHARGING"
	}
	return "UNKNOWN"
}

// parseHorodatage accepts the timestamp spellings seen in the file: RFC 3339,
// and the "2026-07-13 13:42:40+00:00" / fractional-second variants the
// consolidation emits.
func parseHorodatage(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02T15:04:05.999999Z07:00",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func max4(a, b, c, d int) int {
	m := a
	for _, v := range []int{b, c, d} {
		if v > m {
			m = v
		}
	}
	return m
}
