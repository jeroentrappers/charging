package datex

// Outbound DATEX II v3 (AFIR Recharging profile) publisher: turns our canonical
// data into the XML encoding an AFIR/NAP consumer expects. This is the inverse
// of afir.go, and is entirely separate from the parsing structs there — XML
// *marshaling* needs explicit namespace prefixes and xsi:type attributes, which
// the namespace-agnostic parser structs deliberately drop.
//
// Both documents use the <d2:payload xsi:type="aegi:…"> root and validate
// against the official DATEX II v3.7 schema set (see scripts/validate-datex.sh).
// The pricing lives under refillPoint>energyProduct>energyRate and the live
// status under energyInfrastructureSiteStatus, matching where the schema puts
// them.

import (
	"encoding/xml"
	"io"
	"strings"
	"time"

	"github.com/appmire/charging/internal/model"
)

// DATEX II v3 namespace URIs (the AFIR profile subset we emit).
const (
	nsD2   = "http://datex2.eu/schema/3/d2Payload"
	nsCom  = "http://datex2.eu/schema/3/common"
	nsAfac = "http://datex2.eu/schema/3/afirFacilities"
	nsLoc  = "http://datex2.eu/schema/3/locationReferencing"
	nsAegi = "http://datex2.eu/schema/3/afirEnergyInfrastructure"
	nsXsi  = "http://www.w3.org/2001/XMLSchema-instance"
)

// modelBaseVersion is the fixed="3" attribute the payload schema requires.
const modelBaseVersion = "3"

// ---- Neutral publish inputs (shared by the XML and JSON encoders) ----

// Creator identifies the publishing NAP participant (publicationCreator).
type Creator struct {
	Country            string // ISO 3166-1 alpha-2, lowercased on output (CountryCode)
	NationalIdentifier string
}

// PublishSite is one physical location: a site holding one or more stations.
type PublishSite struct {
	ID         string
	Name       string
	Lat, Lon   float64
	PostalCode string
	City       string
	Street     string
	Stations   []PublishStation
}

// PublishStation groups refill points (kept 1:1 with a site here, mirroring the
// export's one-EVSE-per-location shape).
type PublishStation struct {
	ID           string
	RefillPoints []PublishRefillPoint
}

// PublishRefillPoint is one connector we publish.
type PublishRefillPoint struct {
	ID            string
	CurrentType   string  // model.CurrentAC | model.CurrentDC
	ConnectorType string  // OCPI connector standard, e.g. IEC_62196_T2_COMBO
	PowerKW       float64 // max power at socket
	Rate          *PublishRate
}

// PublishRate is an ad-hoc EnergyRate (nil when the refill point has no price).
type PublishRate struct {
	Currency    string
	LastUpdated time.Time
	Prices      []PublishPrice
}

// PublishPrice is one EnergyPrice component in our vocabulary.
type PublishPrice struct {
	Type  string  // model PriceComponent type: ENERGY | TIME | FLAT
	Value float64 // in the model's units (TIME is €/hour)
}

// PublishStatus is one refill point's live availability (the status delta).
type PublishStatus struct {
	RefillPointID string
	Status        string // OUR EVSE status vocabulary (AVAILABLE, CHARGING, ...)
}

// ---- Reverse vocabulary (inverse of the tables in afir.go) ----

// plugToConnectorType maps our OCPI connector standard back to a DATEX II
// ConnectorTypeEnum value. Unknown values fall through to "other".
var plugToConnectorType = map[string]string{
	"IEC_62196_T2":       "iec62196T2",
	"IEC_62196_T2_COMBO": "iec62196T2COMBO",
	"IEC_62196_T1":       "iec62196T1",
	"IEC_62196_T1_COMBO": "iec62196T1COMBO",
	"CHADEMO":            "chademo",
	"DOMESTIC_F":         "domesticFType",
	"TESLA_S":            "teslaS",
	"TESLA_R":            "teslaR",
}

