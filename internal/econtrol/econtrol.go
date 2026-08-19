// Package econtrol reads Austria's national charging register, the E-Control
// Ladestellenverzeichnis (ladestellen.at), through its Public API.
//
// The API is free but keyed, and the key is bound to a domain: every request
// must carry BOTH an "Apikey" header and a "Referer" whose host matches the one
// registered with the key — a mismatched Referer answers 401 with a prose page,
// not JSON.
//
// There is no bulk export. The register is walked three levels deep:
//
//	/countries/{c}/operators                                  → operator ids
//	/countries/{c}/operators/{op}/stations                    → sites
//	/countries/{c}/operators/{op}/stations/{station}/points    → EVSEs
//
// Only the deepest level carries what we need: power, connector type, live
// status, and a structured ad-hoc price in euro cents (per kWh, per minute, a
// start fee, and a blocking fee with a grace threshold). Requests are therefore
// numerous, so the crawl is bounded and the operator/station tree is meant to be
// cached by the caller between passes.
package econtrol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/appmire/charging/internal/model"
)

// DefaultReferer is the domain our API key is registered against. E-Control
// matches the request's Referer host against the key, so this must stay in step
// with the registration.
const DefaultReferer = "https://charging.appmire.be"

// DefaultConcurrency bounds in-flight requests. E-Control publishes no rate
// limit, so this is deliberately modest: a full pass is thousands of small
// requests and we would rather be slow than be cut off.
const DefaultConcurrency = 4

// maxFailureRatio is how much of a level may fail before the whole pass is
// treated as failed. One pass is ~15,800 requests, so the odd timeout is
// expected and must not discard the other 15,799 results — but a systemic
// outage or a revoked key has to surface as a failed run rather than a silently
// shrinking register (which would also trip the retire guard).
const maxFailureRatio = 0.05

// ---- wire types ----

type operatorJSON struct {
	OperatorID string `json:"operatorId"`
	Status     string `json:"status"`
}

type stationJSON struct {
	StationID     string  `json:"stationId"`
	StationStatus string  `json:"stationStatus"` // ACTIVE | ...
	Label         string  `json:"label"`
	Street        string  `json:"street"`
	PostCode      string  `json:"postCode"`
	City          string  `json:"city"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Owner         string  `json:"owner"`
}

type pointJSON struct {
	EvseID     string   `json:"evseId"`
	CapacityKw float64  `json:"capacityKw"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
	Status     string   `json:"status"` // AVAILABLE | CHARGING | UNKNOWN | ...

	FreeOfCharge bool `json:"freeOfCharge"`
	// Prices are in euro CENTS. priceCentMin is charged per minute of charging;
	// blockingFee* is the idle fee that starts after blockingFeeFromMinute.
	PriceCentKwh          float64 `json:"priceCentKwh"`
	PriceCentMin          float64 `json:"priceCentMin"`
	StartFeeCent          float64 `json:"startFeeCent"`
	BlockingFeeCentMin    float64 `json:"blockingFeeCentMin"`
	BlockingFeeFromMinute int     `json:"blockingFeeFromMinute"`

	ConnectorType   []string `json:"connectorType"`
	ElectricityType []string `json:"electricityType"` // AC_1_PHASE | AC_3_PHASE | DC
}

// Station is one site plus the operator it belongs to, as needed to fetch points.
type Station struct {
	OperatorID string
	Station    stationJSON
}

// ---- client ----

type Client struct {
	Base        string
	APIKey      string
	Referer     string
	Concurrency int
	HTTP        *http.Client
}

