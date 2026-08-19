// Package oicp reads the Swiss national charging feed published by the Federal
// Office of Energy (SFOE) behind ich-tanke-strom.ch.
//
// Switzerland is outside AFIR, so its NAP does not speak DATEX II or OCPI: the
// data is Hubject's OICP (Open InterCharge Protocol) serialised as JSON, in two
// open files (no key) — EVSEData (static) and EVSEStatus (live availability):
//
//	.../data/ch.bfe.ladestellen-elektromobilitaet.json     EVSEData
//	.../status/ch.bfe.ladestellen-elektromobilitaet.json    EVSEStatus
//
// The publication carries NO price: OICP's only price-adjacent field is
// PaymentOptions (how you may pay, not what it costs). Note that OICP's
// Accessibility value "Free publicly accessible" means freely *accessible*, not
// free of charge, so it must not be read as a zero tariff — this source is
// coverage plus live availability only.
package oicp

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

// ---- OICP JSON ----

type dataFile struct {
	EVSEData []operatorEVSEData `json:"EVSEData"`
}

type operatorEVSEData struct {
	OperatorID   string       `json:"OperatorID"`
	OperatorName string       `json:"OperatorName"`
	Records      []evseRecord `json:"EVSEDataRecord"`
}

type evseRecord struct {
	EvseID              string             `json:"EvseID"`
	Accessibility       string             `json:"Accessibility"`
	Address             evseAddress        `json:"Address"`
	ChargingFacilities  []chargingFacility `json:"ChargingFacilities"`
	ChargingStationName localisedNames     `json:"ChargingStationNames"`
	GeoCoordinates      geoCoordinates     `json:"GeoCoordinates"`
	Plugs               []string           `json:"Plugs"`
}

type evseAddress struct {
	Street     string     `json:"Street"`
	City       string     `json:"City"`
	PostalCode flexString `json:"PostalCode"` // usually a string, occasionally a number
}

type localisedName struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

// localisedNames is a list of localised names that tolerates the two other
// shapes this feed uses for the same field: a bare object when there is only one
// name (40 records), and null when there is none.
type localisedNames []localisedName

func (n *localisedNames) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "null" || trimmed == "" {
		*n = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []localisedName
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		*n = list
		return nil
	}
	var one localisedName
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*n = localisedNames{one}
	return nil
}

// chargingFacility carries the electrical rating. `power` arrives as either a
// JSON number or a decimal string in this feed, so it is decoded leniently.
type chargingFacility struct {
	Amperage  flexNumber `json:"Amperage"`
	Voltage   flexNumber `json:"Voltage"`
	Power     flexNumber `json:"power"`
	PowerType string     `json:"powertype"` // AC_1_PHASE | AC_3_PHASE | DC
}

// geoCoordinates holds the "lat lon" Google-format pair this feed publishes.
type geoCoordinates struct {
	Google string `json:"Google"`
}

type statusFile struct {
	EVSEStatuses []operatorEVSEStatus `json:"EVSEStatuses"`
}

type operatorEVSEStatus struct {
	Records []evseStatusRecord `json:"EVSEStatusRecord"`
}

type evseStatusRecord struct {
	EvseID string `json:"EvseID"`
	Status string `json:"EVSEStatus"`
}

// flexString accepts a JSON string or number (two records publish the postcode
// as a number) and renders both as text.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "null" || t == "" {
		*f = ""
		return nil
	}
	*f = flexString(strings.Trim(t, `"`))
	return nil
}

// flexNumber accepts a JSON number or a numeric string.
type flexNumber float64

func (f *flexNumber) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil // tolerate junk rather than failing a whole national feed
	}
	*f = flexNumber(v)
	return nil
}

// ---- mapping tables ----

