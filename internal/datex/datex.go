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

	// The Spanish NAP (DGT/MITERD) publishes the same profile but puts the site
	// coordinates on a PointLocation's coordinatesForDisplay, and the address
	// under _locationReferenceExtension as free-text lines ("Dirección: …",
	// "Municipio: …"). Used as fallbacks when the paths above are empty.
	DispLatitude  float64    `xml:"locationReference>coordinatesForDisplay>latitude"`
	DispLongitude float64    `xml:"locationReference>coordinatesForDisplay>longitude"`
	ExtPostalCode string     `xml:"locationReference>_locationReferenceExtension>facilityLocation>address>postcode"`
	ExtLines      []textLine `xml:"locationReference>_locationReferenceExtension>facilityLocation>address>addressLine"`
}

// textLine is one DATEX II AddressLine: a type plus a multilingual text value.
type textLine struct {
	Type string `xml:"type"`
	Text string `xml:"text>values>value"`
}

func (s site) lat() float64 {
	if s.Latitude != 0 {
		return s.Latitude
	}
	return s.DispLatitude
}

func (s site) lon() float64 {
	if s.Longitude != 0 {
		return s.Longitude
	}
	return s.DispLongitude
}

func (s site) postcode() string {
	if s.PostalCode != "" {
		return s.PostalCode
	}
	return s.ExtPostalCode
}

// labelledLine returns the text of the first address line whose value starts
// with one of the given label prefixes, with the label stripped. The Spanish
// NAP types every line "generalTextLine" and instead prefixes the value with a
// Spanish label, so the label is the only way to tell street from municipality.
func (s site) labelledLine(labels ...string) string {
	for _, l := range s.ExtLines {
		v := strings.TrimSpace(l.Text)
		low := strings.ToLower(v)
		for _, lab := range labels {
			if !strings.HasPrefix(low, lab) {
				continue
			}
			// Drop everything up to the label's colon; matching only the ASCII
			// stem of the label keeps accents out of the comparison.
			if _, rest, ok := strings.Cut(v, ":"); ok {
				return strings.TrimSpace(rest)
			}
			return strings.TrimSpace(v[len(lab):])
		}
	}
	return ""
}

// street returns the site's street address from the labelled address lines
// ("Dirección: Calle Marea Baja, 16"); empty when the feed carries none.
func (s site) street() string { return s.labelledLine("direcci") }

func (s site) city() string {
	if s.City != "" {
		return s.City
	}
	return s.labelledLine("municipio")
}

type station struct {
	RefillPoints []refillPoint `xml:"refillPoint"`
}

type refillPoint struct {
	ID            string  `xml:"id,attr"`
	ExternalID    string  `xml:"externalIdentifier"`
	Name          string  `xml:"name>values>value"`
	ConnectorType string  `xml:"connector>connectorType"`
	ChargingMode  string  `xml:"connector>chargingMode"`     // mode3AC3p (AC), mode4 (DC), ...
	MaxPowerW     float64 `xml:"connector>maxPowerAtSocket"` // watts
}

// uid picks the most stable identifier for a refill point. externalIdentifier
// is the publisher's roaming id and wins (Eco-Movement, Indigo). Failing that,
// a name that looks like an EVSE id is preferred over the id attribute: the
// Spanish NAP carries no externalIdentifier and its id attribute is an opaque
// token, while the name holds the real EVSE id ("ES*WEN*EWSMALAGA04DC2") — the
// only value we can trust to stay the same across daily republications.
func (rp refillPoint) uid() string {
	if rp.ExternalID != "" {
		return rp.ExternalID
	}
	if n := strings.TrimSpace(rp.Name); strings.Contains(n, "*") {
		return n
	}
	return rp.ID
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
	// Generous cap: the NAP feeds are large (Eco-Movement ≈ 31 MB) and generated
	// on demand, so the server can be slow to start responding. Cancellation
	// still honors the caller's ctx; this is just an upper bound.
	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(req)
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
				conns = append(conns, model.Connector{
					CPOID:       cpoID,
					EVSEUID:     rp.uid(),
					ConnectorID: "1",
					Lat:         s.lat(),
					Lon:         s.lon(),
					PowerKW:     round1(rp.MaxPowerW / 1000),
					PlugType:    rp.ConnectorType,
					CurrentType: currentType(rp.ChargingMode),
					Name:        name(s),
					Address:     address(s),
					PostalCode:  s.postcode(),
					City:        s.city(),
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
	if st := s.street(); st != "" {
		parts = append(parts, st+",")
	}
	if pc := s.postcode(); pc != "" {
		parts = append(parts, pc)
	}
	if c := s.city(); c != "" {
		parts = append(parts, c)
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
