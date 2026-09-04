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
//
// Pull feeds may skip the envelope altogether and put the publication(s) at the
// document root, and may carry BOTH kinds in one document (Eco-Movement's
// Belgian NAP feed pages locations and their live status together) — both are
// parsed, and Kind then reports the table.

import (
	"bytes"
	"encoding/json"
	"math"
	"time"

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
	// LastUpdated is when the publisher last changed this point's status. Zero
	// when absent. A record the publisher has stopped touching is a strong hint
	// that it is a leftover: Eco-Movement publishes some EVSEs twice, and the
	// decommissioned copy's timestamp stands still while its live twin moves.
	LastUpdated time.Time
	// PriceWithdrawn distinguishes the two reasons Tariff can be nil: the
	// publisher sent a price update that prices NOTHING (a €0 flat fee standing
	// in for a missing tariff), rather than sending no price update at all. On a
	// push/delta feed that difference is everything — the first is evidence the
	// point has no usable price, the second is no evidence either way.
	PriceWithdrawn bool
}

// AFIRDoc is the parsed result of one MessageContainer.
type AFIRDoc struct {
	Kind       string // "table" | "status" | "" (unknown/test)
	Creator    AFIRCreator
	Operator   string                  // readable operator org name (table push only; "" on status/test)
	Connectors []model.Connector       // table only; CPOID left EMPTY (caller sets it)
	Tariffs    map[string]model.Tariff // table only; keyed by energyRate idG (== each connector's TariffID)
	Statuses   []AFIRStatusUpdate      // status only
	// EVSEIDs maps a refill-point idG to the roaming (eMI3) EVSE id published
	// on its externalIdentifier, when there is one. Callers whose publisher
	// keys refill points by an internal id (Eco-Movement) can re-key connectors
	// onto the stable roaming id and still join Statuses, which reference idG.
	EVSEIDs map[string]string
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

// all returns every non-empty language value (used to scan for the free-text
// per-minute grace note, which may be in any language).
func (m jafirML) all() []string {
	out := make([]string, 0, len(m.Values))
	for _, v := range m.Values {
		if v.Value != "" {
			out = append(out, v.Value)
		}
	}
	return out
}

type jafirCreatorWire struct {
	Country            string `json:"country"`
	NationalIdentifier string `json:"nationalIdentifier"`
}

type jafirEnergyPrice struct {
	PriceType             jafirValued `json:"priceType"`
	Value                 float64     `json:"value"`
	TaxIncluded           bool        `json:"taxIncluded"`
	TaxRate               float64     `json:"taxRate"`
	AdditionalInformation jafirML     `json:"additionalInformation"`
	// TimeBasedApplicability carries the AFIR structured grace threshold: the
	// price applies only past that many minutes into the session. Eco-Movement's
	// Belgian NAP feed uses it for idle/blocking fees.
	TimeBasedApplicability struct {
		FromMinute int `json:"fromMinute"`
	} `json:"timeBasedApplicability"`
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
	ConnectorType      jafirValued       `json:"connectorType"`
	ExternalIdentifier []jafirExternalID `json:"externalIdentifier"`
	MaxPowerAtSocket   float64           `json:"maxPowerAtSocket"`
	ConnectorFormat    jafirValued       `json:"connectorFormat"`
	Voltage            float64           `json:"voltage"`
	MaximumCurrent     float64           `json:"maximumCurrent"`
}

// jafirExternalID is a publisher-supplied identifier alongside the DATEX idG —
// the roaming (eMI3) EVSE id when typeOfIdentifier says "evseId".
type jafirExternalID struct {
	Identifier       string      `json:"identifier"`
	TypeOfIdentifier jafirValued `json:"typeOfIdentifier"`
}

func (e jafirExternalID) isEVSEID() bool {
	return e.TypeOfIdentifier.ExtendedValueG == "evseId" || e.TypeOfIdentifier.Value == "evseId"
}

type jafirChargingPoint struct {
	IDG                string                `json:"idG"`
	VersionG           string                `json:"versionG"`
	ExternalIdentifier []jafirExternalID     `json:"externalIdentifier"`
	DeliveryUnit       jafirValued           `json:"deliveryUnit"`
	CurrentType        jafirValued           `json:"currentType"`
	Connector          []jafirConnector      `json:"connector"`
	ElectricEnergy     []jafirElectricEnergy `json:"electricEnergy"`
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
	EnergyDistributor    jafirOperator      `json:"energyDistributor"`
	RefillPoint          []jafirRefillPoint `json:"refillPoint"`
}

// operatorName is the readable network name: the AFIR operator when published,
// else the energyDistributor Eco-Movement uses to carry the CPO.
func (st jafirStation) operatorName() string {
	return jafirFirstNonEmpty(st.Operator.name(), st.EnergyDistributor.name())
}

// jafirLocRef (coordinates + address) and jafirOperator may appear at the site
// OR the station level depending on the publisher — e.g. GP JOULE puts them on
// the site, Grid and Co. on the station. The builder prefers station, falls
// back to site.
type jafirCoords struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// jafirLocBody is one location wrapper. AFIR uses two interchangeably
// (locAreaLocation, locPointLocation) and two coordinate forms
// (coordinatesForDisplay, pointByCoordinates>pointCoordinates) — publishers mix
// and match, so we accept all combinations.
type jafirLocBody struct {
	CoordinatesForDisplay jafirCoords `json:"coordinatesForDisplay"`
	PointByCoordinates    struct {
		PointCoordinates jafirCoords `json:"pointCoordinates"`
	} `json:"pointByCoordinates"`
	LocLocationExtensionG struct {
		FacilityLocation struct {
			Address jafirAddress `json:"address"`
		} `json:"FacilityLocation"`
	} `json:"locLocationExtensionG"`
}

type jafirLocRef struct {
	LocAreaLocation  jafirLocBody `json:"locAreaLocation"`
	LocPointLocation jafirLocBody `json:"locPointLocation"`
}

func (l jafirLocRef) coords() jafirCoords {
	for _, b := range []jafirLocBody{l.LocAreaLocation, l.LocPointLocation} {
		if b.CoordinatesForDisplay.Latitude != 0 && b.CoordinatesForDisplay.Longitude != 0 {
			return b.CoordinatesForDisplay
		}
		if p := b.PointByCoordinates.PointCoordinates; p.Latitude != 0 && p.Longitude != 0 {
			return p
		}
	}
	return jafirCoords{}
}

func (l jafirLocRef) hasCoords() bool {
	c := l.coords()
	return c.Latitude != 0 && c.Longitude != 0
}

func (l jafirLocRef) address() jafirAddress {
	a := l.LocAreaLocation.LocLocationExtensionG.FacilityLocation.Address
	if a.Postcode == "" && a.City.first() == "" && len(a.AddressLine) == 0 {
		a = l.LocPointLocation.LocLocationExtensionG.FacilityLocation.Address
	}
	return a
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
	Entrance          []jafirLocRef  `json:"entrance"`
	Operator          jafirOperator  `json:"operator"`
	EnergyDistributor jafirOperator  `json:"energyDistributor"`
	Station           []jafirStation `json:"energyInfrastructureStation"`
}

// location returns the site's coordinates + address: the AFIR locationReference
// when it carries them, else the first entrance that does (Eco-Movement puts the
// whole FacilityLocation on the site entrance).
func (s jafirSite) location() jafirLocRef {
	if s.LocationReference.hasCoords() {
		return s.LocationReference
	}
	for _, e := range s.Entrance {
		if e.hasCoords() {
			return e
		}
	}
	return s.LocationReference
}

func (s jafirSite) operatorName() string {
	return jafirFirstNonEmpty(s.Operator.name(), s.EnergyDistributor.name())
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
	LastUpdated      string      `json:"lastUpdated"`
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

// jafirContainer locates the payload regardless of envelope: publishers send
// either {"payload":…} (GP JOULE, Grid) or {"messageContainer":{"payload":…}}
// (EnBW, Tesla, SMATRICS, …). The payload itself may be a single object or an
// ARRAY of publication objects.
type jafirContainer struct {
	Payload          json.RawMessage `json:"payload"`
	MessageContainer *struct {
		Payload json.RawMessage `json:"payload"`
	} `json:"messageContainer"`
}

// ParseAFIRJSON decodes one Mobilithek AFIR JSON message. Handles both envelope
// shapes and an object- or array-valued payload. A payload carrying no known
// aegi* publication (e.g. the synthetic test push) yields Kind="". Never panics.
func ParseAFIRJSON(data []byte) (*AFIRDoc, error) {
	var c jafirContainer
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	doc := &AFIRDoc{Tariffs: map[string]model.Tariff{}, EVSEIDs: map[string]string{}}

	raw := c.Payload
	if len(bytes.TrimSpace(raw)) == 0 && c.MessageContainer != nil {
		raw = c.MessageContainer.Payload
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		// No envelope: pull feeds (Eco-Movement) put the publications at the
		// document root. A root object carrying neither still yields Kind="".
		raw = bytes.TrimSpace(data)
	}
	if len(raw) == 0 {
		return doc, nil
	}

	// payload may be one object or an array of publication objects.
	var elems []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &elems); err != nil {
			return nil, err
		}
	} else {
		elems = []json.RawMessage{raw}
	}

	for _, e := range elems {
		var p jafirPayload
		if err := json.Unmarshal(e, &p); err != nil {
			continue // skip a malformed element, keep the rest
		}
		// A document may carry both publications; build each. Table first, so
		// Kind reports "table" for a combined document.
		if p.TablePublication != nil {
			jafirBuildTable(doc, p.TablePublication)
		}
		if p.StatusPublication != nil {
			jafirBuildStatus(doc, p.StatusPublication)
		}
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
					loc = site.location()
				}
				coords := loc.coords()
				if coords.Latitude == 0 || coords.Longitude == 0 {
					continue // no usable coordinates at either level
				}
				addr := loc.address()
				street := jafirStreetLine(addr)
				operator := st.operatorName()
				if operator == "" {
					operator = site.operatorName()
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
					if id := jafirEVSEID(cp); id != "" {
						doc.EVSEIDs[cp.IDG] = id
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
	if doc.Kind == "" { // a combined document is reported as its table
		doc.Kind = "status"
	}
	if (doc.Creator == AFIRCreator{}) {
		doc.Creator = AFIRCreator{Country: pub.PublicationCreator.Country, NationalIdentifier: pub.PublicationCreator.NationalIdentifier}
	}

	for _, site := range pub.SiteStatus {
		for _, st := range site.StationStatus {
			for _, rp := range st.RefillPointStatus {
				cps := rp.ChargingPointStatus
				upd := AFIRStatusUpdate{
					EVSEUID: cps.Reference.IDG,
					Status:  jafirMapStatus(cps.Status.Value),
				}
				if t, err := time.Parse(time.RFC3339, cps.LastUpdated); err == nil {
					upd.LastUpdated = t
				}
				// Build a tariff from energyRateUpdate (pick adHoc, else first).
				if len(cps.EnergyRateUpdate) > 0 {
					upd.PriceWithdrawn = true // until a usable price is found below
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
						upd.PriceWithdrawn = false
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
	if len(comps) == 0 {
		return "" // nothing priceable (e.g. a €0 flat fee standing in for a missing tariff)
	}
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

// jafirPriceComponents maps AFIR energyPrice entries to our price components,
// as the driver-facing (tax-inclusive) price.
func jafirPriceComponents(prices []jafirEnergyPrice) []model.PriceComponent {
	var out []model.PriceComponent
	for _, ep := range prices {
		v := jafirGrossPrice(ep)
		switch ep.PriceType.Value {
		case "pricePerKWh":
			out = append(out, model.PriceComponent{Type: "ENERGY", Price: v})
		case "pricePerMinute":
			// Our TIME component is €/hour.
			out = append(out, model.PriceComponent{Type: "TIME", Price: jafirRound4(v * 60),
				AfterMinutes: graceThreshold(ep.TimeBasedApplicability.FromMinute, ep.AdditionalInformation.all()...)})
		case "flatRate", "basePrice":
			out = append(out, model.PriceComponent{Type: "FLAT", Price: v})
		case "free":
			out = append(out, model.PriceComponent{Type: "ENERGY", Price: 0})
		default:
			// unknown price type → skip
		}
	}
	return usableComponents(dedupeComponents(out))
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

// jafirEVSEID returns the refill point's roaming (eMI3) EVSE id, from the
// charging point's own externalIdentifier or, failing that, its first
// connector's. Empty when the publisher carries none.
func jafirEVSEID(cp jafirChargingPoint) string {
	for _, e := range cp.ExternalIdentifier {
		if e.isEVSEID() && e.Identifier != "" {
			return e.Identifier
		}
	}
	for _, c := range cp.Connector {
		for _, e := range c.ExternalIdentifier {
			if e.isEVSEID() && e.Identifier != "" {
				return e.Identifier
			}
		}
	}
	return ""
}

// jafirGrossPrice converts a published price into what a driver actually pays.
// AFIR states the tax per price: taxIncluded=false means the value is net, with
// taxRate alongside. Publishers disagree on that rate's unit — Eco-Movement's
// Belgian feed mixes 21 and 0.21 for the same 21% VAT — so a rate above 1 is
// read as a percentage and anything at or below it as a fraction.
func jafirGrossPrice(ep jafirEnergyPrice) float64 {
	if ep.TaxIncluded || ep.TaxRate <= 0 {
		return ep.Value
	}
	rate := ep.TaxRate
	if rate > 1 {
		rate /= 100
	}
	return jafirRound4(ep.Value * (1 + rate))
}

func jafirRound1(v float64) float64 { return math.Round(v*10) / 10 }
func jafirRound4(v float64) float64 { return math.Round(v*10000) / 10000 }

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
