package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/model"
)

// eliso pushes a flat, non-DATEX JSON feed to the Mobilithek push endpoint: a
// live availability + ad-hoc price overlay keyed by EVSE id, with no locations
// or power. Shape:
//
//	{"evses":[{
//	  "evseId":"DE*ELI*E3584570", "operator_name":"eliso GmbH",
//	  "adhoc_price":0.49, "blocking_fee":0.10,
//	  "operational_status":"Operational", "availability_status":"Not in use",
//	  "mobilithek_last_updated_dts":"2026-06-15T08:29:00.451084+00:00"}]}
//
// The chargers themselves are seeded elsewhere (the locations arrive via a
// broker/aggregator AFIR table push), so we match by EVSE id alone and apply
// status + price to whatever rows exist. EVSEs we don't have a location for are
// skipped (rows=0), exactly like an AFIR status push for an unknown EVSE.
//
// elisoCPOID is the synthetic lock key serializing eliso pushes against each
// other in the spool worker pool. It is NOT used for charger attribution —
// attribution stays with the seeding CPO via ChargersForEVSEAny.
const elisoCPOID = "mob-eliso"

type elisoEVSE struct {
	EVSEID             string   `json:"evseId"`
	OperatorName       string   `json:"operator_name"`
	AdhocPrice         *float64 `json:"adhoc_price"`
	BlockingFee        *float64 `json:"blocking_fee"`
	OperationalStatus  string   `json:"operational_status"`
	AvailabilityStatus string   `json:"availability_status"`
}

type elisoPush struct {
	EVSEs []elisoEVSE `json:"evses"`
}

// parseElisoPush decodes an eliso flat push. ok is false (and the AFIR path
// takes over) unless the payload clearly matches the eliso shape: a non-empty
// evses[] whose first entry carries an evseId and an operational_status — fields
// no AFIR DATEX II envelope has at top level.
func parseElisoPush(data []byte) (*elisoPush, bool) {
	var p elisoPush
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, false
	}
	if len(p.EVSEs) == 0 {
		return nil, false
	}
	first := p.EVSEs[0]
	if first.EVSEID == "" || first.OperationalStatus == "" {
		return nil, false
	}
	return &p, true
}

// ingestElisoPush applies an eliso overlay push: per EVSE, upsert live status
// and (when priced) an ad-hoc tariff onto every connector row we already hold
// for that EVSE id. Returns kind "eliso" so the spool worker treats it as
// handled rather than quarantining it.
func (e *Engine) ingestElisoPush(ctx context.Context, p *elisoPush) (string, int, error) {
	// Serialize eliso pushes against each other (the spool runs many workers);
	// charger attribution is unaffected — we look up by EVSE id, not CPO.
	defer e.mobLocks.lock(elisoCPOID)()

	// No push heartbeat here: eliso sends a full snapshot every push, so each one
	// freshens charger_status.updated_at on all its chargers directly. (The
	// heartbeat exists for delta feeds that only push what changed.)

	n := 0
	for _, ev := range p.EVSEs {
		if ev.EVSEID == "" {
			continue
		}
		rows, rerr := e.Store.ChargersForEVSEAny(ctx, ev.EVSEID)
		if rerr != nil {
			e.Log.Error("eliso chargers-for-evse", "evse", ev.EVSEID, "err", rerr)
			continue
		}
		status := elisoStatus(ev.OperationalStatus, ev.AvailabilityStatus)
		avail := 0
		if status == "AVAILABLE" {
			avail = 1
		}
		tar := elisoTariff(ev)
		for _, row := range rows {
			if serr := e.Store.UpsertStatus(ctx, row.ID, status, avail); serr != nil {
				e.Log.Error("eliso upsert status", "id", row.ID, "err", serr)
				continue
			}
			if tar != nil {
				conn := model.Connector{
					EVSEUID: ev.EVSEID, ConnectorID: row.ConnectorID,
					PowerKW: row.PowerKW, CurrentType: row.CurrentType, TariffID: tar.OCPIID,
				}
				if _, perr := e.processTariff(ctx, row.ID, conn, map[string]model.Tariff{tar.OCPIID: *tar}); perr != nil {
					e.Log.Error("eliso process tariff", "id", row.ID, "err", perr)
				} else if cerr := e.Store.ConfirmTariff(ctx, row.ID); cerr != nil {
					e.Log.Error("eliso confirm tariff", "id", row.ID, "err", cerr)
				}
			}
			n++
		}
	}
	e.Log.Info("eliso push ingested", "cpo", elisoCPOID, "kind", "eliso", "evses", len(p.EVSEs), "rows", n)
	return "eliso", n, nil
}

