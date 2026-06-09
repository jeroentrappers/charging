// Package ocpi contains types and a client for the OCPI 2.1.1 "sender"
// interface as published by Belgian CPOs on the National Access Point
// (transportdata.be) under AFIR Article 20.
//
// Spec reference: OCPI 2.1.1 modules Locations and Tariffs.
package ocpi

import "time"

// Envelope is the standard OCPI response wrapper.
type Envelope[T any] struct {
	Data       []T       `json:"data"`
	StatusCode int       `json:"status_code"`
	StatusMsg  string    `json:"status_message"`
	Timestamp  time.Time `json:"timestamp"`
}

// StatusSuccess is the OCPI status_code for a successful request.
const StatusSuccess = 1000

// ---- Versions / module discovery (OCPI versions endpoint) ----

// ObjectEnvelope wraps a single (non-list) OCPI data object.
type ObjectEnvelope[T any] struct {
	Data       T      `json:"data"`
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_message"`
}

// VersionDetails is the response of an OCPI version-details endpoint: the set of
// module endpoints exposed for that version.
type VersionDetails struct {
	Version   string     `json:"version"`
	Endpoints []Endpoint `json:"endpoints"`
}

type Endpoint struct {
	Identifier string `json:"identifier"` // e.g. "locations", "tariffs"
	Role       string `json:"role"`       // SENDER | RECEIVER (2.2+)
	URL        string `json:"url"`
}

// ---- Locations module ----

type Location struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Name        string           `json:"name"`
	Address     string           `json:"address"`
	City        string           `json:"city"`
	PostalCode  string           `json:"postal_code"`
	Country     string           `json:"country"`
	Coordinates GeoLocation      `json:"coordinates"`
	EVSEs       []EVSE           `json:"evses"`
	Operator    *BusinessDetails `json:"operator,omitempty"`
	LastUpdated time.Time        `json:"last_updated"`
}

type GeoLocation struct {
	// OCPI encodes coordinates as decimal strings, not numbers.
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
}

type BusinessDetails struct {
	Name string `json:"name"`
}

type EVSE struct {
	UID         string       `json:"uid"`
	EVSEID      string       `json:"evse_id"`
	Status      string       `json:"status"` // AVAILABLE, CHARGING, OUTOFORDER, ...
	Connectors  []Connector  `json:"connectors"`
	Coordinates *GeoLocation `json:"coordinates,omitempty"` // optional EVSE-level override
	LastUpdated time.Time    `json:"last_updated"`
}

type Connector struct {
	ID        string `json:"id"`
	Standard  string `json:"standard"`   // e.g. IEC_62196_T2, IEC_62196_T2_COMBO
	Format    string `json:"format"`     // SOCKET | CABLE
	PowerType string `json:"power_type"` // AC_1_PHASE | AC_3_PHASE | DC
	Voltage   int    `json:"voltage"`
	Amperage  int    `json:"amperage"`
	// MaxElectricPower (watts) is present in OCPI 2.2.1 and preferred over
	// computing power from voltage*amperage when available.
	MaxElectricPower int       `json:"max_electric_power"`
	TariffID         string    `json:"tariff_id"`
	TariffIDs        []string  `json:"tariff_ids"` // OCPI 2.2.1 uses a list
	LastUpdated      time.Time `json:"last_updated"`
}

// Tariff returns the connector's effective tariff id across OCPI versions
// (2.1.1 single tariff_id, 2.2.1 tariff_ids list).
func (c Connector) Tariff() string {
	if c.TariffID != "" {
		return c.TariffID
	}
	if len(c.TariffIDs) > 0 {
		return c.TariffIDs[0]
	}
	return ""
}

// ---- Tariffs module ----

type Tariff struct {
	ID          string          `json:"id"`
	Currency    string          `json:"currency"`
	Elements    []TariffElement `json:"elements"`
	LastUpdated time.Time       `json:"last_updated"`
}

type TariffElement struct {
	PriceComponents []PriceComponent    `json:"price_components"`
	Restrictions    *TariffRestrictions `json:"restrictions,omitempty"`
}

// PriceComponent.Type values (OCPI 2.1.1 DimensionType).
const (
	DimEnergy      = "ENERGY"       // price per kWh
	DimFlat        = "FLAT"         // price per session
	DimTime        = "TIME"         // price per hour while charging
	DimParkingTime = "PARKING_TIME" // price per hour while parked, not charging
)

type PriceComponent struct {
	Type     string  `json:"type"`
	Price    float64 `json:"price"`
	StepSize int     `json:"step_size"`
}

type TariffRestrictions struct {
	StartTime   string   `json:"start_time,omitempty"` // "HH:MM"
	EndTime     string   `json:"end_time,omitempty"`
	StartDate   string   `json:"start_date,omitempty"` // "YYYY-MM-DD"
	EndDate     string   `json:"end_date,omitempty"`
	MinKWh      *float64 `json:"min_kwh,omitempty"`
	MaxKWh      *float64 `json:"max_kwh,omitempty"`
	MinPower    *float64 `json:"min_power,omitempty"`
	MaxPower    *float64 `json:"max_power,omitempty"`
	MinDuration *int     `json:"min_duration,omitempty"` // seconds
	MaxDuration *int     `json:"max_duration,omitempty"`
	DayOfWeek   []string `json:"day_of_week,omitempty"`
}
