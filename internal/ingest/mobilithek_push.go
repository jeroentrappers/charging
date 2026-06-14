package ingest

import (
	"context"
	"strings"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/store"
)

// IngestMobilithekPush ingests one Mobilithek consumer-push packet (AFIR DATEX II
// JSON). One endpoint receives all pushes; we dispatch on the publication type:
//   - table  → full static snapshot: upsert connectors + ad-hoc tariffs (SCD2)
//   - status → live availability (+ price update) by refill-point id
//
// The CPO is derived from the payload's publicationCreator, and a minimal cpo
// row is ensured (disabled — push sources aren't polled) so chargers attribute
// to the right operator/country. Returns the publication kind + rows touched.
func (e *Engine) IngestMobilithekPush(ctx context.Context, data []byte) (kind string, n int, err error) {
	// Serialize pushes so a status push can't race a table push (or two tables)
	// into the SCD2 tariff path. Pushes are infrequent; queuing is fine.
	e.mobMu.Lock()
	defer e.mobMu.Unlock()

	doc, err := datex.ParseAFIR(data) // XML (LISY/broker) or JSON encoding
	if err != nil {
		return "", 0, err
	}
	if doc.Kind == "" { // synthetic test / unknown publication — nothing to ingest
		return "", 0, nil
	}

	cpoID := mobilithekCPOID(doc.Creator)
	// Prefer the readable operator name from the table push (e.g. "GP JOULE
	// CONNECT"). Status pushes don't carry it, so don't let them downgrade an
	// already-seeded readable name; fall back to the raw NAP id only on a cold
	// start where no table push has named the CPO yet.
	name := doc.Operator
	if name == "" {
		if cur, ok, _ := e.Store.GetCPO(ctx, cpoID); ok && cur.Name != "" {
			name = cur.Name
		} else if doc.Creator.NationalIdentifier != "" {
			name = doc.Creator.NationalIdentifier
		} else {
			name = cpoID
		}
	}
	// Ensure the cpo row exists for attribution (country/name). Disabled so the
	// scheduler never tries to poll a push-only source.
	if serr := e.Store.SeedCPO(ctx, store.CPO{
		ID: cpoID, Name: name, OCPIBaseURL: "push://" + cpoID,
		Country: doc.Creator.Country, SourceType: "mobilithek", Enabled: false,
	}); serr != nil {
		e.Log.Warn("mobilithek: seed cpo", "cpo", cpoID, "err", serr)
	}

	switch doc.Kind {
	case "table":
		// Full snapshot — upsert every connector + its tariff (resilient: a bad
		// row is logged and skipped, never aborting the whole push).
		for _, conn := range doc.Connectors {
			conn.CPOID = cpoID
			id, uerr := e.upsertConnector(ctx, conn)
			if uerr != nil {
				e.Log.Error("mobilithek upsert connector", "cpo", cpoID, "evse", conn.EVSEUID, "err", uerr)
				continue
			}
			if ch, perr := e.processTariff(ctx, id, conn, doc.Tariffs); perr != nil {
				e.Log.Error("mobilithek process tariff", "cpo", cpoID, "evse", conn.EVSEUID, "err", perr)
			} else if ch {
				n++
			}
		}
		e.Log.Info("mobilithek push ingested", "cpo", cpoID, "kind", "table", "connectors", len(doc.Connectors), "tariff_changes", n)
		return "table", n, nil

	case "status":
		for _, u := range doc.Statuses {
			rows, rerr := e.Store.ChargersForEVSE(ctx, cpoID, u.EVSEUID)
			if rerr != nil {
				e.Log.Error("mobilithek chargers-for-evse", "cpo", cpoID, "evse", u.EVSEUID, "err", rerr)
				continue
			}
			avail := 0
			if u.Status == "AVAILABLE" {
				avail = 1
			}
			for _, row := range rows {
				if serr := e.Store.UpsertStatus(ctx, row.ID, u.Status, avail); serr != nil {
					e.Log.Error("mobilithek upsert status", "id", row.ID, "err", serr)
					continue
				}
				// Apply a live ad-hoc price update if present (recomputes the
				// comparable using the charger's stored power/current type).
				if u.Tariff != nil && u.Tariff.OCPIID != "" {
					conn := model.Connector{
						CPOID: cpoID, EVSEUID: u.EVSEUID, ConnectorID: row.ConnectorID,
						PowerKW: row.PowerKW, CurrentType: row.CurrentType, TariffID: u.Tariff.OCPIID,
					}
					if _, perr := e.processTariff(ctx, row.ID, conn, map[string]model.Tariff{u.Tariff.OCPIID: *u.Tariff}); perr != nil {
						e.Log.Error("mobilithek status tariff", "id", row.ID, "err", perr)
					}
				}
				n++
			}
		}
		e.Log.Info("mobilithek push ingested", "cpo", cpoID, "kind", "status", "updates", len(doc.Statuses), "rows", n)
		return "status", n, nil
	}
	return doc.Kind, 0, nil
}

// mobilithekCPOID derives a stable cpo id from the NAP creator id, e.g.
// "DE-NAP-GPJOULECONNECT" → "mob-gpjouleconnect".
func mobilithekCPOID(c datex.AFIRCreator) string {
	id := strings.ToLower(c.NationalIdentifier)
	id = strings.TrimPrefix(id, c.Country+"-")
	id = strings.TrimPrefix(id, strings.ToLower(c.Country)+"-")
	id = strings.TrimPrefix(id, "nap-")
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	slug := b.String()
	if slug == "" {
		return "mobilithek"
	}
	return "mob-" + slug
}
