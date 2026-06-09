// Package model holds the canonical domain types shared across ingestion,
// pricing, and storage. They are deliberately decoupled from the OCPI wire
// format so that additional source protocols (e.g. DATEX II, mandatory on
// Belgian NAPs from 2026-04-14) can map into the same model later.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"
)

// Current type of a connector.
const (
	CurrentAC = "AC"
	CurrentDC = "DC"
)

// Connector is one physical plug (the unit we track and price).
type Connector struct {
	CPOID       string
	EVSEUID     string
	ConnectorID string

	Lat, Lon    float64
	PowerKW     float64
	PlugType    string // OCPI connector standard, e.g. IEC_62196_T2_COMBO
	CurrentType string // AC | DC
	Name        string
	Address     string

	EVSEStatus string // raw OCPI EVSE status (AVAILABLE, CHARGING, ...)
	TariffID   string // reference into the tariff map; "" if none
}

// Available reports whether this connector's EVSE is ready to use.
func (c Connector) Available() bool { return c.EVSEStatus == "AVAILABLE" }

// PriceComponent mirrors an OCPI price component (currency-neutral).
type PriceComponent struct {
	Type     string  `json:"type"` // ENERGY | FLAT | TIME | PARKING_TIME
	Price    float64 `json:"price"`
	StepSize int     `json:"step_size"`
}

// Restrictions limits when/how a tariff element applies.
type Restrictions struct {
	StartTime   string   `json:"start_time,omitempty"`
	EndTime     string   `json:"end_time,omitempty"`
	StartDate   string   `json:"start_date,omitempty"`
	EndDate     string   `json:"end_date,omitempty"`
	MinKWh      *float64 `json:"min_kwh,omitempty"`
	MaxKWh      *float64 `json:"max_kwh,omitempty"`
	MinPower    *float64 `json:"min_power,omitempty"`
	MaxPower    *float64 `json:"max_power,omitempty"`
	MinDuration *int     `json:"min_duration,omitempty"`
	MaxDuration *int     `json:"max_duration,omitempty"`
	DayOfWeek   []string `json:"day_of_week,omitempty"`
}

// TariffElement is a set of price components with optional restrictions.
type TariffElement struct {
	PriceComponents []PriceComponent `json:"price_components"`
	Restrictions    *Restrictions    `json:"restrictions,omitempty"`
}

// Tariff is a normalized, currency-aware ad-hoc tariff.
type Tariff struct {
	OCPIID      string          `json:"ocpi_id"`
	Currency    string          `json:"currency"`
	Elements    []TariffElement `json:"elements"`
	LastUpdated time.Time       `json:"-"` // excluded from the content hash
}

// Hash returns a stable content hash of the tariff's pricing structure,
// independent of element ordering and ignoring last_updated. Two tariffs with
// the same hash are considered economically identical, so no new historical
// version is recorded.
func (t Tariff) Hash() string {
	h := sha256.New()
	h.Write([]byte(t.Currency))
	h.Write([]byte{0})
	// Canonicalize: sort element fingerprints so ordering can't change the hash.
	prints := make([]string, 0, len(t.Elements))
	for _, el := range t.Elements {
		b, _ := json.Marshal(el)
		prints = append(prints, string(b))
	}
	slices.Sort(prints)
	for _, p := range prints {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Components returns the tariff serialized for storage in price_components jsonb.
func (t Tariff) Components() ([]byte, error) { return json.Marshal(t) }