// New builds a client. The credential may be given as "<apikey>" or
// "<apikey>|<referer>" when the key is registered against another domain.
func New(base, credential string) *Client {
	key, referer, _ := strings.Cut(credential, "|")
	if strings.TrimSpace(referer) == "" {
		referer = DefaultReferer
	}
	return &Client{
		Base:        strings.TrimRight(base, "/"),
		APIKey:      strings.TrimSpace(key),
		Referer:     strings.TrimSpace(referer),
		Concurrency: DefaultConcurrency,
		// Short per-request timeout: the API answers in ~0.2s, and a pass makes
		// ~15,800 requests, so a hung connection should be abandoned quickly and
		// absorbed by the failure tolerance rather than stalling the whole crawl.
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
}

// Stations walks operators → stations and returns every active site.
func (c *Client) Stations(ctx context.Context, country string) ([]Station, error) {
	var operators []operatorJSON
	if err := c.get(ctx, "/countries/"+country+"/operators", &operators); err != nil {
		return nil, fmt.Errorf("operators: %w", err)
	}

	var mu sync.Mutex
	out := make([]Station, 0, len(operators))
	var failed int
	var firstErr error

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(c.concurrency())
	for _, op := range operators {
		op := op
		g.Go(func() error {
			var stations []stationJSON
			path := "/countries/" + country + "/operators/" + url.PathEscape(op.OperatorID) + "/stations"
			err := c.get(gctx, path, &stations)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("stations of operator %s: %w", op.OperatorID, err)
				}
				return nil // tolerated here; judged against the ratio below
			}
			for _, s := range stations {
				if s.StationStatus != "" && !strings.EqualFold(s.StationStatus, "ACTIVE") {
					continue // decommissioned / planned sites are not chargers to show
				}
				out = append(out, Station{OperatorID: op.OperatorID, Station: s})
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if tooManyFailed(failed, len(operators)) {
		return nil, fmt.Errorf("%d of %d operators failed, first: %w", failed, len(operators), firstErr)
	}
	return out, nil
}

// tooManyFailed reports whether a level failed so widely that its results should
// not be trusted as a complete view.
func tooManyFailed(failed, total int) bool {
	return failed > 0 && (total == 0 || float64(failed)/float64(total) > maxFailureRatio)
}

// Points fetches the EVSEs of every station and maps them to canonical
// connectors plus their ad-hoc tariffs.
func (c *Client) Points(ctx context.Context, cpoID, country string, stations []Station) ([]model.Connector, map[string]model.Tariff, error) {
	var mu sync.Mutex
	conns := make([]model.Connector, 0, len(stations)*2)
	tariffs := map[string]model.Tariff{}

	var failed int
	var firstErr error

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(c.concurrency())
	for _, st := range stations {
		st := st
		g.Go(func() error {
			var points []pointJSON
			path := "/countries/" + country + "/operators/" + url.PathEscape(st.OperatorID) +
				"/stations/" + url.PathEscape(st.Station.StationID) + "/points"
			err := c.get(gctx, path, &points)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("points of station %s: %w", st.Station.StationID, err)
				}
				return nil // tolerated here; judged against the ratio below
			}
			for _, p := range points {
				conn, tariff, hasTariff := mapPoint(cpoID, st.Station, p)
				if conn == nil {
					continue
				}
				if hasTariff {
					tariffs[conn.TariffID] = tariff
				}
				conns = append(conns, *conn)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	if tooManyFailed(failed, len(stations)) {
		return nil, nil, fmt.Errorf("%d of %d stations failed, first: %w", failed, len(stations), firstErr)
	}
	return conns, tariffs, nil
}

func (c *Client) concurrency() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	return DefaultConcurrency
}

func (c *Client) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Apikey", c.APIKey)
	// Required: the key is bound to this domain and a mismatch answers 401.
	req.Header.Set("Referer", c.Referer)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// ---- mapping ----

// mapPoint turns one EVSE into a connector (and its tariff, when priced).
// Returns nil when the point cannot be placed on a map.
func mapPoint(cpoID string, st stationJSON, p pointJSON) (*model.Connector, model.Tariff, bool) {
	lat, lon := st.Latitude, st.Longitude
	if p.Latitude != nil && p.Longitude != nil && (*p.Latitude != 0 || *p.Longitude != 0) {
		lat, lon = *p.Latitude, *p.Longitude
	}
	if lat == 0 && lon == 0 {
		return nil, model.Tariff{}, false
	}
	plug := mapPlug(p.ConnectorType)
	conn := &model.Connector{
		CPOID:       cpoID,
		EVSEUID:     p.EvseID,
		ConnectorID: "1",
		Lat:         lat,
		Lon:         lon,
		PowerKW:     round1(p.CapacityKw),
		PlugType:    plug,
		CurrentType: currentType(p.ElectricityType, plug),
		Name:        name(st),
		Address:     address(st),
		PostalCode:  st.PostCode,
		City:        st.City,
		EVSEStatus:  statusVocab(p.Status),
	}
	tariff, ok := buildTariff(p)
	if ok {
		conn.TariffID = p.EvseID
	}
	return conn, tariff, ok
}

// maxPricePerKWh is a sanity ceiling in euros. Measured over the whole register
// the 99th percentile is €0.95/kWh, so a handful of values several euros per kWh
// are unit mix-ups (a cent amount entered as euros, or a per-minute fee typed
// into the per-kWh field). They are dropped rather than shown to a driver.
//
// Per-minute prices are deliberately NOT capped: charging billed by the minute is
// normal in Austria, and where an operator publishes both a per-kWh and a
// per-minute amount we keep both, as the schema says both apply. That can
// overstate a session's cost when an operator duplicated one field into the
// other, which makes the charger look expensive — the safe direction of error,
// since it never sends a driver to a charger that turns out to be dearer than we
// promised. Worth reporting upstream all the same.
const maxPricePerKWh = 3.0

// buildTariff converts the point's cent amounts into a euro tariff.
//
// A point that states no amounts at all stays unpriced rather than free. When
// freeOfCharge contradicts published amounts (a handful of operators set both),
// the amounts win: quoting a charger as free when its operator also published a
// price is the more damaging error.
func buildTariff(p pointJSON) (model.Tariff, bool) {
	var comps []model.PriceComponent
	if kwh := cents(p.PriceCentKwh); p.PriceCentKwh > 0 && kwh <= maxPricePerKWh {
		comps = append(comps, model.PriceComponent{Type: "ENERGY", Price: kwh})
	}
	if p.PriceCentMin > 0 {
		// Per minute upstream; our TIME component is per hour.
		comps = append(comps, model.PriceComponent{Type: "TIME", Price: round2(cents(p.PriceCentMin) * 60)})
	}
	if p.StartFeeCent > 0 {
		comps = append(comps, model.PriceComponent{Type: "FLAT", Price: cents(p.StartFeeCent)})
	}
	if p.BlockingFeeCentMin > 0 {
		// An idle fee: charged per minute of occupation once the grace period is
		// over. PARKING_TIME keeps it out of the comparable charging session, the
		// same treatment other sources' blocking fees get.
		comps = append(comps, model.PriceComponent{
			Type:         "PARKING_TIME",
			Price:        round2(cents(p.BlockingFeeCentMin) * 60),
			AfterMinutes: p.BlockingFeeFromMinute,
		})
	}
	if len(comps) == 0 {
		if p.FreeOfCharge {
			// An explicit statement, unlike the ambiguous "freely accessible" flags
			// other registers use: charging here costs nothing.
			return model.Tariff{
				OCPIID:   p.EvseID,
				Currency: "EUR",
				Elements: []model.TariffElement{{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0}}}},
			}, true
		}
		return model.Tariff{}, false
	}
	return model.Tariff{
		OCPIID:   p.EvseID,
		Currency: "EUR",
		Elements: []model.TariffElement{{PriceComponents: comps}},
	}, true
}

// statusVocab maps E-Control's point status to our EVSE status vocabulary.
func statusVocab(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "AVAILABLE", "FREE":
		return "AVAILABLE"
	case "CHARGING", "OCCUPIED", "RESERVED", "BLOCKED":
		return "CHARGING"
	case "OUTOFORDER", "OUT_OF_ORDER", "FAULTED", "INOPERATIVE", "UNAVAILABLE":
		return "OUTOFORDER"
	default: // UNKNOWN, "", anything new
		return "UNKNOWN"
	}
}

// plugStandard maps E-Control's connector vocabulary to canonical OCPI
// standards. The register mixes OCPI-style names with its own codes, where a
// leading C means a cable and S a socket of the same standard.
var plugStandard = map[string]string{
	"CTYPE2":             "IEC_62196_T2",
	"STYPE2":             "IEC_62196_T2",
	"IEC_62196_T2":       "IEC_62196_T2",
	"CTYPE1":             "IEC_62196_T1",
	"STYPE1":             "IEC_62196_T1",
	"IEC_62196_T1":       "IEC_62196_T1",
	"CCCS2":              "IEC_62196_T2_COMBO",
	"IEC_62196_T2_COMBO": "IEC_62196_T2_COMBO",
	"CCCS1":              "IEC_62196_T1_COMBO",
	"IEC_62196_T1_COMBO": "IEC_62196_T1_COMBO",
	"CG105":              "CHADEMO", // JEVS G105 = CHAdeMO
	"CHADEMO":            "CHADEMO",
	"CTESLA":             "TESLA_S",
	"STYPE3":             "IEC_62196_T3C",
	"SCEE-7-8":           "DOMESTIC_F",
	// Industrial sockets, wireless pads, pantographs and the catch-alls are not
	// car plug standards we filter or rank on, so they map to nothing.
	"S309-1P-16A": "", "S309-1P-32A": "", "S309-3P-16A": "", "S309-3P-32A": "",
	"WINDUCTIVE": "", "WRESONANT": "", "PAN": "", "UNKNOWN": "",
	"OTHER1PHMAX16A": "", "OTHER1PHOVER16A": "", "OTHER3PH": "",
}

// mapPlug picks ONE primary plug, preferring the DC fast option.
//
// A single point may list twenty connector types — some operators tick every box
// in the register's form — so emitting one connector per listed type would
// invent hardware. One row per EVSE with its most defining plug is closer to the
// truth.
func mapPlug(types []string) string {
	best := ""
	for _, t := range types {
		key := strings.ToUpper(strings.TrimSpace(t))
		std, known := plugStandard[key]
		if !known {
			std = model.NormalizePlug(key)
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

func currentType(electricityTypes []string, plug string) string {
	for _, t := range electricityTypes {
		if strings.EqualFold(strings.TrimSpace(t), "DC") {
			return model.CurrentDC
		}
	}
	if len(electricityTypes) > 0 {
		return model.CurrentAC
	}
	switch plug {
	case "IEC_62196_T2_COMBO", "IEC_62196_T1_COMBO", "CHADEMO":
		return model.CurrentDC
	}
	return model.CurrentAC
}

// name prefers "Owner · Site" so cards stay recognisable: every Austrian record
// shares one cpo_id, so the operator would otherwise be lost.
func name(st stationJSON) string {
	owner := strings.TrimSpace(st.Owner)
	label := strings.TrimSpace(st.Label)
	if owner != "" && label != "" && owner != label {
		return owner + " · " + label
	}
	if label != "" {
		return label
	}
	return owner
}

func address(st stationJSON) string {
	parts := []string{}
	if st.Street != "" {
		parts = append(parts, st.Street+",")
	}
	if st.PostCode != "" {
		parts = append(parts, st.PostCode)
	}
	if st.City != "" {
		parts = append(parts, st.City)
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

func cents(c float64) float64  { return round4(c / 100) }
func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }
