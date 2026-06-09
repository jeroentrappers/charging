// Package datex reads DATEX II (v3, EnergyInfrastructure profile) charging data
// into the canonical model. DATEX II becomes the mandatory NAP format on
// 2026-04-14 and is what aggregators such as Eco-Movement publish today.
//
// NOTE: element paths here follow the DATEX II v3 EnergyInfrastructure profile
// and are matched by local name (namespace-agnostic). The exact nesting varies
// slightly between publishers, so paths may need tuning against a real feed —
// validated against the Eco-Movement NAP feed once access is granted. The
// static publication carries locations + (optionally) ad-hoc price; live
// availability is a separate status publication, so connectors parsed here have
// unknown status until that is wired in.
package datex

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/appmire/charging/internal/model"
)

// ---- DATEX II XML structs (local-name matching) ----

type publication struct {
	Sites []site `xml:"energyInfrastructureTable>energyInfrastructureSite"`
}

type site struct {
	ID         string    `xml:"id,attr"`
	Name       string    `xml:"name>values>value"`
	Latitude   float64   `xml:"locationReference>pointByCoordinates>pointCoordinates>latitude"`
	Longitude  float64   `xml:"locationReference>pointByCoordinates>pointCoordinates>longitude"`
	PostalCode string    `xml:"locationReference>addressByName>postalCode"`
	City       string    `xml:"locationReference>addressByName>city"`
	Stations   []station `xml:"energyInfrastructureStation"`
}

type station struct {
	ID           string        `xml:"id,attr"`
	RefillPoints []refillPoint `xml:"refillPoint"`
}

type refillPoint struct {
	ID            string   `xml:"id,attr"`
	ConnectorType string   `xml:"connector>connectorType"`
	ChargingMode  string   `xml:"connector>chargingMode"` // e.g. "mode4" (DC) / "mode3" (AC), or AC/DC text
	MaxPowerW     float64  `xml:"connector>maximumPower"` // watts
	PricePerKWh   *float64 `xml:"applicablePrice>priceForEnergy>priceForKWh"`
	Currency      string   `xml:"applicablePrice>priceForEnergy>currency"`
}

// Fetch retrieves and parses a DATEX II locations publication.
func Fetch(ctx context.Context, cpoID, url, token string) ([]model.Connector, map[string]model.Tariff, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/xml")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("datex http %d", resp.StatusCode)
	}
	return Parse(cpoID, body)
}

// Parse maps a DATEX II EnergyInfrastructure publication to canonical connectors
// and tariffs (keyed by a synthesized tariff id).
func Parse(cpoID string, data []byte) ([]model.Connector, map[string]model.Tariff, error) {
	var pub publication
	if err := xml.Unmarshal(data, &pub); err != nil {
		return nil, nil, fmt.Errorf("decode datex: %w", err)
	}
	var conns []model.Connector
	tariffs := map[string]model.Tariff{}

	for _, s := range pub.Sites {
		for _, st := range s.Stations {
			for _, rp := range st.RefillPoints {
				c := model.Connector{
					CPOID:       cpoID,
					EVSEUID:     st.ID,
					ConnectorID: rp.ID,
					Lat:         s.Latitude,
					Lon:         s.Longitude,
					PowerKW:     round1(rp.MaxPowerW / 1000),
					PlugType:    rp.ConnectorType,
					CurrentType: currentType(rp.ChargingMode),
					Name:        s.Name,
					Address:     address(s),
					PostalCode:  s.PostalCode,
					City:        s.City,
					// DATEX static feed carries no live status (separate publication).
					EVSEStatus: "",
				}
				if rp.PricePerKWh != nil {
					tid := "datex:" + cpoID + ":" + st.ID + ":" + rp.ID
					cur := rp.Currency
					if cur == "" {
						cur = "EUR"
					}
					tariffs[tid] = model.Tariff{
						OCPIID:   tid,
						Currency: cur,
						Elements: []model.TariffElement{{
							PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: *rp.PricePerKWh}},
						}},
					}
					c.TariffID = tid
				}
				conns = append(conns, c)
			}
		}
	}
	return conns, tariffs, nil
}

func currentType(mode string) string {
	m := strings.ToLower(mode)
	if strings.Contains(m, "dc") || strings.Contains(m, "mode4") {
		return model.CurrentDC
	}
	return model.CurrentAC
}

func address(s site) string {
	parts := []string{}
	if s.PostalCode != "" {
		parts = append(parts, s.PostalCode)
	}
	if s.City != "" {
		parts = append(parts, s.City)
	}
	return strings.Join(parts, " ")
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
