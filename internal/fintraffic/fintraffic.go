// Package fintraffic reads Finland's national AFIR charging feed, published
// open (no key) by Fintraffic on afir.digitraffic.fi.
//
// Fintraffic collects OCPI from the Finnish CPOs and republishes it as one
// national service, so the payload is OCPI 2.2.1 in all but spelling: the JSON
// uses camelCase keys and splits the data over three endpoints
//
//	/locations          GeoJSON FeatureCollection, OCPI location per feature
//	/locations/statuses one live status per EVSE id
//	/tariffs            OCPI tariffs (ad-hoc price), referenced by tariffIds
//
// Each of the three has an "/all" sibling that answers with the complete set in
// one response, which is what this package asks for.
//
// Rather than re-derive power, restrictions and address handling, this package
// translates the payload into the internal/ocpi wire types and lets
// internal/normalize do the mapping — the same path NL DOT-NL and Road take.
//
// Two request quirks, both enforced by the server: it requires a
// "Digitraffic-User" identification header, and it refuses requests that do not
// accept gzip (HTTP 406). Snapshots regenerate every minute.
package fintraffic

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/appmire/charging/internal/ocpi"
)

// UserAgent identifies us to Digitraffic, which requires the header on every
// request (their fair-use policy: identify yourself so they can reach you).
const UserAgent = "appmire-charging (https://charging.appmire.be)"

// maxPages bounds cursor-following. The /all endpoints return everything in one
// response today; the cursor loop is a safety net if that ever changes.
const maxPages = 200

// pageLimit is the page size for cursor-following. The API validates the
// parameter ("Limit must be one of: 500 or ALL"), so 500 is the only usable
// value once we are paging rather than asking for /all.
const pageLimit = 500

// ---- wire types (camelCase OCPI) ----

type locationsResponse struct {
	Features   []locationFeature `json:"features"`
	Pagination pagination        `json:"pagination"`
	ModifiedAt time.Time         `json:"modifiedAt"`
}

type pagination struct {
	NextCursor string `json:"nextCursor"`
}

type locationFeature struct {
	Geometry   *geometry          `json:"geometry"`
	Properties locationProperties `json:"properties"`
}

type geometry struct {
	Coordinates []float64 `json:"coordinates"` // [lon, lat]
}

type locationProperties struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Operator *operator `json:"operator"`
	Address  *address  `json:"address"`
	EVSEs    []evse    `json:"evses"`
}

type operator struct {
	Details *struct {
		Name string `json:"name"`
	} `json:"details"`
}

type address struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
}

type evse struct {
	ID         string      `json:"id"`
	Geometry   *geometry   `json:"geometry"`
	Connectors []connector `json:"connectors"`
}

type connector struct {
	PowerType        string   `json:"powerType"`
	Standard         string   `json:"standard"`
	Format           string   `json:"format"`
	MaxVoltage       int      `json:"maxVoltage"`
	MaxAmperage      int      `json:"maxAmperage"`
	MaxElectricPower int      `json:"maxElectricPower"` // watts
	TariffIDs        []string `json:"tariffIds"`
}

type statusesResponse struct {
	Statuses   []evseStatus `json:"statuses"`
	Pagination pagination   `json:"pagination"`
}

type evseStatus struct {
	EVSEID string `json:"evseId"`
	Status string `json:"status"` // AVAILABLE | CHARGING | OUTOFORDER | ...
}

type tariffsResponse struct {
	Tariffs    []tariff   `json:"tariffs"`
	Pagination pagination `json:"pagination"`
}

type tariff struct {
	ID          string          `json:"id"`
	Currency    string          `json:"currency"`
	Type        string          `json:"type"`        // AD_HOC_PAYMENT | REGULAR | ""
	TaxIncluded string          `json:"taxIncluded"` // YES | NO
	Elements    []tariffElement `json:"elements"`
	LastUpdated time.Time       `json:"lastUpdated"`
}

type tariffElement struct {
	PriceComponents []priceComponent `json:"priceComponents"`
	Restrictions    *restrictions    `json:"restrictions"`
}

type priceComponent struct {
	Type     string   `json:"type"`
	Price    float64  `json:"price"`
	StepSize int      `json:"stepSize"`
	VAT      *float64 `json:"vat"` // percent, e.g. 25.5
}

