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
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/appmire/charging/internal/model"
)

// ---- Static publication (EnergyInfrastructureTablePublication) ----

// afirCreatorXML is the publicationCreator (country + NAP id) — used on the
// push path to derive which CPO a payload belongs to.
type afirCreatorXML struct {
	Country            string `xml:"country"`
	NationalIdentifier string `xml:"nationalIdentifier"`
}

// Publishers differ in their envelope: Mobilithek wraps the publication in a
// container with a <payload> child, while EnergyVision's document root IS the
// payload element. Each pub struct therefore embeds its body twice — once for
// the root's own children, once under "payload" — and merges the two.
type afirStaticBody struct {
	Creator afirCreatorXML `xml:"publicationCreator"`
	Sites   []afirSite     `xml:"energyInfrastructureTable>energyInfrastructureSite"`
}

type afirStaticPub struct {
	afirStaticBody
	Wrapped afirStaticBody `xml:"payload"`
}

func (p afirStaticPub) allSites() []afirSite {
	return append(append([]afirSite(nil), p.Sites...), p.Wrapped.Sites...)
}

func (p afirStaticPub) creator() afirCreatorXML {
	if p.Creator != (afirCreatorXML{}) {
		return p.Creator
	}
	return p.Wrapped.Creator
}

type afirSite struct {
	Name       string        `xml:"name>values>value"`
	Latitude   float64       `xml:"locationReference>pointByCoordinates>pointCoordinates>latitude"`
	Longitude  float64       `xml:"locationReference>pointByCoordinates>pointCoordinates>longitude"`
	PostalCode string        `xml:"locationReference>_pointLocationExtension>facilityLocation>address>postcode"`
	City       string        `xml:"locationReference>_pointLocationExtension>facilityLocation>address>city"`
	Operator   string        `xml:"operator>name>values>value"`
	Brand      string        `xml:"brand>values>value"`
	Rates      []afirRate    `xml:"energyRate"`
	// Accept the energyProduct wrapper at the site level too (see afirStation).
	ProductRates []afirRate    `xml:"energyProduct>energyRate"`
	Stations     []afirStation `xml:"energyInfrastructureStation"`

	// Some publishers (EnergyVision) carry the site location on an
	// aegi:entrance PointLocation instead of locationReference, with the AFIR
	// facility address (multilingual city, typed address lines) under
	// _locationExtension. Used as fallbacks when the primary paths are empty.
	EntLatitude  float64        `xml:"entrance>pointByCoordinates>pointCoordinates>latitude"`
	EntLongitude float64        `xml:"entrance>pointByCoordinates>pointCoordinates>longitude"`
	EntPostcode  string         `xml:"entrance>_locationExtension>afirFacilityLocation>address>postcode"`
	EntCity      string         `xml:"entrance>_locationExtension>afirFacilityLocation>address>city>values>value"`
	EntLines     []afirAddrLine `xml:"entrance>_locationExtension>afirFacilityLocation>address>addressLine"`
}

// rates returns the site's rates from either placement (direct or nested under
// energyProduct).
func (s afirSite) rates() []afirRate {
	if len(s.Rates) > 0 {
		return s.Rates
	}
	return s.ProductRates
}

type afirAddrLine struct {
	Type string `xml:"type"`
	Text string `xml:"text>values>value"`
}

// lat/lon/postcode/city resolve the primary location path with the entrance
// fallback.
func (s afirSite) lat() float64 {
	if s.Latitude != 0 {
		return s.Latitude
	}
	return s.EntLatitude
}

func (s afirSite) lon() float64 {
	if s.Longitude != 0 {
		return s.Longitude
	}
	return s.EntLongitude
}

func (s afirSite) postcode() string {
	if s.PostalCode != "" {
		return s.PostalCode
	}
	return s.EntPostcode
}

func (s afirSite) city() string {
	if s.City != "" {
		return s.City
	}
	return s.EntCity
}

// street composes "Street 12" from typed address lines, in line order.
func (s afirSite) street() string {
	var street, nr string
	for _, l := range s.EntLines {
		switch strings.ToLower(l.Type) {
		case "street":
			street = l.Text
		case "housenumber":
			nr = l.Text
		}
	}
	if street != "" && nr != "" {
		return street + " " + nr
	}
	return street
}

// operator falls back to the brand (EnergyVision publishes no operator element)
// so cards still get a recognisable name.
func (s afirSite) operator() string {
	if s.Operator != "" {
		return s.Operator
	}
	return s.Brand
}

type afirStation struct {
	Rates []afirRate `xml:"energyRate"`
	// Like the refill point, the station may carry the rate directly or nested
	// under energyProduct. EnergyVision moved its station-wide ad-hoc rate to
	// energyProduct>energyRate in 2026-07, which the bare mapping missed.
	ProductRates []afirRate        `xml:"energyProduct>energyRate"`
	RefillPoints []afirRefillPoint `xml:"refillPoint"`
}

