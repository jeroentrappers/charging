package datex

// AFIR (Alternative Fuels Infrastructure Regulation) DATEX II v3 Recharging
// profile parser. Unlike the locations-only profile in datex.go, the German
// AFIR feed carries ad-hoc pricing (EnergyRate / EnergyPrice) on the static
// EnergyInfrastructureTablePublication and live status + price updates on a
// separate EnergyInfrastructureStatusPublication.
//
// Matching is by local element name (namespace-agnostic), so the parser works
// regardless of which xmlns prefixes a publisher uses.

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/appmire/charging/internal/model"
)

// ---- Static publication (EnergyInfrastructureTablePublication) ----

type afirStaticPub struct {
	Sites []afirSite `xml:"payload>energyInfrastructureTable>energyInfrastructureSite"`
}

type afirSite struct {
	Name       string        `xml:"name>values>value"`
	Latitude   float64       `xml:"locationReference>pointByCoordinates>pointCoordinates>latitude"`
	Longitude  float64       `xml:"locationReference>pointByCoordinates>pointCoordinates>longitude"`
	PostalCode string        `xml:"locationReference>_pointLocationExtension>facilityLocation>address>postcode"`
	City       string        `xml:"locationReference>_pointLocationExtension>facilityLocation>address>city"`
	Operator   string        `xml:"operator>name>values>value"`
	Rates      []afirRate    `xml:"energyRate"`
	Stations   []afirStation `xml:"energyInfrastructureStation"`
}

type afirStation struct {
	Rates        []afirRate        `xml:"energyRate"`
	RefillPoints []afirRefillPoint `xml:"refillPoint"`
}

type afirRefillPoint struct {
	ID            string     `xml:"id,attr"`
	ConnectorType string     `xml:"connector>connectorType"`
	ChargingMode  string     `xml:"connector>chargingMode"`
	MaxPowerW     float64    `xml:"connector>maxPowerAtSocket"`
	Rates         []afirRate `xml:"energyRate"`
}

// afirRate is an EnergyRate: a currency + a policy (ad-hoc vs contract) + a set
// of EnergyPrice components.
type afirRate struct {
	RatePolicyAttr string      `xml:"ratePolicy,attr"`
	RatePolicyElem string      `xml:"ratePolicy"`
	Currency       string      `xml:"applicableCurrency"`
	Prices         []afirPrice `xml:"energyPrice"`
}

func (r afirRate) policy() string {
	if r.RatePolicyAttr != "" {
		return r.RatePolicyAttr
	}
	return r.RatePolicyElem
}

func (r afirRate) isAdHoc() bool {
	return strings.Contains(strings.ToLower(r.policy()), "hoc")
}

// afirPrice is an EnergyPrice element.
type afirPrice struct {
	PriceType   string  `xml:"priceType"`
	Value       float64 `xml:"value"`
	TaxIncluded bool    `xml:"taxIncluded"`
	TaxRate     float64 `xml:"taxRate"`
}

// ---- Status publication (EnergyInfrastructureStatusPublication) ----

type afirStatusPub struct {
	Stations []afirStationStatus `xml:"payload>energyInfrastructureStatus>energyInfrastructureStationStatus"`
}

type afirStationStatus struct {
	RefillPoints []afirRefillPointStatus `xml:"refillPointStatus"`
}

type afirRefillPointStatus struct {
	Reference   afirReference `xml:"reference"`
	Status      string        `xml:"status"`
	UpdateRates []afirPrice   `xml:"energyRateUpdate>energyPrice"`
	UpdateCurr  string        `xml:"energyRateUpdate>applicableCurrency"`
}

// afirReference is a DATEX II reference back to a static element. The id is
// usually an attribute, but may instead be carried as element text.
type afirReference struct {
	ID          string `xml:"id,attr"`
	TargetClass string `xml:"targetClass,attr"`
	Text        string `xml:",chardata"`
}

func (s afirRefillPointStatus) refID() string {
	if s.Reference.ID != "" {
		return s.Reference.ID
	}
	return strings.TrimSpace(s.Reference.Text)
}

// ---- Mapping tables ----

// connectorTypePlug maps DATEX II ConnectorTypeEnum values to canonical OCPI
// connector standards. Unknown values pass through uppercased.
var connectorTypePlug = map[string]string{
	"iec62196t2":      "IEC_62196_T2",
	"iec62196t2combo": "IEC_62196_T2_COMBO",
	"chademo":         "CHADEMO",
	"iec62196t1":      "IEC_62196_T1",
	"domesticf":       "DOMESTIC_F",
}

func mapPlug(connectorType string) string {
	if connectorType == "" {
		return ""
	}
	if v, ok := connectorTypePlug[strings.ToLower(connectorType)]; ok {
		return v
	}
	return strings.ToUpper(connectorType)
}

// afirCurrentType decides AC vs DC from connector type and charging mode.
func afirCurrentType(connectorType, chargingMode string) string {
	ct := strings.ToLower(connectorType)
	if strings.Contains(ct, "combo") || strings.Contains(ct, "chademo") {
		return model.CurrentDC
	}
	return currentType(chargingMode) // reuse datex.go's mode logic
}