type restrictions struct {
	StartTime   string   `json:"startTime"`
	EndTime     string   `json:"endTime"`
	StartDate   string   `json:"startDate"`
	EndDate     string   `json:"endDate"`
	MinKWh      *float64 `json:"minKwh"`
	MaxKWh      *float64 `json:"maxKwh"`
	MinPower    *float64 `json:"minPower"`
	MaxPower    *float64 `json:"maxPower"`
	MinDuration *int     `json:"minDuration"` // seconds
	MaxDuration *int     `json:"maxDuration"`
	DayOfWeek   []string `json:"dayOfWeek"`
}

// ---- fetching ----

// Client talks to one Fintraffic AFIR base URL (e.g.
// https://afir.digitraffic.fi/api/charging-network/v1).
type Client struct {
	Base string
	HTTP *http.Client
}

func New(base string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"),
		HTTP: &http.Client{Timeout: 300 * time.Second},
	}
}

// locations returns every published location (all pages).
func (c *Client) locations(ctx context.Context) ([]locationFeature, time.Time, error) {
	var out []locationFeature
	var mod time.Time
	err := c.pages(ctx, "/locations", func(b []byte) (string, error) {
		var r locationsResponse
		if err := json.Unmarshal(b, &r); err != nil {
			return "", err
		}
		out = append(out, r.Features...)
		if r.ModifiedAt.After(mod) {
			mod = r.ModifiedAt
		}
		return r.Pagination.NextCursor, nil
	})
	return out, mod, err
}

// statuses returns the current EVSE status keyed by EVSE id.
func (c *Client) statuses(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	err := c.pages(ctx, "/locations/statuses", func(b []byte) (string, error) {
		var r statusesResponse
		if err := json.Unmarshal(b, &r); err != nil {
			return "", err
		}
		for _, s := range r.Statuses {
			out[s.EVSEID] = s.Status
		}
		return r.Pagination.NextCursor, nil
	})
	return out, err
}

// tariffs returns every published tariff.
func (c *Client) tariffs(ctx context.Context) ([]tariff, error) {
	var out []tariff
	err := c.pages(ctx, "/tariffs", func(b []byte) (string, error) {
		var r tariffsResponse
		if err := json.Unmarshal(b, &r); err != nil {
			return "", err
		}
		out = append(out, r.Tariffs...)
		return r.Pagination.NextCursor, nil
	})
	return out, err
}

// Snapshot fetches one consistent view of the feed as OCPI types. Tariffs are
// only fetched when asked for: the availability pass does not need them, and
// skipping them halves the work.
func (c *Client) Snapshot(ctx context.Context, withTariffs bool) ([]ocpi.Location, []ocpi.Tariff, error) {
	locs, _, err := c.locations(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fintraffic locations: %w", err)
	}
	st, err := c.statuses(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fintraffic statuses: %w", err)
	}
	var tars []tariff
	if withTariffs {
		if tars, err = c.tariffs(ctx); err != nil {
			return nil, nil, fmt.Errorf("fintraffic tariffs: %w", err)
		}
	}
	ol, ot := ToOCPI(locs, st, tars)
	return ol, ot, nil
}

// pages GETs path's /all sibling and follows nextCursor until it is empty.
//
// The complete set is requested as /<path>/all rather than /<path>?limit=ALL:
// the latter answers 302 to the former and drops the query string on the way,
// which would silently lose a cursor. Since /all ignores a cursor and always
// answers in full, following one means falling back to the paged base path.
func (c *Client) pages(ctx context.Context, path string, handle func([]byte) (string, error)) error {
	cursor := ""
	for page := 0; page < maxPages; page++ {
		u := c.Base + path + "/all"
		if cursor != "" {
			u = fmt.Sprintf("%s%s?limit=%d&cursor=%s", c.Base, path, pageLimit, url.QueryEscape(cursor))
		}
		body, err := c.get(ctx, u)
		if err != nil {
			return err
		}
		next, err := handle(body)
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if next == "" || next == cursor {
			return nil
		}
		cursor = next
	}
	return fmt.Errorf("fintraffic %s: cursor did not terminate after %d pages", path, maxPages)
}

// get performs one request with the headers Digitraffic requires, gunzipping the
// response itself: asking for gzip is mandatory (the server answers 406
// otherwise), so we cannot rely on the transport's transparent handling.
func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Digitraffic-User", UserAgent)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Accept", "application/json, application/geo+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fintraffic http %d", resp.StatusCode)
	}
	var r io.Reader = resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gunzip: %w", err)
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(io.LimitReader(r, 512<<20))
}

// ---- mapping to the OCPI wire types ----