// plugStandard maps OICP's human-readable plug names to canonical OCPI
// connector standards.
var plugStandard = map[string]string{
	"type 2 outlet":                      "IEC_62196_T2",
	"type 2 connector (cable attached)":  "IEC_62196_T2",
	"ccs combo 2 plug (cable attached)":  "IEC_62196_T2_COMBO",
	"ccs combo 1 plug (cable attached)":  "IEC_62196_T1_COMBO",
	"type 1 connector (cable attached)":  "IEC_62196_T1",
	"chademo":                            "CHADEMO",
	"tesla connector":                    "TESLA_S",
	"type j swiss standard":              "DOMESTIC_J",
	"type g british standard":            "DOMESTIC_G",
	"type f schuko":                      "DOMESTIC_F",
	"type e french standard":             "DOMESTIC_E",
	"small paddle inductive":             "",
	"large paddle inductive":             "",
	"avcon connector":                    "",
	"nema 5-20":                          "",
	"type 3 outlet":                      "IEC_62196_T3A",
	"iec 60309 single phase":             "IEC_60309_2_single_16",
	"iec 60309 three phase":              "IEC_60309_2_three_16",
	"battery exchange":                   "",
	"unspecified":                        "",
	"ccs combo 2 plug (cable attached) ": "IEC_62196_T2_COMBO",
}

// statusVocab maps an OICP EVSEStatusType to our EVSE status vocabulary.
func statusVocab(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "available":
		return "AVAILABLE"
	case "occupied", "reserved":
		return "CHARGING"
	case "outofservice":
		return "OUTOFORDER"
	default: // unknown, evsenotfound, ""
		return "UNKNOWN"
	}
}

// publiclyAccessible reports whether an OICP Accessibility value describes a
// charger the public may actually use. "Restricted access" points are private
// (fleet, staff, tenants) and test stations are not real, so both are dropped
// rather than ingested as public coverage.
func publiclyAccessible(a string) bool {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "restricted access", "test station":
		return false
	default:
		return true
	}
}

// ---- fetching ----

// Fetch retrieves the OICP data + status pair and returns connectors with live
// availability. pairURL is "<data-url>|<status-url>". The tariff map is always
// empty: this publication carries no price.
//
// The signature matches the other location-only readers so the ingest engine can
// treat it uniformly.
func Fetch(ctx context.Context, cpoID, pairURL, token string) ([]model.Connector, map[string]model.Tariff, error) {
	dataURL, statusURL, _ := strings.Cut(pairURL, "|")
	dataJSON, err := get(ctx, strings.TrimSpace(dataURL), token)
	if err != nil {
		return nil, nil, fmt.Errorf("oicp data: %w", err)
	}
	conns, err := ParseData(cpoID, dataJSON)
	if err != nil {
		return nil, nil, err
	}
	if s := strings.TrimSpace(statusURL); s != "" {
		statusJSON, err := get(ctx, s, token)
		if err != nil {
			return nil, nil, fmt.Errorf("oicp status: %w", err)
		}
		statuses, err := ParseStatus(statusJSON)
		if err != nil {
			return nil, nil, err
		}
		for i := range conns {
			if st, ok := statuses[conns[i].EVSEUID]; ok {
				conns[i].EVSEStatus = st
			}
		}
	}
	return conns, map[string]model.Tariff{}, nil
}

func get(ctx context.Context, u, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// ---- parsing ----

// ParseData maps an OICP EVSEData document to canonical connectors. One record
// is one EVSE with one connector, keyed by EvseID. Records without usable
// coordinates, and those not publicly accessible, are skipped.
func ParseData(cpoID string, data []byte) ([]model.Connector, error) {
	var f dataFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode oicp data: %w", err)
	}
	var conns []model.Connector
	for _, op := range f.EVSEData {
		for _, r := range op.Records {
			if !publiclyAccessible(r.Accessibility) {
				continue
			}
			lat, lon, ok := coords(r.GeoCoordinates.Google)
			if !ok {
				continue
			}
			fac := bestFacility(r.ChargingFacilities)
			plug := mapPlug(r.Plugs)
			conns = append(conns, model.Connector{
				CPOID:       cpoID,
				EVSEUID:     r.EvseID,
				ConnectorID: "1",
				Lat:         lat,
				Lon:         lon,
				PowerKW:     powerKW(fac),
				PlugType:    plug,
				CurrentType: currentType(fac.PowerType, plug),
				Name:        name(r, op.OperatorName),
				Address:     address(r.Address),
				PostalCode:  string(r.Address.PostalCode),
				City:        r.Address.City,
			})
		}
	}
	return conns, nil
}

