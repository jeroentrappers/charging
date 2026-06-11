// Package bnetza reads Germany's Bundesnetzagentur "Ladesäulenregister" CSV
// (the public charging-point register behind the Ladesäulenkarte) into the
// canonical model.
//
// The register is LOCATION-ONLY: it carries coordinates, operator, address,
// plug types and rated power, but NO ad-hoc price and NO live availability.
// Connectors parsed here therefore have an empty tariff map, EVSEStatus="" and
// TariffID="".
//
// The dated CSV download URL is not stable, so Fetch scrapes the landing page
// for the current link. The CSV itself is ISO-8859-1 encoded, semicolon
// delimited, uses a comma as decimal separator, and is preceded by a preamble:
// the real header row is the one containing "Breitengrad" and "Betreiber".
package bnetza

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/appmire/charging/internal/model"
)

const maxBody = 256 << 20 // 256 MB

// csvLinkRE matches the dated CSV download link on the landing page.
var csvLinkRE = regexp.MustCompile(`https?://data\.bundesnetzagentur\.de/[^"']+\.csv`)

// Fetch retrieves and parses the Ladesäulenregister CSV.
//
// The url argument is the landing-page URL. Fetch GETs it, scrapes the current
// dated CSV link, then GETs that CSV. If url already ends in ".csv" it is
// fetched directly. token is unused (the source needs no auth) and kept only to
// match the shared source signature.
func Fetch(ctx context.Context, cpoID, url, token string) ([]model.Connector, map[string]model.Tariff, error) {
	_ = token // location-only source, no auth
	client := &http.Client{Timeout: 120 * time.Second}

	csvURL := url
	if !strings.HasSuffix(strings.ToLower(url), ".csv") {
		page, err := get(ctx, client, url)
		if err != nil {
			return nil, nil, fmt.Errorf("bnetza landing page: %w", err)
		}
		m := csvLinkRE.Find(page)
		if m == nil {
			return nil, nil, fmt.Errorf("bnetza: no CSV link found on %s", url)
		}
		csvURL = string(m)
	}

	data, err := get(ctx, client, csvURL)
	if err != nil {
		return nil, nil, fmt.Errorf("bnetza csv: %w", err)
	}
	return Parse(cpoID, data)
}

func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return body, nil
}

// Parse maps the Ladesäulenregister CSV to canonical connectors. The CSV is
// ISO-8859-1 encoded; it is decoded to UTF-8, the preamble is trimmed, then the
// remaining records are read with a semicolon delimiter. No tariffs exist in
// this source, so the tariff map is always empty.
func Parse(cpoID string, data []byte) ([]model.Connector, map[string]model.Tariff, error) {
	utf8Data, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return nil, nil, fmt.Errorf("decode latin-1: %w", err)
	}

	trimmed, err := trimPreamble(utf8Data)
	if err != nil {
		return nil, nil, err
	}

	r := csv.NewReader(bytes.NewReader(trimmed))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	idx := indexHeader(header)

	tariffs := map[string]model.Tariff{}
	var conns []model.Connector

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read record: %w", err)
		}

		lat, ok1 := parseFloat(field(rec, idx, "Breitengrad"))
		lon, ok2 := parseFloat(field(rec, idx, "Längengrad"))
		if !ok1 || !ok2 || lat == 0 || lon == 0 {
			continue
		}

		operator := field(rec, idx, "Betreiber")
		display := field(rec, idx, "Anzeigename (Karte)")
		if display == "" {
			display = field(rec, idx, "Standortbezeichnung")
		}
		stationID := field(rec, idx, "Ladeeinrichtungs-ID")
		einrichtungKW, _ := parseFloat(field(rec, idx, "Nennleistung Ladeeinrichtung [kW]"))

		addr := buildAddress(rec, idx)
		postal := field(rec, idx, "Postleitzahl")
		city := field(rec, idx, "Ort")
		nm := name(operator, display)

		for n := 1; n <= 6; n++ {
			plugRaw := field(rec, idx, "Steckertypen"+strconv.Itoa(n))
			if strings.TrimSpace(plugRaw) == "" {
				continue
			}
			plug, current := mapPlug(plugRaw)

			kw, ok := parseFloat(field(rec, idx, "Nennleistung Stecker"+strconv.Itoa(n)))
			if !ok {
				kw = einrichtungKW
			}

			conns = append(conns, model.Connector{
				CPOID:       cpoID,
				EVSEUID:     stationID,
				ConnectorID: strconv.Itoa(n),
				Lat:         lat,
				Lon:         lon,
				PowerKW:     round1(kw),
				PlugType:    plug,
				CurrentType: current,
				Name:        nm,
				Address:     addr,
				PostalCode:  postal,
				City:        city,
				EVSEStatus:  "", // location-only source
				TariffID:    "", // no tariffs
			})
		}
	}
	return conns, tariffs, nil
}

// trimPreamble drops everything before the real header line, which is the first
// line containing "Breitengrad". CRLF line endings are tolerated.
func trimPreamble(data []byte) ([]byte, error) {
	const marker = "Breitengrad"
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		if bytes.Contains(line, []byte(marker)) {
			return bytes.Join(lines[i:], []byte("\n")), nil
		}
	}
	return nil, fmt.Errorf("bnetza: header row containing %q not found", marker)
}

// indexHeader maps trimmed header names to their column index.
func indexHeader(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	return idx
}

func field(rec []string, idx map[string]int, name string) string {
	i, ok := idx[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

func buildAddress(rec []string, idx map[string]int) string {
	parts := []string{}
	if s := field(rec, idx, "Straße"); s != "" {
		street := s
		if h := field(rec, idx, "Hausnummer"); h != "" {
			street += " " + h
		}
		parts = append(parts, street)
	} else if h := field(rec, idx, "Hausnummer"); h != "" {
		parts = append(parts, h)
	}
	if z := field(rec, idx, "Adresszusatz"); z != "" {
		parts = append(parts, z)
	}
	return strings.Join(parts, " ")
}

// parseFloat parses a German-formatted decimal (comma separator). It returns
// false when the value is empty or unparseable.
func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// mapPlug maps a German plug-type string to a canonical OCPI plug type and
// current type.
func mapPlug(raw string) (plug, current string) {
	s := strings.TrimSpace(raw)
	l := strings.ToLower(s)
	hasDC := strings.Contains(l, "dc")

	switch {
	case strings.Contains(l, "ccs") || strings.Contains(l, "combo"):
		return "IEC_62196_T2_COMBO", model.CurrentDC
	case strings.Contains(l, "chademo"):
		return "CHADEMO", model.CurrentDC
	case hasDC && strings.Contains(l, "typ 2"):
		return "IEC_62196_T2", model.CurrentDC
	case strings.Contains(l, "typ 2"):
		return "IEC_62196_T2", model.CurrentAC
	case strings.Contains(l, "typ 1"):
		return "IEC_62196_T1", model.CurrentAC
	case strings.Contains(l, "schuko") || strings.Contains(l, "steckdose") || strings.Contains(l, "haushalt"):
		return "DOMESTIC_F", model.CurrentAC
	case strings.Contains(l, "tesla"):
		return "IEC_62196_T2_COMBO", model.CurrentDC
	default:
		if hasDC {
			return s, model.CurrentDC
		}
		return s, model.CurrentAC
	}
}

// name prefers "Operator · Site" so cards are recognisable (all sites share one
// cpo_id, so the operator would otherwise be lost).
func name(operator, site string) string {
	if operator != "" && site != "" {
		return operator + " · " + site
	}
	if site != "" {
		return site
	}
	return operator
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
