// Package export builds the open bulk dataset dumps: full normalized records
// (NDJSON), a GeoJSON point layer, an OCPI 2.1.1-shaped Locations + Tariffs
// pair (for roaming/interop, like ROAD publishes), and a small availability
// delta. Files are regenerated on a schedule and served as static files.
package export

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/store"
)

// ndjsonRecord is one normalized connector: the stored fields plus the parsed
// structured tariff nested under "tariff".
type ndjsonRecord struct {
	store.ExportCharger
	Tariff *model.Tariff `json:"tariff,omitempty"`
}

func parseTariff(raw json.RawMessage) *model.Tariff {
	if len(raw) == 0 {
		return nil
	}
	var t model.Tariff
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil
	}
	return &t
}

// WriteNDJSON writes one JSON record per line (streamable).
func WriteNDJSON(w io.Writer, rows []store.ExportCharger) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for i := range rows {
		if err := enc.Encode(ndjsonRecord{ExportCharger: rows[i], Tariff: parseTariff(rows[i].Components)}); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ---- GeoJSON ----

type geoFeature struct {
	Type     string         `json:"type"`
	Geometry geoGeometry    `json:"geometry"`
	Props    map[string]any `json:"properties"`
}

type geoGeometry struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"` // [lon, lat]
}

type geoCollection struct {
	Type     string       `json:"type"`
	Features []geoFeature `json:"features"`
}

// WriteGeoJSON writes a FeatureCollection of charger points for mapping tools.
func WriteGeoJSON(w io.Writer, rows []store.ExportCharger) error {
	fc := geoCollection{Type: "FeatureCollection", Features: make([]geoFeature, 0, len(rows))}
	for i := range rows {
		r := &rows[i]
		fc.Features = append(fc.Features, geoFeature{
			Type:     "Feature",
			Geometry: geoGeometry{Type: "Point", Coordinates: [2]float64{r.Lon, r.Lat}},
			Props: map[string]any{
				"id": r.ID, "cpo_id": r.CPOID, "name": r.Name,
				"power_kw": r.PowerKW, "plug_type": r.PlugType, "current_type": r.CurrentType,
				"comparable_price_eur": r.PriceEUR, "currency": r.Currency,
				"available_count": r.AvailableCount, "status": r.Status,
				"status_updated_at": r.StatusAt,
				"postal_code":       r.PostalCode, "city": r.City,
			},
		})
	}
	return writeJSON(w, fc)
}

// ---- OCPI 2.1.1-shaped dump ----

// BuildOCPI groups connectors into OCPI Locations (one Location per EVSE) and
// returns the deduplicated set of referenced Tariffs.
func BuildOCPI(rows []store.ExportCharger, now time.Time) ([]ocpi.Location, []ocpi.Tariff) {
	type loc struct {
		l        *ocpi.Location
		evse     *ocpi.EVSE
		statuses []string
	}
	locs := map[string]*loc{}
	var order []string
	tariffs := map[string]ocpi.Tariff{}

	for i := range rows {
		r := &rows[i]
		key := r.CPOID + "|" + r.EVSEUID

		var tariffIDs []string
		if t := parseTariff(r.Components); t != nil {
			id := t.OCPIID
			if id == "" {
				id = "h_" + t.Hash()[:16]
			}
			if _, seen := tariffs[id]; !seen {
				tariffs[id] = toOCPITariff(id, *t, r.StatusAt, now)
			}
			tariffIDs = []string{id}
		}

		conn := ocpi.Connector{
			ID:               r.ConnectorID,
			Standard:         r.PlugType,
			Format:           "SOCKET",
			PowerType:        powerType(r.CurrentType),
			MaxElectricPower: int(r.PowerKW * 1000),
			TariffIDs:        tariffIDs,
			LastUpdated:      now,
		}
		if len(tariffIDs) > 0 {
			conn.TariffID = tariffIDs[0]
		}

		l, ok := locs[key]
		if !ok {
			coord := ocpi.GeoLocation{Latitude: ftoa(r.Lat), Longitude: ftoa(r.Lon)}
			newLoc := &ocpi.Location{
				ID: key, Type: "ON_STREET", Name: r.Name,
				Address: r.Address, City: r.City, PostalCode: r.PostalCode, Country: countryAlpha3(r.Country),
				Coordinates: coord, LastUpdated: now,
			}
			if r.CPOID != "" {
				newLoc.Operator = &ocpi.BusinessDetails{Name: r.CPOID}
			}
			evse := ocpi.EVSE{UID: r.EVSEUID, EVSEID: r.EVSEUID, LastUpdated: now}
			newLoc.EVSEs = []ocpi.EVSE{evse}
			l = &loc{l: newLoc, evse: &newLoc.EVSEs[0]}
			locs[key] = l
			order = append(order, key)
		}
		l.evse.Connectors = append(l.evse.Connectors, conn)
		if r.Status != "" {
			l.statuses = append(l.statuses, r.Status)
		}
	}

	outLocs := make([]ocpi.Location, 0, len(order))
	for _, key := range order {
		l := locs[key]
		l.evse.Status = evseStatus(l.statuses)
		outLocs = append(outLocs, *l.l)
	}

	ids := make([]string, 0, len(tariffs))
	for id := range tariffs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	outTariffs := make([]ocpi.Tariff, 0, len(ids))
	for _, id := range ids {
		outTariffs = append(outTariffs, tariffs[id])
	}
	return outLocs, outTariffs
}