// ParseStatus maps an OICP EVSEStatus document to our status vocabulary, keyed
// by EvseID.
func ParseStatus(data []byte) (map[string]string, error) {
	var f statusFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode oicp status: %w", err)
	}
	out := make(map[string]string)
	for _, op := range f.EVSEStatuses {
		for _, r := range op.Records {
			if r.EvseID == "" {
				continue
			}
			out[r.EvseID] = statusVocab(r.Status)
		}
	}
	return out, nil
}

// coords parses OICP's "lat lon" Google-format coordinate string.
func coords(g string) (lat, lon float64, ok bool) {
	latS, lonS, found := strings.Cut(strings.TrimSpace(g), " ")
	if !found {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(latS), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(lonS), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if lat == 0 && lon == 0 || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	return lat, lon, true
}

// bestFacility picks the highest-power charging facility of a record: a few
// records list several (and two list 144), and the strongest rating is the one
// that describes what the point can deliver.
func bestFacility(fs []chargingFacility) chargingFacility {
	var best chargingFacility
	for _, f := range fs {
		if powerKW(f) > powerKW(best) {
			best = f
		}
	}
	return best
}

// powerKW returns the facility's power in kW, falling back to
// voltage × amperage (× 3 for three-phase) when `power` is absent.
func powerKW(f chargingFacility) float64 {
	if f.Power > 0 {
		return round1(float64(f.Power))
	}
	if f.Voltage > 0 && f.Amperage > 0 {
		w := float64(f.Voltage) * float64(f.Amperage)
		if strings.EqualFold(f.PowerType, "AC_3_PHASE") {
			w *= 3
		}
		return round1(w / 1000)
	}
	return 0
}

// mapPlug picks one primary plug, preferring the DC fast option when a point
// lists several (a CCS point is the meaningful choice even beside a Type 2).
func mapPlug(plugs []string) string {
	best := ""
	for _, p := range plugs {
		std, known := plugStandard[strings.ToLower(strings.TrimSpace(p))]
		if !known {
			std = model.NormalizePlug(p)
		}
		if std == "" {
			continue
		}
		if best == "" || plugRank(std) > plugRank(best) {
			best = std
		}
	}
	return best
}

// plugRank orders plug standards by how much they define the charger, so a post
// listing several gets the one a European driver is most likely to use: CCS2 is
// the continent's DC default, CHAdeMO is legacy, and a Tesla connector in Europe
// is usually CCS2 anyway. AC sockets rank below any DC standard, and industrial
// or wireless couplers below those.
func plugRank(std string) int {
	switch std {
	case "IEC_62196_T2_COMBO":
		return 6
	case "IEC_62196_T1_COMBO":
		return 5
	case "CHADEMO":
		return 4
	case "TESLA_S", "TESLA_R":
		return 3
	case "IEC_62196_T2", "IEC_62196_T1", "IEC_62196_T3C", "IEC_62196_T3A":
		return 2
	default:
		return 1
	}
}

// currentType decides AC vs DC from the facility's power type, falling back to
// the plug standard when the feed omits it (525 records carry no powertype).
func currentType(powerType, plug string) string {
	if strings.EqualFold(powerType, "DC") {
		return model.CurrentDC
	}
	if powerType != "" {
		return model.CurrentAC
	}
	switch plug {
	case "IEC_62196_T2_COMBO", "IEC_62196_T1_COMBO", "CHADEMO":
		return model.CurrentDC
	}
	return model.CurrentAC
}

// name prefers "Operator · Station" so cards stay recognisable: every Swiss
// record shares one cpo_id, so the operator would otherwise be lost.
func name(r evseRecord, operator string) string {
	site := ""
	for _, n := range r.ChargingStationName {
		if v := strings.TrimSpace(n.Value); v != "" {
			site = v
			break
		}
	}
	if operator != "" && site != "" {
		return operator + " · " + site
	}
	if site != "" {
		return site
	}
	return operator
}

func address(a evseAddress) string {
	parts := []string{}
	if a.Street != "" {
		parts = append(parts, a.Street+",")
	}
	if a.PostalCode != "" {
		parts = append(parts, string(a.PostalCode))
	}
	if a.City != "" {
		parts = append(parts, a.City)
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
