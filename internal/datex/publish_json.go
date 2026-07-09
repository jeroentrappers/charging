package datex

// Outbound DATEX II v3 AFIR *JSON* encoding (profile "AFIR Energy
// Infrastructure" 01-00-00) — the JSON counterpart of publish.go. Field names
// and nesting mirror the wire types the JSON consumer reads in afir_json.go, so
// what we publish round-trips through ParseAFIRJSON. Unlike the XML encoding
// (which has an XSD gate), the JSON encoding's compliance check IS that
// round-trip plus structural review.

import (
	"encoding/json"
	"io"
	"time"
)

// ---- JSON output structs (mirror the jafir* parser types) ----

type joutValued struct {
	Value string `json:"value"`
}

type joutML struct {
	Values []joutMLValue `json:"values"`
}

type joutMLValue struct {
	Lang  string `json:"lang"`
	Value string `json:"value"`
}

func mlString(s string) joutML {
	return joutML{Values: []joutMLValue{{Lang: "en", Value: s}}}
}

type joutEnvelope struct {
	Payload joutPayload `json:"payload"`
}

type joutPayload struct {
	Table  *joutTablePub  `json:"aegiEnergyInfrastructureTablePublication,omitempty"`
	Status *joutStatusPub `json:"aegiEnergyInfrastructureStatusPublication,omitempty"`
}

type joutCreator struct {
	Country            string `json:"country"`
	NationalIdentifier string `json:"nationalIdentifier"`
}

// ---- table ----

type joutTablePub struct {
	Lang               string      `json:"lang"`
	PublicationTime    string      `json:"publicationTime"`
	PublicationCreator joutCreator `json:"publicationCreator"`
	Table              []joutTable `json:"energyInfrastructureTable"`
}

type joutTable struct {
	IDG   string     `json:"idG"`
	Sites []joutSite `json:"energyInfrastructureSite"`
}

type joutSite struct {
	IDG               string        `json:"idG"`
	LocationReference *joutLocRef   `json:"locationReference,omitempty"`
	Operator          *joutOperator `json:"operator,omitempty"`
	Station           []joutStation `json:"energyInfrastructureStation"`
}

type joutLocRef struct {
	LocPointLocation joutLocBody `json:"locPointLocation"`
}

type joutLocBody struct {
	PointByCoordinates joutPointBy `json:"pointByCoordinates"`
}

type joutPointBy struct {
	PointCoordinates joutCoords `json:"pointCoordinates"`
}

type joutCoords struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type joutOperator struct {
	Organisation joutOrg `json:"afacAnOrganisation"`
}

type joutOrg struct {
	Name joutML `json:"name"`
}

type joutStation struct {
	IDG         string            `json:"idG"`
	RefillPoint []joutRefillPoint `json:"refillPoint"`
}

type joutRefillPoint struct {
	ChargingPoint joutChargingPoint `json:"aegiElectricChargingPoint"`
}

type joutChargingPoint struct {
	IDG            string               `json:"idG"`
	CurrentType    joutValued           `json:"currentType"`
	Connector      []joutConnector      `json:"connector"`
	ElectricEnergy []joutElectricEnergy `json:"electricEnergy,omitempty"`
}

type joutConnector struct {
	ConnectorType    joutValued `json:"connectorType"`
	MaxPowerAtSocket float64    `json:"maxPowerAtSocket"`
}

type joutElectricEnergy struct {
	EnergyRate []joutEnergyRate `json:"energyRate"`
}

type joutEnergyRate struct {
	IDG                string            `json:"idG"`
	RatePolicy         joutValued        `json:"ratePolicy"`
	ApplicableCurrency []string          `json:"applicableCurrency"`
	EnergyPrice        []joutEnergyPrice `json:"energyPrice"`
}

type joutEnergyPrice struct {
	PriceType joutValued `json:"priceType"`
	Value     float64    `json:"value"`
}

// ---- status ----

type joutStatusPub struct {
	Lang               string           `json:"lang"`
	PublicationTime    string           `json:"publicationTime"`
	PublicationCreator joutCreator      `json:"publicationCreator"`
	SiteStatus         []joutSiteStatus `json:"energyInfrastructureSiteStatus"`
}

type joutSiteStatus struct {
	Reference     joutRef             `json:"reference"`
	StationStatus []joutStationStatus `json:"energyInfrastructureStationStatus"`
}

type joutStationStatus struct {
	Reference         joutRef                 `json:"reference"`
	RefillPointStatus []joutRefillPointStatus `json:"refillPointStatus"`
}

type joutRefillPointStatus struct {
	ChargingPointStatus joutChargingPointStatus `json:"aegiElectricChargingPointStatus"`
}

type joutChargingPointStatus struct {
	Reference joutRef    `json:"reference"`
	Status    joutValued `json:"status"`
}

type joutRef struct {
	TargetClass string `json:"targetClass"`
	IDG         string `json:"idG"`
}

