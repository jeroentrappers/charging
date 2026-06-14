package datex

// AFIR (Alternative Fuels Infrastructure Regulation) DATEX II v3 *JSON*
// encoding parser, profile "AFIR Energy Infrastructure" 01-00-00, as pushed by
// Germany's Mobilithek (e.g. GP JOULE CONNECT). This is the JSON encoding of
// the DATEX II v3 model and is entirely separate from the XML parser in
// afir.go — do not conflate the two.
//
// Two publication kinds share one MessageContainer envelope:
//   - the STATIC EnergyInfrastructureTablePublication (locations + ad-hoc tariffs)
//   - the DYNAMIC EnergyInfrastructureStatusPublication (status + price updates)
//
// A synthetic test push carries `payload` as an ARRAY of commonGenericPublication
// objects with no aegi* energy publication; that parses to Kind="" and is ignored.

import (
	"encoding/json"
	"math"

	"github.com/appmire/charging/internal/model"
)

// AFIRCreator identifies the publishing NAP participant.
type AFIRCreator struct{ Country, NationalIdentifier string }

// AFIRStatusUpdate is one charging-point status (and optional price) from the
// dynamic status publication.
type AFIRStatusUpdate struct {
	EVSEUID string        // the refill-point idG (join key), e.g. "cp-DE*CNT*EP90046*002*1-1"
	Status  string        // mapped to OUR vocab
	Tariff  *model.Tariff // from energyRateUpdate adHoc price; nil if none
}

// AFIRDoc is the parsed result of one MessageContainer.
type AFIRDoc struct {
	Kind       string // "table" | "status" | "" (unknown/test)
	Creator    AFIRCreator
	Operator   string                  // readable operator org name (table push only; "" on status/test)
	Connectors []model.Connector       // table only; CPOID left EMPTY (caller sets it)
	Tariffs    map[string]model.Tariff // table only; keyed by energyRate idG (== each connector's TariffID)
	Statuses   []AFIRStatusUpdate      // status only
}

// ---- Wire types (lenient; only the fields we consume) -------------------

// valuedG is the recurring {"value":"...","extendedValueG":"..."} enum wrapper.
type jafirValued struct {
	Value          string `json:"value"`
	ExtendedValueG string `json:"extendedValueG"`
}

// multilingual {"values":[{"lang":"en","value":"..."}]}.
type jafirML struct {
	Values []struct {
		Lang  string `json:"lang"`
		Value string `json:"value"`
	} `json:"values"`
}

func (m jafirML) first() string {
	for _, v := range m.Values {
		if v.Value != "" {
			return v.Value
		}
	}
	return ""
}

type jafirCreatorWire struct {
	Country            string `json:"country"`
	NationalIdentifier string `json:"nationalIdentifier"`
}

type jafirEnergyPrice struct {
	PriceType   jafirValued `json:"priceType"`
	Value       float64     `json:"value"`
	TaxIncluded bool        `json:"taxIncluded"`
	TaxRate     float64     `json:"taxRate"`
}

type jafirEnergyRate struct {
	IDG                string             `json:"idG"`
	RatePolicy         jafirValued        `json:"ratePolicy"`
	ApplicableCurrency []string           `json:"applicableCurrency"`
	EnergyPrice        []jafirEnergyPrice `json:"energyPrice"`
}

type jafirElectricEnergy struct {
	EnergyRate []jafirEnergyRate `json:"energyRate"`
}

type jafirConnector struct {
	ConnectorType    jafirValued `json:"connectorType"`
	MaxPowerAtSocket float64     `json:"maxPowerAtSocket"`
	ConnectorFormat  jafirValued `json:"connectorFormat"`
	Voltage          float64     `json:"voltage"`
	MaximumCurrent   float64     `json:"maximumCurrent"`
}

type jafirChargingPoint struct {
	IDG                string `json:"idG"`
	VersionG           string `json:"versionG"`
	ExternalIdentifier []struct {
		Identifier       string      `json:"identifier"`
		TypeOfIdentifier jafirValued `json:"typeOfIdentifier"`
	} `json:"externalIdentifier"`
	DeliveryUnit   jafirValued           `json:"deliveryUnit"`
	CurrentType    jafirValued           `json:"currentType"`
	Connector      []jafirConnector      `json:"connector"`
	ElectricEnergy []jafirElectricEnergy `json:"electricEnergy"`
}

