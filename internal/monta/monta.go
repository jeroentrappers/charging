// Package monta reads the Monta Public API (DATEX-II-as-JSON): an open,
// paginated AFIR charge-points list (locations + power) plus a per-EVSE status
// endpoint (live availability + ad-hoc price) that needs an OAuth bearer token
// and only serves Monta's own (party "MON") EVSEs. Schemas per
// https://docs.public-api.monta.com (OpenAPI 2023-09-14).
package monta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/appmire/charging/internal/model"
)

const base = "https://public-api.monta.com"

// Client talks to the Monta Public API, caching the OAuth token.
type Client struct {
	clientID, clientSecret string
	http                   *http.Client
	limiter                *rate.Limiter // throttles the per-EVSE status calls (100/10min)

	mu    sync.Mutex
	token string
	exp   time.Time
}

func New(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         &http.Client{Timeout: 60 * time.Second},
		// 100 requests / 10 min: allow a burst, then ~1 per 6s.
		limiter: rate.NewLimiter(rate.Every(6*time.Second), 90),
	}
}

// SetLimit reconfigures the status-call rate limiter. Use a conservative rate
// for the background crawl so on-demand lookups (sharing the same 100/10min
// credential budget) still have headroom.
func (c *Client) SetLimit(every time.Duration, burst int) {
	c.limiter = rate.NewLimiter(rate.Every(every), burst)
}