func joutReference(id string) joutRef {
	return joutRef{TargetClass: "afac:FacilityObject", IDG: id}
}

// ---- Public writers ----

// WriteAFIRTableJSON writes the table publication in the AFIR JSON encoding.
func WriteAFIRTableJSON(w io.Writer, sites []PublishSite, creator Creator, now time.Time) error {
	tbl := joutTable{IDG: "T1", Sites: make([]joutSite, 0, len(sites))}
	for _, s := range sites {
		tbl.Sites = append(tbl.Sites, buildJSONSite(s))
	}
	env := joutEnvelope{Payload: joutPayload{Table: &joutTablePub{
		Lang:               "en",
		PublicationTime:    now.UTC().Format(time.RFC3339),
		PublicationCreator: joutCreator{Country: creator.Country, NationalIdentifier: creator.NationalIdentifier},
		Table:              []joutTable{tbl},
	}}}
	return writeJSONIndent(w, env)
}

// WriteAFIRStatusJSON writes the status publication in the AFIR JSON encoding.
// All statuses are grouped under one synthetic site/station; the refill point
// reference idG is the join key back to the table.
func WriteAFIRStatusJSON(w io.Writer, statuses []PublishStatus, creator Creator, now time.Time) error {
	rps := make([]joutRefillPointStatus, 0, len(statuses))
	for _, s := range statuses {
		rps = append(rps, joutRefillPointStatus{ChargingPointStatus: joutChargingPointStatus{
			Reference: joutReference(s.RefillPointID),
			Status:    joutValued{Value: statusEnum(s.Status)},
		}})
	}
	env := joutEnvelope{Payload: joutPayload{Status: &joutStatusPub{
		Lang:               "en",
		PublicationTime:    now.UTC().Format(time.RFC3339),
		PublicationCreator: joutCreator{Country: creator.Country, NationalIdentifier: creator.NationalIdentifier},
		SiteStatus: []joutSiteStatus{{
			Reference: joutReference("status-site"),
			StationStatus: []joutStationStatus{{
				Reference:         joutReference("status-station"),
				RefillPointStatus: rps,
			}},
		}},
	}}}
	return writeJSONIndent(w, env)
}

func buildJSONSite(s PublishSite) joutSite {
	out := joutSite{IDG: s.ID}
	if s.Lat != 0 || s.Lon != 0 {
		out.LocationReference = &joutLocRef{LocPointLocation: joutLocBody{
			PointByCoordinates: joutPointBy{PointCoordinates: joutCoords{Latitude: s.Lat, Longitude: s.Lon}},
		}}
	}
	if s.Name != "" {
		out.Operator = &joutOperator{Organisation: joutOrg{Name: mlString(s.Name)}}
	}
	for _, st := range s.Stations {
		jst := joutStation{IDG: st.ID}
		for _, rp := range st.RefillPoints {
			jst.RefillPoint = append(jst.RefillPoint, buildJSONRefillPoint(rp))
		}
		out.Station = append(out.Station, jst)
	}
	return out
}

func buildJSONRefillPoint(rp PublishRefillPoint) joutRefillPoint {
	cp := joutChargingPoint{
		IDG:         rp.ID,
		CurrentType: joutValued{Value: currentTypeEnum(rp.CurrentType)},
		Connector: []joutConnector{{
			ConnectorType:    joutValued{Value: connectorTypeEnum(rp.ConnectorType)},
			MaxPowerAtSocket: rp.PowerKW * 1000,
		}},
	}
	if rp.Rate != nil {
		if rate, ok := buildJSONRate(rp.ID, *rp.Rate); ok {
			cp.ElectricEnergy = []joutElectricEnergy{{EnergyRate: []joutEnergyRate{rate}}}
		}
	}
	return joutRefillPoint{ChargingPoint: cp}
}

func buildJSONRate(rpID string, r PublishRate) (joutEnergyRate, bool) {
	prices := make([]joutEnergyPrice, 0, len(r.Prices))
	for _, p := range r.Prices {
		enum, v, ok := priceTypeEnum(p.Type, p.Value)
		if !ok {
			continue
		}
		// The JSON encoding has no fractionDigits facet, so we keep full price
		// precision here (e.g. 0.368 €/kWh) — only the XSD-bound XML rounds to the
		// AmountOfMoney 2-digit limit. jsonPrice snaps off float64 noise. See the
		// note in publish.go.
		prices = append(prices, joutEnergyPrice{PriceType: joutValued{Value: enum}, Value: jsonPrice(v)})
	}
	if len(prices) == 0 {
		return joutEnergyRate{}, false
	}
	cur := normalizeCurrency(r.Currency)
	return joutEnergyRate{
		IDG:                "rate-" + rpID,
		RatePolicy:         joutValued{Value: "adHoc"},
		ApplicableCurrency: []string{cur},
		EnergyPrice:        prices,
	}, true
}

func writeJSONIndent(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