// elisoStatus maps eliso's two status fields onto our vocabulary. A
// non-operational EVSE is OUTOFORDER regardless of availability; an operational
// one is AVAILABLE when free ("Not in use") and CHARGING when occupied ("In
// use"). Anything unrecognised (e.g. "None") is UNKNOWN.
func elisoStatus(operational, availability string) string {
	if operational != "Operational" {
		return "OUTOFORDER"
	}
	switch availability {
	case "Not in use":
		return "AVAILABLE"
	case "In use":
		return "CHARGING"
	default:
		return "UNKNOWN"
	}
}

// elisoTariff builds an ad-hoc tariff from the per-EVSE prices. adhoc_price is
// €/kWh (the comparable energy price). blocking_fee is the idle fee in €/min;
// it is recorded as PARKING_TIME (€/hour) for display only — pricing.Evaluate
// excludes PARKING_TIME from the charging-session comparable, so it never skews
// rankings. Returns nil when there's no usable price (status-only update).
func elisoTariff(ev elisoEVSE) *model.Tariff {
	var comps []model.PriceComponent
	if ev.AdhocPrice != nil {
		comps = append(comps, model.PriceComponent{Type: "ENERGY", Price: *ev.AdhocPrice})
	}
	if ev.BlockingFee != nil && *ev.BlockingFee > 0 {
		comps = append(comps, model.PriceComponent{Type: "PARKING_TIME", Price: *ev.BlockingFee * 60})
	}
	if len(comps) == 0 {
		return nil
	}
	// Stable per-EVSE tariff id so the SCD2 path only versions on real changes.
	return &model.Tariff{
		OCPIID:   "eliso-" + ev.EVSEID,
		Currency: "EUR",
		Elements: []model.TariffElement{{PriceComponents: comps}},
	}
}

// ---- eliso location table (the other, table-shaped, eliso feed) ----------
//
// Besides the flat status/price overlay above, eliso also pushes its full
// location inventory as a proprietary top-level JSON *array* of stations
// (locations + connectors + power + plug — but no price), e.g.:
//
//	[{"city":"Haßfurt","postalCode":"97437","address":"Am Wasserwerk 2",
//	  "operator_name":"eliso GmbH","country_iso_3166_alpha_2":"DE",
//	  "coordinates":{"latitude":50.03,"longitude":10.52},
//	  "evses":[{"evseId":"DE*ELI*E5207045","charge_points_type":"dc",
//	    "connectors":[{"maxPower":135,"powerType":"DC","type_of_connector":"Combo2/CCS (DC)"}]}]}]
//
// This is the table half of the eliso feed: it SEEDS the eliso chargers (under
// elisoCPOID) so the status/price overlay above has rows to attach to — exactly
// the "locations arrive elsewhere" the overlay's doc note assumes, except eliso
// now publishes them itself instead of via a broker AFIR table push.

type elisoTableConnector struct {
	MaxPower        float64 `json:"maxPower"`
	PowerType       string  `json:"powerType"`
	TypeOfConnector string  `json:"type_of_connector"`
}

type elisoTableEVSE struct {
	EVSEID           string                `json:"evseId"`
	ChargePointsType string                `json:"charge_points_type"`
	Connectors       []elisoTableConnector `json:"connectors"`
}