func connectorTypeEnum(plug string) string {
	if v, ok := plugToConnectorType[strings.ToUpper(plug)]; ok {
		return v
	}
	return "other"
}

// currentTypeEnum maps our AC/DC to the CurrentTypeEnum.
func currentTypeEnum(ct string) string {
	if ct == model.CurrentDC {
		return "dc"
	}
	return "ac"
}

// statusEnum maps our EVSE status vocabulary to a RefillPointStatusEnum value
// (inverse of statusVocab). Unknown/empty → "unknown".
func statusEnum(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "AVAILABLE":
		return "available"
	case "CHARGING", "OCCUPIED":
		return "charging"
	case "OUTOFORDER":
		return "outOfOrder"
	default:
		return "unknown"
	}
}

// priceTypeEnum maps a model price-component type to a PriceTypeEnum value, and
// converts the value into DATEX units where they differ (TIME is €/hour in the
// model but pricePerMinute in DATEX). All values are rounded to 2 fraction
// digits because the schema's AmountOfMoney type permits no more (real prices
// like 0.368 €/kWh otherwise fail XSD validation). Returns ok=false for types
// we don't emit.
func priceTypeEnum(t string, v float64) (enum string, value float64, ok bool) {
	switch strings.ToUpper(t) {
	case "ENERGY":
		return "pricePerKWh", round2(v), true
	case "TIME":
		return "pricePerMinute", round2(v / 60), true
	case "FLAT":
		return "flatRate", round2(v), true
	default:
		return "", 0, false
	}
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

// ---- XML marshaling structs (prefixes emitted literally; xmlns declared on
// the root). encoding/xml does not rewrite prefixes, so we control them here to
// match exactly the shapes validated against the official v3.7 XSDs. ----

type xmlPayload struct {
	XMLName          xml.Name
	XsiType          string `xml:"xsi:type,attr"`
	Lang             string `xml:"lang,attr"`
	ModelBaseVersion string `xml:"modelBaseVersion,attr"`

	XMLNSd2   string `xml:"xmlns:d2,attr"`
	XMLNScom  string `xml:"xmlns:com,attr"`
	XMLNSafac string `xml:"xmlns:afac,attr"`
	XMLNSloc  string `xml:"xmlns:loc,attr"`
	XMLNSaegi string `xml:"xmlns:aegi,attr"`
	XMLNSxsi  string `xml:"xmlns:xsi,attr"`

	PublicationTime    string        `xml:"com:publicationTime"`
	PublicationCreator xmlCreator    `xml:"com:publicationCreator"`
	Tables             []xmlTable    `xml:"aegi:energyInfrastructureTable,omitempty"`
	SiteStatuses       []xmlSiteStat `xml:"aegi:energyInfrastructureSiteStatus,omitempty"`
}

type xmlCreator struct {
	Country            string `xml:"com:country"`
	NationalIdentifier string `xml:"com:nationalIdentifier"`
}

type xmlTable struct {
	ID      string    `xml:"id,attr"`
	Version string    `xml:"version,attr"`
	Sites   []xmlSite `xml:"aegi:energyInfrastructureSite"`
}

type xmlSite struct {
	ID       string       `xml:"id,attr"`
	Version  string       `xml:"version,attr"`
	Name     *xmlMLString `xml:"afac:name,omitempty"`
	Location *xmlLocation `xml:"afac:locationReference,omitempty"`
	Stations []xmlStation `xml:"aegi:energyInfrastructureStation"`
}

type xmlMLString struct {
	Values []xmlMLValue `xml:"com:values>com:value"`
}

type xmlMLValue struct {
	Lang string `xml:"lang,attr"`
	Text string `xml:",chardata"`
}

type xmlLocation struct {
	XsiType     string     `xml:"xsi:type,attr"`
	Coordinates xmlPointBy `xml:"loc:pointByCoordinates"`
}

type xmlPointBy struct {
	Point xmlPoint `xml:"loc:pointCoordinates"`
}

type xmlPoint struct {
	Latitude  float64 `xml:"loc:latitude"`
	Longitude float64 `xml:"loc:longitude"`
}

type xmlStation struct {
	ID           string           `xml:"id,attr"`
	Version      string           `xml:"version,attr"`
	RefillPoints []xmlRefillPoint `xml:"aegi:refillPoint"`
}

type xmlRefillPoint struct {
	XsiType       string            `xml:"xsi:type,attr"`
	ID            string            `xml:"id,attr"`
	Version       string            `xml:"version,attr"`
	DeliveryUnit  string            `xml:"aegi:deliveryUnit"`
	EnergyProduct *xmlEnergyProduct `xml:"aegi:energyProduct,omitempty"`
	CurrentType   string            `xml:"aegi:currentType"`
	Connector     xmlConnector      `xml:"aegi:connector"`
}

type xmlEnergyProduct struct {
	Rate xmlEnergyRate `xml:"aegi:energyRate"`
}

type xmlEnergyRate struct {
	ID          string           `xml:"id,attr"`
	RatePolicy  string           `xml:"aegi:ratePolicy"`
	LastUpdated string           `xml:"aegi:lastUpdated"`
	Currency    string           `xml:"aegi:applicableCurrency"`
	Prices      []xmlEnergyPrice `xml:"aegi:energyPrice"`
}

type xmlEnergyPrice struct {
	PriceGroupIndex int     `xml:"aegi:priceGroupIndex"`
	PriceType       string  `xml:"aegi:priceType"`
	Value           float64 `xml:"aegi:value"`
}

type xmlConnector struct {
	ConnectorType    string  `xml:"aegi:connectorType"`
	MaxPowerAtSocket float64 `xml:"aegi:maxPowerAtSocket"`
}

// ---- status marshaling structs ----

type xmlSiteStat struct {
	Reference xmlReference     `xml:"afac:reference"`
	Stations  []xmlStationStat `xml:"aegi:energyInfrastructureStationStatus"`
}

type xmlStationStat struct {
	Reference    xmlReference `xml:"afac:reference"`
	RefillPoints []xmlRPStat  `xml:"aegi:refillPointStatus"`
}

type xmlRPStat struct {
	XsiType   string       `xml:"xsi:type,attr"`
	Reference xmlReference `xml:"afac:reference"`
	Status    string       `xml:"aegi:status"`
}

type xmlReference struct {
	ID          string `xml:"id,attr"`
	Version     string `xml:"version,attr"`
	TargetClass string `xml:"targetClass,attr"`
}

func newReference(id string) xmlReference {
	return xmlReference{ID: id, Version: "1", TargetClass: "afac:FacilityObject"}
}

// ---- Public writers ----

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// WriteAFIRTable writes an EnergyInfrastructureTablePublication (static:
// locations, connectors, ad-hoc pricing) as DATEX II v3.7 XML.
func WriteAFIRTable(w io.Writer, sites []PublishSite, creator Creator, now time.Time) error {
	p := newPayload("aegi:EnergyInfrastructureTablePublication", creator, now)
	p.Tables = []xmlTable{{ID: "T1", Version: "1", Sites: make([]xmlSite, 0, len(sites))}}
	for _, s := range sites {
		p.Tables[0].Sites = append(p.Tables[0].Sites, buildXMLSite(s))
	}
	return encodeXML(w, p)
}

// WriteAFIRStatus writes an EnergyInfrastructureStatusPublication (live refill
// point status). All statuses are grouped under one synthetic site/station; the
// refillPoint reference id is the real join key back to the table.
func WriteAFIRStatus(w io.Writer, statuses []PublishStatus, creator Creator, now time.Time) error {
	p := newPayload("aegi:EnergyInfrastructureStatusPublication", creator, now)
	rps := make([]xmlRPStat, 0, len(statuses))
	for _, s := range statuses {
		rps = append(rps, xmlRPStat{
			XsiType:   "aegi:ElectricChargingPointStatus",
			Reference: newReference(s.RefillPointID),
			Status:    statusEnum(s.Status),
		})
	}
	p.SiteStatuses = []xmlSiteStat{{
		Reference: newReference("status-site"),
		Stations: []xmlStationStat{{
			Reference:    newReference("status-station"),
			RefillPoints: rps,
		}},
	}}
	return encodeXML(w, p)
}

func newPayload(xsiType string, creator Creator, now time.Time) *xmlPayload {
	return &xmlPayload{
		XMLName:          xml.Name{Local: "d2:payload"},
		XsiType:          xsiType,
		Lang:             "en",
		ModelBaseVersion: modelBaseVersion,
		XMLNSd2:          nsD2,
		XMLNScom:         nsCom,
		XMLNSafac:        nsAfac,
		XMLNSloc:         nsLoc,
		XMLNSaegi:        nsAegi,
		XMLNSxsi:         nsXsi,
		PublicationTime:  now.UTC().Format(time.RFC3339),
		PublicationCreator: xmlCreator{
			Country:            strings.ToLower(creator.Country),
			NationalIdentifier: creator.NationalIdentifier,
		},
	}
}

func buildXMLSite(s PublishSite) xmlSite {
	out := xmlSite{ID: s.ID, Version: "1"}
	if s.Name != "" {
		out.Name = &xmlMLString{Values: []xmlMLValue{{Lang: "en", Text: s.Name}}}
	}
	if s.Lat != 0 || s.Lon != 0 {
		out.Location = &xmlLocation{
			XsiType:     "loc:PointLocation",
			Coordinates: xmlPointBy{Point: xmlPoint{Latitude: s.Lat, Longitude: s.Lon}},
		}
	}
	for _, st := range s.Stations {
		xst := xmlStation{ID: st.ID, Version: "1"}
		for _, rp := range st.RefillPoints {
			xst.RefillPoints = append(xst.RefillPoints, buildXMLRefillPoint(rp))
		}
		out.Stations = append(out.Stations, xst)
	}
	return out
}

func buildXMLRefillPoint(rp PublishRefillPoint) xmlRefillPoint {
	out := xmlRefillPoint{
		XsiType:      "aegi:ElectricChargingPoint",
		ID:           rp.ID,
		Version:      "1",
		DeliveryUnit: "kWh",
		CurrentType:  currentTypeEnum(rp.CurrentType),
		Connector: xmlConnector{
			ConnectorType:    connectorTypeEnum(rp.ConnectorType),
			MaxPowerAtSocket: rp.PowerKW * 1000,
		},
	}
	if rp.Rate != nil {
		if rate, ok := buildXMLRate(rp.ID, *rp.Rate); ok {
			out.EnergyProduct = &xmlEnergyProduct{Rate: rate}
		}
	}
	return out
}

func buildXMLRate(rpID string, r PublishRate) (xmlEnergyRate, bool) {
	prices := make([]xmlEnergyPrice, 0, len(r.Prices))
	for _, p := range r.Prices {
		enum, v, ok := priceTypeEnum(p.Type, p.Value)
		if !ok {
			continue
		}
		prices = append(prices, xmlEnergyPrice{
			PriceGroupIndex: len(prices) + 1,
			PriceType:       enum,
			Value:           v,
		})
	}
	if len(prices) == 0 {
		return xmlEnergyRate{}, false
	}
	cur := r.Currency
	if cur == "" {
		cur = "EUR"
	}
	upd := r.LastUpdated
	if upd.IsZero() {
		upd = time.Now().UTC()
	}
	return xmlEnergyRate{
		ID:          "rate-" + rpID,
		RatePolicy:  "adHoc",
		LastUpdated: upd.UTC().Format(time.RFC3339),
		Currency:    cur,
		Prices:      prices,
	}, true
}

func encodeXML(w io.Writer, p *xmlPayload) error {
	if _, err := io.WriteString(w, xmlHeader); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(p); err != nil {
		return err
	}
	return enc.Flush()
}