// rates returns the station's rates from either placement (direct or nested
// under energyProduct).
func (st afirStation) rates() []afirRate {
	if len(st.Rates) > 0 {
		return st.Rates
	}
	return st.ProductRates
}

type afirRefillPoint struct {
	ID            string     `xml:"id,attr"`
	ConnectorType string     `xml:"connector>connectorType"`
	ChargingMode  string     `xml:"connector>chargingMode"`
	MaxPowerW     float64    `xml:"connector>maxPowerAtSocket"`
	Rates         []afirRate `xml:"energyRate"`
	// The AFIR schema nests the rate under energyProduct; some publishers put it
	// directly on the refillPoint. Accept both (our own publisher uses the former).
	ProductRates []afirRate `xml:"energyProduct>energyRate"`
}

// rates returns the refill point's rates from either placement (direct or nested
// under energyProduct).
func (rp afirRefillPoint) rates() []afirRate {
	if len(rp.Rates) > 0 {
		return rp.Rates
	}
	return rp.ProductRates
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
	// AddInfo holds any additionalInformation text values — where German NAP
	// ad-hoc tariffs state a per-minute grace threshold in prose (see graceMinutes).
	AddInfo []string `xml:"additionalInformation>values>value"`
}

// ---- Status publication (EnergyInfrastructureStatusPublication) ----

type afirStatusBody struct {
	Creator afirCreatorXML `xml:"publicationCreator"`
	// Publishers wrap station statuses under either energyInfrastructureStatus
	// or energyInfrastructureSiteStatus (e.g. DELND, EnergyVision). Accept both.
	Stations     []afirStationStatus `xml:"energyInfrastructureStatus>energyInfrastructureStationStatus"`
	SiteStations []afirStationStatus `xml:"energyInfrastructureSiteStatus>energyInfrastructureStationStatus"`
}

type afirStatusPub struct {
	afirStatusBody
	Wrapped afirStatusBody `xml:"payload"`
}

func (p afirStatusPub) allStations() []afirStationStatus {
	out := append(append([]afirStationStatus(nil), p.Stations...), p.SiteStations...)
	return append(append(out, p.Wrapped.Stations...), p.Wrapped.SiteStations...)
}

func (p afirStatusPub) creator() afirCreatorXML {
	if p.Creator != (afirCreatorXML{}) {
		return p.Creator
	}
	return p.Wrapped.Creator
}

type afirStationStatus struct {
	RefillPoints []afirRefillPointStatus `xml:"refillPointStatus"`
	// Some publishers (e.g. EnergyVision) attach the ad-hoc price update to the
	// station rather than to each refill point; it then applies to every refill
	// point in the station that has no update of its own.
	UpdateRates []afirPrice `xml:"energyRateUpdate>energyPrice"`
	UpdateCurr  string      `xml:"energyRateUpdate>applicableCurrency"`
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

// graceRe matches a per-minute grace threshold stated in free text, e.g.
// "erst nach 240 Minuten", "after 30 min", "à partir de 90 minutes". DATEX II
// has no structured field for this (MobilithekDE AFIR-DATEX-II issue #8), so
// German NAP ad-hoc tariffs put it in a price's additionalInformation.
var graceRe = regexp.MustCompile(`(?i)(?:erst nach|nach|after|from|[àa] partir de|apr[eè]s|vanaf)\s+(\d+)\s*min`)

// graceMinutes returns the threshold N (minutes) after which a per-minute price
// starts to apply, parsed from additionalInformation text; 0 if none is stated
// (the fee applies from the start, our prior behaviour).
func graceMinutes(texts ...string) int {
	for _, s := range texts {
		if m := graceRe.FindStringSubmatch(s); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return n
			}
		}
	}
	return 0
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
			comps = append(comps, model.PriceComponent{Type: "TIME", Price: round1(p.Value * 60), AfterMinutes: graceMinutes(p.AddInfo...)})
		case "flatrate", "baseprice":
			comps = append(comps, model.PriceComponent{Type: "FLAT", Price: p.Value})
		case "free":
			comps = append(comps, model.PriceComponent{Type: "ENERGY", Price: 0})
		default: // other -> skip
		}
	}
	return dedupeComponents(comps)
}

