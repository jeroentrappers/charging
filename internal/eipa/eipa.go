// Package eipa reads Poland's national alternative-fuels register, EIPA
// (Ewidencja Infrastruktury Paliw Alternatywnych), run by the Office of
// Technical Inspection (UDT).
//
// EIPA is not DATEX II or OCPI: it publishes six proprietary JSON files, five
// static and one dynamic, each behind a per-account URL whose last path segment
// is the account's secret token:
//
//	<base>/pool/<token>        sites            (lat/lon, address, operator)
//	<base>/station/<token>     stations         (pool_id, type E/G/H)
//	<base>/point/<token>       charging points  (station_id, connectors, power)
//	<base>/dictionary/<token>  code lists       (connector interfaces, modes)
//	<base>/operator/<token>    operators        (names)
//	<base>/dynamic/<token>     status + prices  (per point)
//
// Download limits are enforced per account: 10/hour for the static files and
// 240/hour for the dynamic one, so the caller is expected to fetch the statics
// rarely and overlay the dynamic file often (see the ingest feed).
//
// PRICES ARE IN PLN. They are all ad-hoc by construction — the register carries
// the prices "obowiązujących dla klientów niezwiązanych umową z operatorem"
// (applicable to customers with no contract with the operator).
package eipa

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

// Currency every EIPA price is quoted in.
const Currency = "PLN"

// StatusMaxAge bounds how old a point's status timestamp may be and still be
// treated as a statement about availability. The file is regenerated every few
// minutes but each point carries the operator's own last-update time, and a
// sizeable tail has not been touched in over a year — reporting those as free
// would invent availability.
const StatusMaxAge = 36 * time.Hour

// ---- wire types ----

type envelope[T any] struct {
	Data      []T    `json:"data"`
	Generated string `json:"generated"`
}