// statusVocab maps a RefillPointStatusEnum value to our EVSE status vocabulary.
func statusVocab(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "available":
		return "AVAILABLE"
	case "occupied", "charging", "reserved", "blocked":
		return "CHARGING"
	case "outoforder", "faulted", "inoperative", "outofstock", "unavailable":
		return "OUTOFORDER"
	default: // planned, removed, unknown, ""
		return "UNKNOWN"
	}
}

// priceComponents converts EnergyPrice elements into our PriceComponents.
func priceComponents(prices []afirPrice) []model.PriceComponent {
	var comps []model.PriceComponent
	for _, p := range prices {
		switch strings.ToLower(p.PriceType) {
		case "priceperkwh":
			comps = append(comps, model.PriceComponent{Type: "ENERGY", Price: p.Value})
		case "priceperminute":
			// DATEX is €/min; our TIME component is €/hour.
			comps = append(comps, model.PriceComponent{Type: "TIME", Price: round1(p.Value * 60)})
		case "flatrate", "baseprice":
			comps = append(comps, model.PriceComponent{Type: "FLAT", Price: p.Value})
		case "free":
			comps = append(comps, model.PriceComponent{Type: "ENERGY", Price: 0})
		default: // other -> skip
		}
	}
	return comps
}

// pickRate selects the preferred EnergyRate from a refillPoint (or its station
// / site). It prefers an ad-hoc rate; otherwise it falls back to the first one.
func pickRate(rates []afirRate) (afirRate, bool) {
	if len(rates) == 0 {
		return afirRate{}, false
	}
	for _, r := range rates {
		if r.isAdHoc() {
			return r, true
		}
	}
	return rates[0], true
}

// buildTariff builds a single-element Tariff from a rate. Returns false when the
// rate yields no usable price components.
func buildTariff(ocpiID string, r afirRate) (model.Tariff, bool) {
	comps := priceComponents(r.Prices)
	if len(comps) == 0 {
		return model.Tariff{}, false
	}
	cur := r.Currency
	if cur == "" {
		cur = "EUR"
	}
	return model.Tariff{
		OCPIID:   ocpiID,
		Currency: cur,
		Elements: []model.TariffElement{{PriceComponents: comps}},
	}, true
}

// ParseAFIRStatic parses an EnergyInfrastructureTablePublication into connectors
// + ad-hoc tariffs. Each connector's TariffID is set to a stable key (use the
// refillPoint id) and that key exists in the returned tariff map when the
// refillPoint had an ad-hoc EnergyRate. ConnectorID is "1".
func ParseAFIRStatic(cpoID string, data []byte) ([]model.Connector, map[string]model.Tariff, error) {
	var pub afirStaticPub
	if err := xml.Unmarshal(data, &pub); err != nil {
		return nil, nil, fmt.Errorf("decode afir static: %w", err)
	}
	tariffs := map[string]model.Tariff{}
	var conns []model.Connector

	for _, s := range pub.Sites {
		for _, st := range s.Stations {
			for _, rp := range st.RefillPoints {
				conn := model.Connector{
					CPOID:       cpoID,
					EVSEUID:     rp.ID,
					ConnectorID: "1",
					Lat:         s.Latitude,
					Lon:         s.Longitude,
					PowerKW:     round1(rp.MaxPowerW / 1000),
					PlugType:    mapPlug(rp.ConnectorType),
					CurrentType: afirCurrentType(rp.ConnectorType, rp.ChargingMode),
					Name:        name(site{Name: s.Name, Operator: s.Operator}),
					Address:     address(site{PostalCode: s.PostalCode, City: s.City}),
					PostalCode:  s.PostalCode,
					City:        s.City,
				}

				// Pricing may sit on the refillPoint, its station, or its site.
				rates := rp.Rates
				if len(rates) == 0 {
					rates = st.Rates
				}
				if len(rates) == 0 {
					rates = s.Rates
				}
				if r, ok := pickRate(rates); ok {
					if t, ok := buildTariff(rp.ID, r); ok {
						tariffs[rp.ID] = t
						conn.TariffID = rp.ID
					}
				}
				conns = append(conns, conn)
			}
		}
	}
	return conns, tariffs, nil
}

// AFIRStatus is one refill point's live state.
type AFIRStatus struct {
	Status string        // mapped to our EVSE status vocabulary
	Tariff *model.Tariff // non-nil if a live price update was present
}

// ParseAFIRStatus parses an EnergyInfrastructureStatusPublication into a map
// keyed by refillPoint id (the reference id).
func ParseAFIRStatus(data []byte) (map[string]AFIRStatus, error) {
	var pub afirStatusPub
	if err := xml.Unmarshal(data, &pub); err != nil {
		return nil, fmt.Errorf("decode afir status: %w", err)
	}
	out := map[string]AFIRStatus{}
	for _, st := range pub.Stations {
		for _, rps := range st.RefillPoints {
			id := rps.refID()
			if id == "" {
				continue
			}
			as := AFIRStatus{Status: statusVocab(rps.Status)}
			if len(rps.UpdateRates) > 0 {
				if t, ok := buildTariff(id, afirRate{Currency: rps.UpdateCurr, Prices: rps.UpdateRates}); ok {
					as.Tariff = &t
				}
			}
			out[id] = as
		}
	}
	return out, nil
}