// dedupeComponents drops exact-duplicate price components, preserving order.
// Some publishers repeat the same price across several priceGroupIndex values
// (EnergyVision emits pricePerKWh three times, pricePerMinute twice, each under
// a distinct index), which would otherwise flatten into a tariff that lists the
// same line several times. Identical (type, price, step) entries are redundant,
// so collapsing them is safe and yields one line per distinct price.
func dedupeComponents(comps []model.PriceComponent) []model.PriceComponent {
	if len(comps) < 2 {
		return comps
	}
	seen := make(map[model.PriceComponent]bool, len(comps))
	out := comps[:0]
	for _, c := range comps {
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
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
	conns, tariffs := buildStaticConnectors(pub, cpoID)
	return conns, tariffs, nil
}

// buildStaticConnectors turns a parsed table publication into connectors + the
// ad-hoc tariff map (keyed by refillPoint id == each connector's TariffID).
func buildStaticConnectors(pub afirStaticPub, cpoID string) ([]model.Connector, map[string]model.Tariff) {
	tariffs := map[string]model.Tariff{}
	var conns []model.Connector

	for _, s := range pub.allSites() {
		for _, st := range s.Stations {
			for _, rp := range st.RefillPoints {
				addr := s.street()
				if addr == "" {
					addr = address(site{PostalCode: s.postcode(), City: s.city()})
				}
				conn := model.Connector{
					CPOID:       cpoID,
					EVSEUID:     rp.ID,
					ConnectorID: "1",
					Lat:         s.lat(),
					Lon:         s.lon(),
					PowerKW:     round1(rp.MaxPowerW / 1000),
					PlugType:    mapPlug(rp.ConnectorType),
					CurrentType: afirCurrentType(rp.ConnectorType, rp.ChargingMode),
					Name:        name(site{Name: s.Name, Operator: s.operator()}),
					Address:     addr,
					PostalCode:  s.postcode(),
					City:        s.city(),
				}

				// Pricing may sit on the refillPoint (directly or under
				// energyProduct), its station, or its site.
				rates := rp.rates()
				if len(rates) == 0 {
					rates = st.rates()
				}
				if len(rates) == 0 {
					rates = s.rates()
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
	return conns, tariffs
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
	pub.each(func(id, status string, tariff *model.Tariff) {
		out[id] = AFIRStatus{Status: status, Tariff: tariff}
	})
	return out, nil
}

// each walks every refill point status in the publication, resolving the price
// update per refill point: its own energyRateUpdate wins, else the enclosing
// station's (EnergyVision publishes station-level updates only).
func (p afirStatusPub) each(fn func(id, status string, tariff *model.Tariff)) {
	for _, st := range p.allStations() {
		for _, rps := range st.RefillPoints {
			id := rps.refID()
			if id == "" {
				continue
			}
			rates, curr := rps.UpdateRates, rps.UpdateCurr
			if len(rates) == 0 {
				rates, curr = st.UpdateRates, st.UpdateCurr
			}
			var tariff *model.Tariff
			if len(rates) > 0 {
				if t, ok := buildTariff(id, afirRate{Currency: curr, Prices: rates}); ok {
					tariff = &t
				}
			}
			fn(id, statusVocab(rps.Status), tariff)
		}
	}
}

// ParseAFIR decodes one Mobilithek AFIR push regardless of encoding: DATEX II
// XML (e.g. LISY / municipal aggregators, e-clearing brokers) or the JSON
// encoding (GP JOULE, …). It returns the same AFIRDoc the JSON path produces,
// so the ingest is identical. Matching is namespace-agnostic.
func ParseAFIR(data []byte) (*AFIRDoc, error) {
	t := bytes.TrimPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte{0xEF, 0xBB, 0xBF})
	t = bytes.TrimLeft(t, " \t\r\n")
	if len(t) > 0 && t[0] == '<' {
		return parseAFIRXML(data)
	}
	return ParseAFIRJSON(data)
}

// parseAFIRXML handles the DATEX II XML encoding of the AFIR profile, reusing
// the same struct definitions as the consumer-pull feed.
func parseAFIRXML(data []byte) (*AFIRDoc, error) {
	doc := &AFIRDoc{Tariffs: map[string]model.Tariff{}}
	switch {
	case bytes.Contains(data, []byte("EnergyInfrastructureStatusPublication")):
		var pub afirStatusPub
		if err := xml.Unmarshal(data, &pub); err != nil {
			return nil, fmt.Errorf("decode afir xml status: %w", err)
		}
		doc.Kind = "status"
		doc.Creator = AFIRCreator{Country: pub.creator().Country, NationalIdentifier: pub.creator().NationalIdentifier}
		pub.each(func(id, status string, tariff *model.Tariff) {
			doc.Statuses = append(doc.Statuses, AFIRStatusUpdate{EVSEUID: id, Status: status, Tariff: tariff})
		})
	case bytes.Contains(data, []byte("EnergyInfrastructureTablePublication")):
		var pub afirStaticPub
		if err := xml.Unmarshal(data, &pub); err != nil {
			return nil, fmt.Errorf("decode afir xml table: %w", err)
		}
		doc.Kind = "table"
		doc.Creator = AFIRCreator{Country: pub.creator().Country, NationalIdentifier: pub.creator().NationalIdentifier}
		for _, s := range pub.allSites() {
			if s.Operator != "" {
				doc.Operator = s.Operator
				break
			}
		}
		doc.Connectors, doc.Tariffs = buildStaticConnectors(pub, "")
	}
	return doc, nil
}