type elisoStation struct {
	City         string `json:"city"`
	Address      string `json:"address"`
	PostalCode   string `json:"postalCode"`
	Country      string `json:"country_iso_3166_alpha_2"`
	OperatorName string `json:"operator_name"`
	Coordinates  struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"coordinates"`
	EVSEs []elisoTableEVSE `json:"evses"`
}

// parseElisoTable decodes the eliso location table. ok is false (and the AFIR
// path takes over) unless the payload is a top-level JSON array of stations —
// which no AFIR DATEX II envelope (always a MessageContainer object) is — with
// at least one station carrying usable coordinates and an EVSE id.
func parseElisoTable(data []byte) ([]elisoStation, bool) {
	t := bytes.TrimPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte{0xEF, 0xBB, 0xBF})
	t = bytes.TrimLeft(t, " \t\r\n")
	if len(t) == 0 || t[0] != '[' {
		return nil, false
	}
	var st []elisoStation
	if err := json.Unmarshal(data, &st); err != nil || len(st) == 0 {
		return nil, false
	}
	for _, s := range st {
		if s.Coordinates.Latitude == 0 && s.Coordinates.Longitude == 0 {
			continue
		}
		for _, ev := range s.EVSEs {
			if ev.EVSEID != "" {
				return st, true
			}
		}
	}
	return nil, false
}

// ingestElisoTable upserts the eliso location snapshot as connector rows under
// elisoCPOID (a full snapshot each push, like an AFIR table push). Returns kind
// "eliso-table" and the number of connector rows built.
func (e *Engine) ingestElisoTable(ctx context.Context, stations []elisoStation) (string, int, error) {
	defer e.mobLocks.lock(elisoCPOID)()

	var conns []model.Connector
	operator, country := "", "DE"
	for _, s := range stations {
		lat, lon := s.Coordinates.Latitude, s.Coordinates.Longitude
		if lat == 0 && lon == 0 {
			continue // no geom — can't create a charger row
		}
		if operator == "" && s.OperatorName != "" {
			operator = s.OperatorName
		}
		if s.Country != "" {
			country = strings.ToUpper(s.Country)
		}
		name := elisoName(s)
		for _, ev := range s.EVSEs {
			if ev.EVSEID == "" {
				continue
			}
			for i, c := range ev.Connectors {
				conns = append(conns, model.Connector{
					EVSEUID:     ev.EVSEID,
					ConnectorID: strconv.Itoa(i + 1),
					Lat:         lat,
					Lon:         lon,
					PowerKW:     elisoPowerKW(c.MaxPower),
					PlugType:    elisoPlug(c.TypeOfConnector),
					CurrentType: elisoCurrent(c.PowerType, ev.ChargePointsType),
					Name:        name,
					Address:     s.Address,
					PostalCode:  s.PostalCode,
					City:        s.City,
				})
			}
		}
	}
	if len(conns) == 0 {
		return "eliso-table", 0, nil
	}
	e.seedAndHeartbeat(ctx, elisoCPOID, operator, datex.AFIRCreator{Country: country, NationalIdentifier: "DEELI"})
	changes := e.applyTable(ctx, elisoCPOID, conns, nil)
	e.Log.Info("eliso table ingested", "cpo", elisoCPOID,
		"stations", len(stations), "connectors", len(conns), "tariff_changes", changes)
	return "eliso-table", len(conns), nil
}

// elisoName builds a readable charger name: "eliso GmbH — Am Wasserwerk 2, Haßfurt".
func elisoName(s elisoStation) string {
	loc := strings.Trim(strings.TrimSpace(s.Address+", "+s.City), " ,")
	switch {
	case s.OperatorName != "" && loc != "":
		return s.OperatorName + " — " + loc
	case loc != "":
		return loc
	default:
		return s.OperatorName
	}
}

// elisoPowerKW returns power in kW. eliso reports kW (e.g. 135, 22); guard
// defensively against a value that's clearly in watts.
func elisoPowerKW(p float64) float64 {
	if p > 1000 {
		p = p / 1000
	}
	return math.Round(p*10) / 10
}

// elisoCurrent maps eliso's powerType / charge_points_type to our AC|DC vocab.
func elisoCurrent(powerType, cpType string) string {
	if strings.Contains(strings.ToLower(powerType+" "+cpType), "dc") {
		return model.CurrentDC
	}
	return model.CurrentAC
}

// elisoPlug maps eliso's free-text connector labels (e.g. "Combo2/CCS (DC)",
// "Type 2 (AC)", "CHAdeMO (DC)") onto the OCPI connector standard we store.
func elisoPlug(s string) string {
	k := strings.ToLower(s)
	switch {
	case strings.Contains(k, "ccs"), strings.Contains(k, "combo"):
		return "IEC_62196_T2_COMBO"
	case strings.Contains(k, "chademo"):
		return "CHADEMO"
	case strings.Contains(k, "type 2"), strings.Contains(k, "type2"), strings.Contains(k, "mennekes"):
		return "IEC_62196_T2"
	case strings.Contains(k, "type 1"), strings.Contains(k, "type1"):
		return "IEC_62196_T1"
	case strings.Contains(k, "schuko"), strings.Contains(k, "domestic"), strings.Contains(k, "type f"):
		return "DOMESTIC_F"
	case strings.Contains(k, "tesla"):
		return "TESLA_S"
	}
	return model.NormalizePlug(s)
}

// tryElisoOverlay dispatches the non-DATEX eliso feeds pushed to the shared
// Mobilithek endpoint: the flat status/price overlay (a JSON object) and the
// location table (a top-level JSON array). handled=false lets AFIR DATEX II
// parsing take over; when handled, err carries any ingest failure so the caller
// can quarantine the payload.
func (e *Engine) tryElisoOverlay(ctx context.Context, body []byte) (handled bool, kind string, rows int, err error) {
	if p, ok := parseElisoPush(body); ok {
		k, n, ierr := e.ingestElisoPush(ctx, p)
		return true, k, n, ierr
	}
	if st, ok := parseElisoTable(body); ok {
		k, n, ierr := e.ingestElisoTable(ctx, st)
		return true, k, n, ierr
	}
	return false, "", 0, nil
}