func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.exp.Add(-60*time.Second)) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"clientId": c.clientID, "clientSecret": c.clientSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/auth/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("monta auth http %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken    string `json:"accessToken"`
		ExpirationDate string `json:"accessTokenExpirationDate"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil || tr.AccessToken == "" {
		return "", fmt.Errorf("monta auth: bad token response")
	}
	c.token = tr.AccessToken
	if t, e := time.Parse(time.RFC3339, tr.ExpirationDate); e == nil {
		c.exp = t
	} else {
		c.exp = time.Now().Add(10 * time.Minute)
	}
	return c.token, nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, int, error) {
	tok, err := c.ensureToken(ctx)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return raw, resp.StatusCode, nil
}

// ---- list (locations) ----

type mlString struct {
	Values []struct {
		Value string `json:"value"`
	} `json:"values"`
}

func (m mlString) first() string {
	if len(m.Values) > 0 {
		return m.Values[0].Value
	}
	return ""
}

type enumVal struct {
	Value string `json:"value"`
}

type listResp struct {
	Table []struct {
		Sites []struct {
			Name              mlString `json:"name"`
			LocationReference struct {
				Loc struct {
					PointByCoordinates struct {
						PointCoordinates struct {
							Latitude  float64 `json:"latitude"`
							Longitude float64 `json:"longitude"`
						} `json:"pointCoordinates"`
					} `json:"pointByCoordinates"`
					Ext struct {
						Facility struct {
							Address struct {
								Postcode string   `json:"postcode"`
								City     mlString `json:"city"`
							} `json:"address"`
						} `json:"FacilityLocation"`
					} `json:"locLocationExtensionG"`
				} `json:"locPointLocation"`
			} `json:"locationReference"`
			Operator struct {
				Org struct {
					Name mlString `json:"name"`
				} `json:"afacAnOrganisation"`
			} `json:"operator"`
			Stations []struct {
				RefillPoints []struct {
					CurrentType enumVal `json:"currentType"`
					ExternalID  []struct {
						Identifier string `json:"identifier"`
					} `json:"externalIdentifier"`
					Connector []struct {
						ConnectorType    enumVal `json:"connectorType"`
						ChargingMode     enumVal `json:"chargingMode"`
						MaxPowerAtSocket float64 `json:"maxPowerAtSocket"`
					} `json:"connector"`
				} `json:"refillPoint"`
			} `json:"energyInfrastructureStation"`
		} `json:"energyInfrastructureSite"`
	} `json:"energyInfrastructureTable"`
	Meta struct {
		Page    int `json:"page"`
		PerPage int `json:"perPage"`
		Total   int `json:"total"`
	} `json:"meta"`
}

// Locations fetches all pages of the AFIR charge-points list for a country and
// maps them to canonical connectors (no price/status yet).
func (c *Client) Locations(ctx context.Context, cpoID, country string) ([]model.Connector, error) {
	var out []model.Connector
	const perPage = 1000
	for page := 1; ; page++ {
		raw, code, err := c.get(ctx, fmt.Sprintf("/api/v1/afir/charge-points?country=%s&page=%d&perPage=%d", country, page, perPage))
		if err != nil {
			return nil, err
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("monta list http %d", code)
		}
		var lr listResp
		if err := json.Unmarshal(raw, &lr); err != nil {
			return nil, fmt.Errorf("monta list decode: %w", err)
		}
		seen := 0
		for _, tbl := range lr.Table {
			for _, s := range tbl.Sites {
				seen++
				lat := s.LocationReference.Loc.PointByCoordinates.PointCoordinates.Latitude
				lon := s.LocationReference.Loc.PointByCoordinates.PointCoordinates.Longitude
				addr := s.LocationReference.Loc.Ext.Facility.Address
				name := siteName(s.Operator.Org.Name.first(), s.Name.first())
				for _, st := range s.Stations {
					for _, rp := range st.RefillPoints {
						if len(rp.ExternalID) == 0 || len(rp.Connector) == 0 {
							continue
						}
						con := rp.Connector[0]
						out = append(out, model.Connector{
							CPOID:       cpoID,
							EVSEUID:     rp.ExternalID[0].Identifier,
							ConnectorID: "1",
							Lat:         lat,
							Lon:         lon,
							PowerKW:     round1(con.MaxPowerAtSocket / 1000),
							PlugType:    con.ConnectorType.Value,
							CurrentType: currentType(rp.CurrentType.Value, con.ChargingMode.Value),
							Name:        name,
							Address:     strings.TrimSpace(addr.Postcode + " " + addr.City.first()),
							PostalCode:  addr.Postcode,
							City:        addr.City.first(),
							EVSEStatus:  "", // filled by status calls (Monta-party only)
						})
					}
				}
			}
		}
		if seen < perPage || (lr.Meta.Total > 0 && page*perPage >= lr.Meta.Total) {
			break
		}
	}
	return out, nil
}

// ---- per-EVSE status (availability + ad-hoc price) ----

// montaEnum is the {"value":"…"} wrapper Monta adopted when it migrated the
// per-EVSE status endpoint to the DATEX/AFIR JSON encoding (2026-07). Fields
// that used to be bare strings (ratePolicy, priceType) are now this shape.
type montaEnum struct {
	Value string `json:"value"`
}

type statusResp struct {
	Status struct {
		EvseID             string `json:"evseId"`
		AvailabilityStatus string `json:"availabilityStatus"`
		EnergyRateUpdate   []struct {
			RatePolicy         montaEnum `json:"ratePolicy"`
			ApplicableCurrency []string  `json:"applicableCurrency"`
			EnergyPrice        []struct {
				PriceType   montaEnum `json:"priceType"`
				Value       float64   `json:"value"`
				TaxIncluded *bool     `json:"taxIncluded"`
			} `json:"energyPrice"`
		} `json:"energyRateUpdate"`
	} `json:"electricChargingPointStatus"`
}

// IsMonta reports whether the EVSE belongs to Monta's own party (status is only
// available for these; roaming partners return 400).
func IsMonta(evseID string) bool { return strings.Contains(evseID, "*MON*") }

// Status returns the live availability (as an OCPI-ish status) and, if present,
// the ad-hoc tariff for one EVSE. It is rate-limited.
func (c *Client) Status(ctx context.Context, evseID string) (status string, tariff *model.Tariff, err error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", nil, err
	}
	raw, code, err := c.get(ctx, "/api/v1/afir/charge-points/"+url.PathEscape(evseID)+"/status")
	if err != nil {
		return "", nil, err
	}
	if code != http.StatusOK {
		return "", nil, fmt.Errorf("monta status %s http %d", evseID, code)
	}
	var sr statusResp
	if err := json.Unmarshal(raw, &sr); err != nil {
		return "", nil, err
	}
	return mapStatus(sr.Status.AvailabilityStatus), mapTariff(evseID, sr), nil
}

// mapTariff builds a canonical ad-hoc tariff from the energyRateUpdate.
func mapTariff(evseID string, sr statusResp) *model.Tariff {
	for _, rate := range sr.Status.EnergyRateUpdate {
		if !strings.EqualFold(rate.RatePolicy.Value, "adHoc") {
			continue
		}
		// The feed may list a component twice (tax-incl + tax-excl). Keep one per
		// type, preferring the tax-inclusive (consumer) price.
		type pick struct {
			price   float64
			taxIncl bool
			order   int
		}
		best := map[string]pick{}
		order := 0
		for _, p := range rate.EnergyPrice {
			typ, perHour := componentType(p.PriceType.Value)
			if typ == "" {
				continue
			}
			price := p.Value
			if perHour {
				price *= 60 // per-minute → our model prices time per hour
			}
			incl := p.TaxIncluded != nil && *p.TaxIncluded
			if cur, ok := best[typ]; ok && (cur.taxIncl || !incl) {
				continue // keep existing (already tax-incl, or new one isn't an upgrade)
			}
			best[typ] = pick{price: price, taxIncl: incl, order: order}
			order++
		}
		if len(best) == 0 {
			continue
		}
		comps := make([]model.PriceComponent, 0, len(best))
		for typ, p := range best {
			comps = append(comps, model.PriceComponent{Type: typ, Price: p.price, StepSize: 1})
		}
		sort.Slice(comps, func(i, j int) bool { return best[comps[i].Type].order < best[comps[j].Type].order })
		cur := "EUR"
		if len(rate.ApplicableCurrency) > 0 && rate.ApplicableCurrency[0] != "" {
			cur = strings.ToUpper(rate.ApplicableCurrency[0])
		}
		return &model.Tariff{
			OCPIID:      evseID,
			Currency:    cur,
			Elements:    []model.TariffElement{{PriceComponents: comps}},
			LastUpdated: time.Time{},
		}
	}
	return nil
}

// componentType maps an AFIR priceType value to our component type and reports
// whether the price is per-minute (and so must be scaled to €/hour). Bounds
// (minimum/maximum) and unknown types yield "" and are skipped.
func componentType(priceType string) (typ string, perMinute bool) {
	switch priceType {
	case "pricePerKWh":
		return "ENERGY", false
	case "pricePerMinute":
		return "TIME", true
	case "pricePerHour":
		return "TIME", false
	case "flatRate", "basePrice":
		return "FLAT", false
	case "free":
		return "ENERGY", false
	default:
		return "", false
	}
}

func mapStatus(s string) string {
	if strings.EqualFold(s, "available") {
		return "AVAILABLE"
	}
	return strings.ToUpper(s)
}

func currentType(refillCurrent, mode string) string {
	v := strings.ToLower(refillCurrent + " " + mode)
	if strings.Contains(v, "dc") || strings.Contains(v, "mode4") {
		return model.CurrentDC
	}
	return model.CurrentAC
}

func siteName(operator, site string) string {
	switch {
	case operator != "" && site != "":
		return operator + " · " + site
	case site != "":
		return site
	default:
		return operator
	}
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