type jafirRefillPoint struct {
	ChargingPoint jafirChargingPoint `json:"aegiElectricChargingPoint"`
}

type jafirStation struct {
	IDG                  string             `json:"idG"`
	TotalMaximumPower    float64            `json:"totalMaximumPower"`
	NumberOfRefillPoints int                `json:"numberOfRefillPoints"`
	LocationReference    jafirLocRef        `json:"locationReference"`
	Operator             jafirOperator      `json:"operator"`
	RefillPoint          []jafirRefillPoint `json:"refillPoint"`
}

// jafirLocRef (coordinates + address) and jafirOperator may appear at the site
// OR the station level depending on the publisher — e.g. GP JOULE puts them on
// the site, Grid and Co. on the station. The builder prefers station, falls
// back to site.
type jafirLocRef struct {
	LocAreaLocation struct {
		CoordinatesForDisplay struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"coordinatesForDisplay"`
		LocLocationExtensionG struct {
			FacilityLocation struct {
				Address jafirAddress `json:"address"`
			} `json:"FacilityLocation"`
		} `json:"locLocationExtensionG"`
	} `json:"locAreaLocation"`
}

func (l jafirLocRef) hasCoords() bool {
	c := l.LocAreaLocation.CoordinatesForDisplay
	return c.Latitude != 0 && c.Longitude != 0
}

type jafirOperator struct {
	Organisation struct {
		Name jafirML `json:"name"`
	} `json:"afacAnOrganisation"`
}

func (o jafirOperator) name() string { return o.Organisation.Name.first() }

type jafirAddressLine struct {
	Order int         `json:"order"`
	Type  jafirValued `json:"type"`
	Text  jafirML     `json:"text"`
}

type jafirAddress struct {
	Postcode    string             `json:"postcode"`
	City        jafirML            `json:"city"`
	CountryCode string             `json:"countryCode"`
	AddressLine []jafirAddressLine `json:"addressLine"`
}

type jafirSite struct {
	IDG               string         `json:"idG"`
	TypeOfSite        jafirValued    `json:"typeOfSite"`
	LocationReference jafirLocRef    `json:"locationReference"`
	Operator          jafirOperator  `json:"operator"`
	Station           []jafirStation `json:"energyInfrastructureStation"`
}

type jafirTable struct {
	IDG       string      `json:"idG"`
	TableName string      `json:"tableName"`
	Site      []jafirSite `json:"energyInfrastructureSite"`
}

type jafirTablePublication struct {
	Lang               string           `json:"lang"`
	PublicationTime    string           `json:"publicationTime"`
	PublicationCreator jafirCreatorWire `json:"publicationCreator"`
	Table              []jafirTable     `json:"energyInfrastructureTable"`
}

// ---- status wire types ----

type jafirChargingPointStatus struct {
	Reference struct {
		TargetClass string `json:"targetClass"`
		IDG         string `json:"idG"`
	} `json:"reference"`
	Status           jafirValued `json:"status"`
	EnergyRateUpdate []struct {
		EnergyRateReference struct {
			IDG string `json:"idG"`
		} `json:"energyRateReference"`
		ApplicableCurrency []string           `json:"applicableCurrency"`
		RatePolicy         jafirValued        `json:"ratePolicy"`
		EnergyPrice        []jafirEnergyPrice `json:"energyPrice"`
	} `json:"energyRateUpdate"`
}

type jafirRefillPointStatus struct {
	ChargingPointStatus jafirChargingPointStatus `json:"aegiElectricChargingPointStatus"`
}

type jafirStationStatus struct {
	Reference struct {
		IDG string `json:"idG"`
	} `json:"reference"`
	RefillPointStatus []jafirRefillPointStatus `json:"refillPointStatus"`
}

type jafirSiteStatus struct {
	LastUpdated string `json:"lastUpdated"`
	Reference   struct {
		TargetClass string `json:"targetClass"`
		IDG         string `json:"idG"`
	} `json:"reference"`
	StationStatus []jafirStationStatus `json:"energyInfrastructureStationStatus"`
}

type jafirStatusPublication struct {
	Lang               string            `json:"lang"`
	PublicationTime    string            `json:"publicationTime"`
	PublicationCreator jafirCreatorWire  `json:"publicationCreator"`
	SiteStatus         []jafirSiteStatus `json:"energyInfrastructureSiteStatus"`
}

// payload object (the real AFIR shape). When payload is an array (synthetic
// test) these stay zero.
type jafirPayload struct {
	TablePublication  *jafirTablePublication  `json:"aegiEnergyInfrastructureTablePublication"`
	StatusPublication *jafirStatusPublication `json:"aegiEnergyInfrastructureStatusPublication"`
}

type jafirContainer struct {
	Payload json.RawMessage `json:"payload"`
}

// ParseAFIRJSON decodes one Mobilithek AFIR JSON MessageContainer. payload may be
// an object (real AFIR) or an array (synthetic test → Kind=""). Never panics on
// missing fields.
func ParseAFIRJSON(data []byte) (*AFIRDoc, error) {
	var c jafirContainer
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	doc := &AFIRDoc{Tariffs: map[string]model.Tariff{}}
	if len(c.Payload) == 0 {
		return doc, nil
	}
	// payload as array → synthetic test publication; ignore.
	if c.Payload[0] == '[' {
		return doc, nil
	}

	var p jafirPayload
	if err := json.Unmarshal(c.Payload, &p); err != nil {
		return nil, err
	}

	switch {
	case p.TablePublication != nil:
		jafirBuildTable(doc, p.TablePublication)
	case p.StatusPublication != nil:
		jafirBuildStatus(doc, p.StatusPublication)
	}
	return doc, nil
}

func jafirBuildTable(doc *AFIRDoc, pub *jafirTablePublication) {
	doc.Kind = "table"
	doc.Creator = AFIRCreator{Country: pub.PublicationCreator.Country, NationalIdentifier: pub.PublicationCreator.NationalIdentifier}

	for _, tbl := range pub.Table {
		for _, site := range tbl.Site {
			for _, st := range site.Station {
				// Location/operator/address may sit on the station (Grid and Co.)
				// or the site (GP JOULE) — prefer the station, fall back to site.
				loc := st.LocationReference
				if !loc.hasCoords() {
					loc = site.LocationReference
				}
				coords := loc.LocAreaLocation.CoordinatesForDisplay
				if coords.Latitude == 0 || coords.Longitude == 0 {
					continue // no usable coordinates at either level
				}
				addr := loc.LocAreaLocation.LocLocationExtensionG.FacilityLocation.Address
				street := jafirStreetLine(addr)
				operator := st.Operator.name()
				if operator == "" {
					operator = site.Operator.name()
				}
				if doc.Operator == "" && operator != "" {
					doc.Operator = operator // readable CPO name for attribution
				}
				name := jafirBuildName(operator, street, tbl.TableName, doc.Creator.NationalIdentifier)
				city := addr.City.first()

				for _, rp := range st.RefillPoint {
					cp := rp.ChargingPoint
					if cp.IDG == "" {
						continue
					}
					tariffID := jafirBuildTariff(doc, cp.ElectricEnergy)

					ct := model.CurrentAC
					if cp.CurrentType.Value == "dc" {
						ct = model.CurrentDC
					}

					for i, conn := range cp.Connector {
						powerW := conn.MaxPowerAtSocket
						if powerW == 0 {
							powerW = st.TotalMaximumPower
						}
						doc.Connectors = append(doc.Connectors, model.Connector{
							EVSEUID:     cp.IDG,
							ConnectorID: jafirItoa(i + 1),
							Lat:         coords.Latitude,
							Lon:         coords.Longitude,
							PowerKW:     jafirRound1(powerW / 1000),
							PlugType:    model.NormalizePlug(conn.ConnectorType.Value),
							CurrentType: ct,
							Name:        name,
							Address:     street,
							PostalCode:  addr.Postcode,
							City:        city,
							EVSEStatus:  "",
							TariffID:    tariffID,
						})
					}
				}
			}
		}
	}
}

func jafirBuildStatus(doc *AFIRDoc, pub *jafirStatusPublication) {
	doc.Kind = "status"
	doc.Creator = AFIRCreator{Country: pub.PublicationCreator.Country, NationalIdentifier: pub.PublicationCreator.NationalIdentifier}

	for _, site := range pub.SiteStatus {
		for _, st := range site.StationStatus {
			for _, rp := range st.RefillPointStatus {
				cps := rp.ChargingPointStatus
				upd := AFIRStatusUpdate{
					EVSEUID: cps.Reference.IDG,
					Status:  jafirMapStatus(cps.Status.Value),
				}
				// Build a tariff from energyRateUpdate (pick adHoc, else first).
				if len(cps.EnergyRateUpdate) > 0 {
					sel := 0
					for i, er := range cps.EnergyRateUpdate {
						if er.RatePolicy.Value == "adHoc" {
							sel = i
							break
						}
					}
					er := cps.EnergyRateUpdate[sel]
					comps := jafirPriceComponents(er.EnergyPrice)
					if len(comps) > 0 {
						currency := "EUR"
						if len(er.ApplicableCurrency) > 0 && er.ApplicableCurrency[0] != "" {
							currency = er.ApplicableCurrency[0]
						}
						upd.Tariff = &model.Tariff{
							OCPIID:   er.EnergyRateReference.IDG,
							Currency: currency,
							Elements: []model.TariffElement{{PriceComponents: comps}},
						}
					}
				}
				doc.Statuses = append(doc.Statuses, upd)
			}
		}
	}
}

// jafirBuildTariff selects the ad-hoc (else first) energyRate from a refill point,
// records the corresponding model.Tariff in doc.Tariffs keyed by its idG, and
// returns that id. Returns "" if there's no usable rate.
func jafirBuildTariff(doc *AFIRDoc, ee []jafirElectricEnergy) string {
	var rates []jafirEnergyRate
	for _, e := range ee {
		rates = append(rates, e.EnergyRate...)
	}
	if len(rates) == 0 {
		return ""
	}
	sel := rates[0]
	for _, r := range rates {
		if r.RatePolicy.Value == "adHoc" {
			sel = r
			break
		}
	}
	if sel.IDG == "" {
		return ""
	}
	comps := jafirPriceComponents(sel.EnergyPrice)
	currency := "EUR"
	if len(sel.ApplicableCurrency) > 0 && sel.ApplicableCurrency[0] != "" {
		currency = sel.ApplicableCurrency[0]
	}
	doc.Tariffs[sel.IDG] = model.Tariff{
		OCPIID:   sel.IDG,
		Currency: currency,
		Elements: []model.TariffElement{{PriceComponents: comps}},
	}
	return sel.IDG
}

// jafirPriceComponents maps AFIR energyPrice entries to our price components.
func jafirPriceComponents(prices []jafirEnergyPrice) []model.PriceComponent {
	var out []model.PriceComponent
	for _, ep := range prices {
		switch ep.PriceType.Value {
		case "pricePerKWh":
			out = append(out, model.PriceComponent{Type: "ENERGY", Price: ep.Value})
		case "pricePerMinute":
			// Our TIME component is €/hour.
			out = append(out, model.PriceComponent{Type: "TIME", Price: ep.Value * 60})
		case "flatRate", "basePrice":
			out = append(out, model.PriceComponent{Type: "FLAT", Price: ep.Value})
		case "free":
			out = append(out, model.PriceComponent{Type: "ENERGY", Price: 0})
		default:
			// unknown price type → skip
		}
	}
	return out
}

func jafirMapStatus(v string) string {
	switch v {
	case "available":
		return "AVAILABLE"
	case "charging", "occupied", "reserved":
		return "CHARGING"
	case "blocked", "outOfOrder", "faulted", "inoperative", "unavailable", "outOfStock", "removed":
		return "OUTOFORDER"
	case "planned", "unknown", "":
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// jafirStreetLine returns the first address line whose type is "street".
func jafirStreetLine(addr jafirAddress) string {
	for _, al := range addr.AddressLine {
		if al.Type.Value == "street" {
			if s := al.Text.first(); s != "" {
				return s
			}
		}
	}
	// fall back to any non-empty address line
	for _, al := range addr.AddressLine {
		if s := al.Text.first(); s != "" {
			return s
		}
	}
	return ""
}

// jafirBuildName composes "<operator> · <locality>" with sensible fallbacks.
func jafirBuildName(operator, street, tableName, napID string) string {
	left := operator
	if left == "" {
		left = napID
	}
	right := jafirFirstNonEmpty(street, tableName)
	switch {
	case left != "" && right != "":
		return left + " · " + right
	case left != "":
		return left
	default:
		return right
	}
}

func jafirFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func jafirRound1(v float64) float64 { return math.Round(v*10) / 10 }

// jafirItoa converts a small non-negative int to its decimal string without fmt.
func jafirItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
