package ingest

import (
	"context"
	"encoding/json"

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