// WriteOCPILocations / WriteOCPITariffs wrap the data in an OCPI response
// envelope, matching what an OCPI Locations/Tariffs module returns.
func WriteOCPILocations(w io.Writer, locs []ocpi.Location, now time.Time) error {
	return writeJSON(w, ocpi.Envelope[ocpi.Location]{Data: locs, StatusCode: 1000, StatusMsg: "Success", Timestamp: now})
}

func WriteOCPITariffs(w io.Writer, tariffs []ocpi.Tariff, now time.Time) error {
	return writeJSON(w, ocpi.Envelope[ocpi.Tariff]{Data: tariffs, StatusCode: 1000, StatusMsg: "Success", Timestamp: now})
}

func toOCPITariff(id string, t model.Tariff, lastUpdated *time.Time, now time.Time) ocpi.Tariff {
	els := make([]ocpi.TariffElement, 0, len(t.Elements))
	for _, e := range t.Elements {
		pcs := make([]ocpi.PriceComponent, 0, len(e.PriceComponents))
		for _, p := range e.PriceComponents {
			pcs = append(pcs, ocpi.PriceComponent{Type: p.Type, Price: p.Price, StepSize: p.StepSize})
		}
		var r *ocpi.TariffRestrictions
		if e.Restrictions != nil {
			rr := ocpi.TariffRestrictions(*e.Restrictions) // identical fields
			r = &rr
		}
		els = append(els, ocpi.TariffElement{PriceComponents: pcs, Restrictions: r})
	}
	upd := now
	if lastUpdated != nil {
		upd = *lastUpdated
	}
	return ocpi.Tariff{ID: id, Currency: t.Currency, Elements: els, LastUpdated: upd}
}

// countryAlpha3 maps an ISO 3166-1 alpha-2 country code (as stored on the CPO)
// to the alpha-3 code OCPI Locations use. Unknown/empty -> "".
func countryAlpha3(c string) string {
	switch c {
	case "BE":
		return "BEL"
	case "NL":
		return "NLD"
	case "DE":
		return "DEU"
	case "FR":
		return "FRA"
	default:
		return ""
	}
}

func powerType(currentType string) string {
	if currentType == model.CurrentDC {
		return "DC"
	}
	return "AC_3_PHASE"
}

// evseStatus reduces the per-connector statuses to one OCPI EVSE status.
func evseStatus(statuses []string) string {
	if len(statuses) == 0 {
		return "UNKNOWN"
	}
	for _, s := range statuses {
		if s == "AVAILABLE" {
			return "AVAILABLE"
		}
	}
	return statuses[0]
}

func writeJSON(w io.Writer, v any) error {
	bw := bufio.NewWriter(w)
	if err := json.NewEncoder(bw).Encode(v); err != nil {
		return err
	}
	return bw.Flush()
}

// ftoa formats a coordinate the way OCPI expects: a decimal string.
func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}