type pool struct {
	ID          int     `json:"id"`
	OperatorID  int     `json:"operator_id"`
	Charging    bool    `json:"charging"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Street      string  `json:"street"`
	HouseNumber string  `json:"house_number"`
	PostalCode  string  `json:"postal_code"`
	City        string  `json:"city"`
}

type station struct {
	ID        int     `json:"id"`
	PoolID    int     `json:"pool_id"`
	Type      string  `json:"type"` // E = electric, G = gas, H = hydrogen
	Suspended bool    `json:"suspended"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type point struct {
	ID         int          `json:"id"`
	StationID  int          `json:"station_id"`
	Code       string       `json:"code"`
	Connectors []connector  `json:"connectors"`
	Solutions  []chargeMode `json:"charging_solutions"`
}

type connector struct {
	Interfaces    []int   `json:"interfaces"`
	Power         float64 `json:"power"` // kW
	CableAttached bool    `json:"cable_attached"`
}

type chargeMode struct {
	Mode  int     `json:"mode"`
	Power float64 `json:"power"`
}

type operator struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

type dictionary struct {
	ConnectorInterface []dictEntry `json:"connector_interface"`
	ChargingMode       []dictEntry `json:"charging_mode"`
}

type dictEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type dynamicPoint struct {
	PointID int            `json:"point_id"`
	Code    string         `json:"code"`
	Status  *dynamicStatus `json:"status"`
	Prices  []dynamicPrice `json:"prices"`
}

// dynamicStatus carries two distinct flags (per the EIPA docs):
// Availability is the device's operational readiness ("czy urządzenie jest
// dostępne operacyjnie"), Status is whether it can charge a vehicle right now
// ("możliwość naładowania pojazdu") — i.e. free vs occupied.
type dynamicStatus struct {
	Availability *int   `json:"availability"`
	Status       *int   `json:"status"`
	TS           string `json:"ts"`
}

type dynamicPrice struct {
	Literal string `json:"literal"`
	Price   string `json:"price"` // decimal string, PLN
	Unit    string `json:"unit"`  // kWh | min | m3 | kg
	TS      string `json:"ts"`
}

// ---- snapshot ----

// Static is the parsed static half of the register, joined and indexed.
type Static struct {
	Pools     map[int]pool
	Stations  map[int]station
	Points    []point
	Operators map[int]operator
	Plugs     map[int]string // connector interface id -> OCPI standard
	Modes     map[int]string // charging mode id -> dictionary name
}

// Dynamic is the parsed dynamic half, keyed by point id.
type Dynamic map[int]dynamicPoint

// Client fetches one EIPA account's files.
type Client struct {
	Base  string // e.g. https://eipa.udt.gov.pl/reader/export-data
	Token string // the account's secret path segment
	HTTP  *http.Client
}

func New(base, token string) *Client {
	return &Client{
		Base:  strings.TrimRight(base, "/"),
		Token: token,
		HTTP:  &http.Client{Timeout: 300 * time.Second},
	}
}

// Static fetches and joins the five static files. Counts against the 10/hour
// static limit, so callers should cache the result.
func (c *Client) Static(ctx context.Context) (*Static, error) {
	var pools envelope[pool]
	if err := c.get(ctx, "pool", &pools); err != nil {
		return nil, err
	}
	var stations envelope[station]
	if err := c.get(ctx, "station", &stations); err != nil {
		return nil, err
	}
	var points envelope[point]
	if err := c.get(ctx, "point", &points); err != nil {
		return nil, err
	}
	var operators envelope[operator]
	if err := c.get(ctx, "operator", &operators); err != nil {
		return nil, err
	}
	var dict dictionary
	if err := c.get(ctx, "dictionary", &dict); err != nil {
		return nil, err
	}

	s := &Static{
		Pools:     make(map[int]pool, len(pools.Data)),
		Stations:  make(map[int]station, len(stations.Data)),
		Points:    points.Data,
		Operators: make(map[int]operator, len(operators.Data)),
		Plugs:     make(map[int]string, len(dict.ConnectorInterface)),
		Modes:     make(map[int]string, len(dict.ChargingMode)),
	}
	for _, p := range pools.Data {
		s.Pools[p.ID] = p
	}
	for _, st := range stations.Data {
		s.Stations[st.ID] = st
	}
	for _, o := range operators.Data {
		s.Operators[o.ID] = o
	}
	// Resolve plug ids through the published dictionary rather than a hardcoded
	// id table, so a renumbering upstream cannot silently mislabel plugs.
	for _, e := range dict.ConnectorInterface {
		s.Plugs[e.ID] = plugStandard(e.Name)
	}
	for _, e := range dict.ChargingMode {
		s.Modes[e.ID] = e.Name
	}
	return s, nil
}

// Dynamic fetches the dynamic file (status + prices), keyed by point id.
func (c *Client) Dynamic(ctx context.Context) (Dynamic, error) {
	var d envelope[dynamicPoint]
	if err := c.get(ctx, "dynamic", &d); err != nil {
		return nil, err
	}
	out := make(Dynamic, len(d.Data))
	for _, p := range d.Data {
		out[p.PointID] = p
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, file string, into any) error {
	u := c.Base + "/" + file + "/" + c.Token
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("eipa %s: %w", file, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// A 429/403 here most likely means the hourly download limit was hit.
		return fmt.Errorf("eipa %s: http %d", file, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return fmt.Errorf("eipa %s: %w", file, err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("eipa %s: decode: %w", file, err)
	}
	return nil
}

// ---- mapping ----

// Build joins the static and dynamic halves into canonical connectors plus the
// ad-hoc tariff map. Only electric stations are emitted; gas and hydrogen
// points, suspended stations and non-charging sites are skipped.
func Build(cpoID string, s *Static, dyn Dynamic, now time.Time) ([]model.Connector, map[string]model.Tariff) {
	tariffs := map[string]model.Tariff{}
	var conns []model.Connector

	for _, p := range s.Points {
		st, ok := s.Stations[p.StationID]
		if !ok || st.Type != "E" || st.Suspended {
			continue
		}
		pl, ok := s.Pools[st.PoolID]
		if !ok || !pl.Charging {
			continue
		}
		lat, lon := st.Latitude, st.Longitude
		if lat == 0 && lon == 0 {
			lat, lon = pl.Latitude, pl.Longitude
		}
		if lat == 0 && lon == 0 {
			continue
		}

		d := dyn[p.ID]
		status := statusVocab(d.Status, now)
		tariff, hasTariff := buildTariff(p.Code, d.Prices)

		for i, con := range p.Connectors {
			power := con.Power
			if power == 0 {
				power = solutionPower(p.Solutions)
			}
			plug := plugOf(s, con.Interfaces)
			c := model.Connector{
				CPOID:       cpoID,
				EVSEUID:     p.Code,
				ConnectorID: strconv.Itoa(i + 1),
				Lat:         lat,
				Lon:         lon,
				PowerKW:     round1(power),
				PlugType:    plug,
				CurrentType: currentType(plug, s.Modes, p.Solutions),
				Name:        name(s, pl),
				Address:     address(pl),
				PostalCode:  pl.PostalCode,
				City:        pl.City,
				EVSEStatus:  status,
			}
			if hasTariff {
				c.TariffID = p.Code
				tariffs[p.Code] = tariff
			}
			conns = append(conns, c)
		}
	}
	return conns, tariffs
}

// statusVocab maps the two EIPA flags onto our EVSE status vocabulary, ignoring
// statements older than StatusMaxAge (they stay UNKNOWN rather than claiming a
// point is free). Operational readiness wins: a point that is out of service is
// unusable regardless of whether it is occupied.
func statusVocab(s *dynamicStatus, now time.Time) string {
	if s == nil || s.Availability == nil || s.Status == nil {
		return "UNKNOWN"
	}
	if ts, ok := parseTS(s.TS); !ok || now.Sub(ts) > StatusMaxAge {
		return "UNKNOWN"
	}
	if *s.Availability != 1 {
		return "OUTOFORDER"
	}
	if *s.Status == 1 {
		return "AVAILABLE"
	}
	return "CHARGING"
}

// buildTariff turns a point's price list into one ad-hoc tariff in PLN.
//
// Every EIPA price is an ad-hoc price by definition, but a point may list
// several variants (day/night, or "via app" beside "Ad hoc"). A literal naming
// ad-hoc wins; otherwise the DEAREST variant is used, so we never advertise a
// charger as cheaper than a driver might actually pay. Gas and hydrogen units
// (m3, kg) are ignored.
func buildTariff(id string, prices []dynamicPrice) (model.Tariff, bool) {
	energy, ok1 := pickPrice(prices, "kwh")
	perMin, ok2 := pickPrice(prices, "min")
	if !ok1 && !ok2 {
		return model.Tariff{}, false
	}
	var comps []model.PriceComponent
	if ok1 {
		comps = append(comps, model.PriceComponent{Type: "ENERGY", Price: energy})
	}
	if ok2 {
		// EIPA quotes per minute; our TIME component is per hour.
		comps = append(comps, model.PriceComponent{Type: "TIME", Price: round2(perMin * 60)})
	}
	return model.Tariff{
		OCPIID:   id,
		Currency: Currency,
		Elements: []model.TariffElement{{PriceComponents: comps}},
	}, true
}

// Sanity ceilings, in PLN. Because pickPrice takes the dearest variant when no
// label identifies the ad-hoc one, a single mis-published row would otherwise
// become a point's headline price. The real distribution is tight (1st
// percentile 1.75, median 2.40, 99th 3.25 PLN/kWh), and the handful of rows
// above these bounds are unit mix-ups — the worst offender publishes
// "0.50 zł/min (naliczany po 60 min)" as 163.00 with unit kWh. Values above the
// ceiling are dropped rather than shown to a driver as real.
const (
	maxPricePerKWh = 10.0 // ≈ €2.30/kWh
	maxPricePerMin = 5.0  // ≈ €70/hour
)

// pickPrice returns the price to use for one unit: an explicitly ad-hoc labelled
// variant if present, else the highest. Zero, unparseable and implausible values
// are ignored so a point with no published price cannot look free, and a
// mis-published one cannot look absurd.
func pickPrice(prices []dynamicPrice, unit string) (float64, bool) {
	ceiling := maxPricePerKWh
	if unit == "min" {
		ceiling = maxPricePerMin
	}
	var best float64
	var found bool
	for _, p := range prices {
		if !strings.EqualFold(strings.TrimSpace(p.Unit), unit) {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(p.Price), 64)
		if err != nil || v <= 0 || v > ceiling {
			continue
		}
		if isAdHocLiteral(p.Literal) {
			return v, true
		}
		if v > best {
			best, found = v, true
		}
	}
	return best, found
}

// isAdHocLiteral spots the drive-up variant among a point's price labels. The
// labels are free text written by each operator, in Polish.
func isAdHocLiteral(literal string) bool {
	l := strings.ToLower(literal)
	return strings.Contains(l, "ad hoc") || strings.Contains(l, "ad-hoc") || strings.Contains(l, "adhoc")
}

// plugStandard maps an EIPA connector-interface dictionary name to a canonical
// OCPI connector standard. Names are like "IEC-62196-T2-F-NOCABLE" (socket) or
// "IEC-62196-T2-COMBO"; the cable/socket suffix does not change the standard.
func plugStandard(name string) string {
	n := strings.ToUpper(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(n, "IEC-62196-T2-COMBO"):
		return "IEC_62196_T2_COMBO"
	case strings.HasPrefix(n, "IEC-62196-T1-COMBO"):
		return "IEC_62196_T1_COMBO"
	case strings.HasPrefix(n, "IEC-62196-T2"):
		return "IEC_62196_T2"
	case strings.HasPrefix(n, "IEC-62196-T1"):
		return "IEC_62196_T1"
	case strings.HasPrefix(n, "IEC-62196-T3C"):
		return "IEC_62196_T3C"
	case strings.HasPrefix(n, "IEC-62196-T3A"):
		return "IEC_62196_T3A"
	case n == "CHADEMO":
		return "CHADEMO"
	case n == "TESLA-SPECIFIC":
		return "TESLA_S"
	case strings.HasPrefix(n, "DOMESTIC-"):
		return "DOMESTIC_" + strings.TrimPrefix(n, "DOMESTIC-")
	case strings.HasPrefix(n, "IEC-309-2"):
		return "" // industrial sockets: not a car plug standard we rank on
	}
	return model.NormalizePlug(name)
}

// plugOf picks one primary plug for a connector, preferring the DC fast option
// when several interfaces are listed on the same physical connector.
func plugOf(s *Static, interfaces []int) string {
	best := ""
	for _, id := range interfaces {
		std := s.Plugs[id]
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

// currentType decides AC vs DC from the plug, falling back to the point's
// charging mode ("Mode4-DC" is the DC mode in the EIPA dictionary).
func currentType(plug string, modes map[int]string, solutions []chargeMode) string {
	switch plug {
	case "IEC_62196_T2_COMBO", "IEC_62196_T1_COMBO", "CHADEMO":
		return model.CurrentDC
	}
	for _, s := range solutions {
		if strings.Contains(strings.ToUpper(modes[s.Mode]), "DC") {
			return model.CurrentDC
		}
	}
	return model.CurrentAC
}

// solutionPower is the fallback power when a connector states none: the
// strongest charging solution declared for the point.
func solutionPower(solutions []chargeMode) float64 {
	var best float64
	for _, s := range solutions {
		if s.Power > best {
			best = s.Power
		}
	}
	return best
}

// name prefers "Operator · Site" so cards stay recognisable: every Polish
// record shares one cpo_id, so the operator would otherwise be lost.
func name(s *Static, pl pool) string {
	op := ""
	if o, ok := s.Operators[pl.OperatorID]; ok {
		op = o.ShortName
		if op == "" {
			op = o.Name
		}
	}
	if op != "" && pl.Name != "" {
		return op + " · " + pl.Name
	}
	if pl.Name != "" {
		return pl.Name
	}
	return op
}

func address(pl pool) string {
	street := strings.TrimSpace(pl.Street)
	if pl.HouseNumber != "" {
		street = strings.TrimSpace(street + " " + pl.HouseNumber)
	}
	parts := []string{}
	if street != "" {
		parts = append(parts, street+",")
	}
	if pl.PostalCode != "" {
		parts = append(parts, pl.PostalCode)
	}
	if pl.City != "" {
		parts = append(parts, pl.City)
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

// parseTS reads the ISO 8601 timestamps EIPA emits (offset form, e.g.
// "2026-08-19T10:30:03+02:00").
func parseTS(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
