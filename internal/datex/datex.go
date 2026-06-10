// Package datex reads DATEX II (v3, EnergyInfrastructure profile) charging data
// into the canonical model. DATEX II is the mandatory NAP format from
// 2026-04-14 and what aggregators such as Eco-Movement publish today.
//
// Element paths were validated against the live Eco-Movement NAP feed
// (api.eco-movement.com/api/nap/datexii/locations). That static publication
// carries locations + connector type + max power, but NOT ad-hoc price or live
// status, so connectors parsed here have no tariff and unknown availability.
// Matching is by local element name (namespace-agnostic).
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
	PostalCode string    `xml:"locationReference>_pointLocationExtension>facilityLocation>address>postcode"`
	City       string    `xml:"locationReference>_pointLocationExtension>facilityLocation>address>city"`
	Operator   string    `xml:"operator>name>values>value"`
	Stations   []station `xml:"energyInfrastructureStation"`
}

type station struct {
	RefillPoints []refillPoint `xml:"refillPoint"`
}

type refillPoint struct {
	ID            string  `xml:"id,attr"`
	ExternalID    string  `xml:"externalIdentifier"`
	ConnectorType string  `xml:"connector>connectorType"`
	ChargingMode  string  `xml:"connector>chargingMode"`     // mode3AC3p (AC), mode4 (DC), ...
	MaxPowerW     float64 `xml:"connector>maxPowerAtSocket"` // watts
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
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("datex http %d", resp.StatusCode)
	}
	return Parse(cpoID, body)
}

// Parse maps a DATEX II EnergyInfrastructure publication to canonical
// connectors. No tariffs are present in this profile, so the tariff map is empty.
func Parse(cpoID string, data []byte) ([]model.Connector, map[string]model.Tariff, error) {
	var pub publication
	if err := xml.Unmarshal(data, &pub); err != nil {
		return nil, nil, fmt.Errorf("decode datex: %w", err)
	}
	tariffs := map[string]model.Tariff{}
	var conns []model.Connector

	for _, s := range pub.Sites {
		for _, st := range s.Stations {
			for _, rp := range st.RefillPoints {
				uid := rp.ExternalID
				if uid == "" {
					uid = rp.ID
				}
				conns = append(conns, model.Connector{
					CPOID:       cpoID,
					EVSEUID:     uid,
					ConnectorID: "1",
					Lat:         s.Latitude,
					Lon:         s.Longitude,
					PowerKW:     round1(rp.MaxPowerW / 1000),
					PlugType:    rp.ConnectorType,
					CurrentType: currentType(rp.ChargingMode),
					Name:        name(s),
					Address:     address(s),
					PostalCode:  s.PostalCode,
					City:        s.City,
					EVSEStatus:  "", // not in this DATEX profile
				})
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

// name prefers "Operator · Site" so cards are recognisable (all sites share one
// cpo_id, so the operator would otherwise be lost).
func name(s site) string {
	if s.Operator != "" && s.Name != "" {
		return s.Operator + " · " + s.Name
	}
	if s.Name != "" {
		return s.Name
	}
	return s.Operator
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