// ToOCPI converts a decoded Fintraffic snapshot into OCPI locations (with the
// live status folded in) and OCPI tariffs, ready for internal/normalize.
//
// Connectors carry no id of their own in this feed, so they are numbered by
// position within their EVSE ("1", "2", …) — stable as long as the publisher
// keeps connector order, which it has across snapshots.
func ToOCPI(locs []locationFeature, statuses map[string]string, tariffs []tariff) ([]ocpi.Location, []ocpi.Tariff) {
	out := make([]ocpi.Location, 0, len(locs))
	for _, f := range locs {
		p := f.Properties
		loc := ocpi.Location{
			ID:          p.ID,
			Name:        locationName(p),
			Coordinates: geoOf(f.Geometry),
		}
		if p.Address != nil {
			loc.Address, loc.City, loc.PostalCode = p.Address.Street, p.Address.City, p.Address.PostalCode
		}
		for _, e := range p.EVSEs {
			oe := ocpi.EVSE{UID: e.ID, EVSEID: e.ID, Status: statuses[e.ID]}
			if e.Geometry != nil {
				g := geoOf(e.Geometry)
				oe.Coordinates = &g
			}
			for i, c := range e.Connectors {
				oe.Connectors = append(oe.Connectors, ocpi.Connector{
					ID:               strconv.Itoa(i + 1),
					Standard:         c.Standard,
					Format:           c.Format,
					PowerType:        c.PowerType,
					MaxVoltage:       c.MaxVoltage,
					MaxAmperage:      c.MaxAmperage,
					MaxElectricPower: c.MaxElectricPower,
					TariffIDs:        c.TariffIDs,
				})
			}
			loc.EVSEs = append(loc.EVSEs, oe)
		}
		out = append(out, loc)
	}

	tars := make([]ocpi.Tariff, 0, len(tariffs))
	for _, t := range tariffs {
		tars = append(tars, toOCPITariff(t))
	}
	return out, tars
}

// toOCPITariff converts one tariff, grossing up net prices to what a driver
// actually pays.
//
// Fintraffic publishes every tariff with taxIncluded="NO" and states the VAT
// rate per price component (25.5% in Finland). Every other source we ingest
// quotes tax-inclusive ad-hoc prices, so leaving these net would make Finnish
// chargers look about a fifth cheaper than they are and rank them wrongly.
// Components without a stated VAT rate are taken as-is.
func toOCPITariff(t tariff) ocpi.Tariff {
	gross := strings.EqualFold(t.TaxIncluded, "NO")
	out := ocpi.Tariff{
		ID:          t.ID,
		Currency:    t.Currency,
		LastUpdated: t.LastUpdated,
		Elements:    make([]ocpi.TariffElement, 0, len(t.Elements)),
	}
	for _, el := range t.Elements {
		oel := ocpi.TariffElement{}
		for _, pc := range el.PriceComponents {
			price := pc.Price
			if gross && pc.VAT != nil && *pc.VAT > 0 {
				price = round4(price * (1 + *pc.VAT/100))
			}
			oel.PriceComponents = append(oel.PriceComponents, ocpi.PriceComponent{
				Type:     pc.Type,
				Price:    price,
				StepSize: pc.StepSize,
			})
		}
		if r := el.Restrictions; r != nil {
			oel.Restrictions = &ocpi.TariffRestrictions{
				StartTime: r.StartTime, EndTime: r.EndTime,
				StartDate: r.StartDate, EndDate: r.EndDate,
				MinKWh: r.MinKWh, MaxKWh: r.MaxKWh,
				MinPower: r.MinPower, MaxPower: r.MaxPower,
				MinDuration: r.MinDuration, MaxDuration: r.MaxDuration,
				DayOfWeek: r.DayOfWeek,
			}
		}
		out.Elements = append(out.Elements, oel)
	}
	return out
}

// locationName prefers "Operator · Site" so cards stay recognisable: every
// Finnish location shares one cpo_id, so the operator would otherwise be lost.
func locationName(p locationProperties) string {
	op := ""
	if p.Operator != nil && p.Operator.Details != nil {
		op = p.Operator.Details.Name
	}
	if op != "" && p.Name != "" {
		return op + " · " + p.Name
	}
	if p.Name != "" {
		return p.Name
	}
	return op
}

// geoOf formats a GeoJSON [lon, lat] pair as OCPI's decimal strings.
func geoOf(g *geometry) ocpi.GeoLocation {
	if g == nil || len(g.Coordinates) < 2 {
		return ocpi.GeoLocation{}
	}
	return ocpi.GeoLocation{
		Latitude:  strconv.FormatFloat(g.Coordinates[1], 'f', -1, 64),
		Longitude: strconv.FormatFloat(g.Coordinates[0], 'f', -1, 64),
	}
}

func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }
