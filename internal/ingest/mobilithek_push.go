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
	// Non-DATEX overlay feeds pushed to the same endpoint are dispatched first;
	// parseElisoPush only claims the payload if it clearly matches that shape, so
	// AFIR DATEX II (XML/JSON) falls through untouched.
	if p, ok := parseElisoPush(data); ok {
		return e.ingestElisoPush(ctx, p)
	}

	// Parse outside any lock so workers decode (big XML/JSON) in parallel.
	doc, err := datex.ParseAFIR(data) // XML (LISY/broker) or JSON encoding
	if err != nil {
		return "", 0, err
	}
	if doc.Kind == "" { // synthetic test / unknown publication — nothing to ingest
		return "", 0, nil
	}

	cpoID := mobilithekCPOID(doc.Creator)
	// Serialize per CPO: a table + status (or two tables) for the same operator
	// mustn't race the SCD2 tariff path. Different CPOs proceed in parallel.
	defer e.mobLocks.lock(cpoID)()
	e.seedAndHeartbeat(ctx, cpoID, doc.Operator, doc.Creator)

	switch doc.Kind {
	case "table":
		n = e.applyTable(ctx, cpoID, doc.Connectors, doc.Tariffs)
		e.Log.Info("mobilithek push ingested", "cpo", cpoID, "kind", "table", "connectors", len(doc.Connectors), "tariff_changes", n)
		return "table", n, nil
	case "status":
		n = e.applyStatuses(ctx, cpoID, doc.Statuses)
		e.Log.Info("mobilithek push ingested", "cpo", cpoID, "kind", "status", "updates", len(doc.Statuses), "rows", n)
		return "status", n, nil
	}
	return doc.Kind, 0, nil
}

// IngestMobilithekBatch parses many push bodies and applies them coalesced (the
// storm fix). eliso overlays are applied per item; DATEX docs are merged per CPO
// and applied once. Convenience wrapper over applyDocsBatch (mainly for tests /
// non-spool callers); the spool worker parses once itself and calls
// applyDocsBatch directly so it can quarantine bad payloads.
func (e *Engine) IngestMobilithekBatch(ctx context.Context, bodies [][]byte) (n int, err error) {
	var docs []*datex.AFIRDoc
	for _, body := range bodies {
		if p, ok := parseElisoPush(body); ok {
			if _, rows, ierr := e.ingestElisoPush(ctx, p); ierr == nil {
				n += rows
			}
			continue
		}
		doc, derr := datex.ParseAFIR(body)
		if derr != nil || doc.Kind == "" {
			continue
		}
		docs = append(docs, doc)
	}
	return n + e.applyDocsBatch(ctx, docs), nil
}

// applyDocsBatch coalesces already-parsed DATEX docs per CPO — the heart of the
// storm fix. It buckets docs by their real creator CPO and, per CPO, merges the
// deltas (latest wins per refill point / per connector) so a burst of small
// status pushes collapses into ONE ingest under ONE lock with a single CPO seed
// + heartbeat, instead of N serialized round-trip-heavy ingests. Bucketing is by
// each doc's own creator, so it's correct no matter how the caller grouped them.
func (e *Engine) applyDocsBatch(ctx context.Context, docs []*datex.AFIRDoc) (n int) {
	type merged struct {
		operator string
		creator  datex.AFIRCreator
		conns    map[string]model.Connector        // key: connKey (evse+connector)
		tariffs  map[string]model.Tariff           // ad-hoc tariffs referenced by conns
		statuses map[string]datex.AFIRStatusUpdate // key: EVSEUID, latest wins
	}
	byCPO := map[string]*merged{}
	get := func(id string) *merged {
		m := byCPO[id]
		if m == nil {
			m = &merged{conns: map[string]model.Connector{}, tariffs: map[string]model.Tariff{}, statuses: map[string]datex.AFIRStatusUpdate{}}
			byCPO[id] = m
		}
		return m
	}

	// Fold in arrival order (so "latest wins" is truly the newest push).
	for _, doc := range docs {
		if doc == nil || doc.Kind == "" {
			continue
		}
		m := get(mobilithekCPOID(doc.Creator))
		if doc.Operator != "" {
			m.operator = doc.Operator
		}
		if doc.Creator.NationalIdentifier != "" || doc.Creator.Country != "" {
			m.creator = doc.Creator
		}
		switch doc.Kind {
		case "table":
			for _, c := range doc.Connectors {
				m.conns[connKey(c)] = c
			}
			for k, v := range doc.Tariffs {
				m.tariffs[k] = v
			}
		case "status":
			for _, u := range doc.Statuses {
				m.statuses[u.EVSEUID] = u // newest push's state for this refill point wins
			}
		}
	}

	// Apply each CPO's merged result once, under its own lock.
	for cpoID, m := range byCPO {
		func() {
			defer e.mobLocks.lock(cpoID)()
			e.seedAndHeartbeat(ctx, cpoID, m.operator, m.creator)
			if len(m.conns) > 0 {
				conns := make([]model.Connector, 0, len(m.conns))
				for _, c := range m.conns {
					conns = append(conns, c)
				}
				n += e.applyTable(ctx, cpoID, conns, m.tariffs)
			}
			if len(m.statuses) > 0 {
				updates := make([]datex.AFIRStatusUpdate, 0, len(m.statuses))
				for _, u := range m.statuses {
					updates = append(updates, u)
				}
				n += e.applyStatuses(ctx, cpoID, updates)
			}
		}()
	}
	return n
}

// seedAndHeartbeat ensures the (push-only, disabled) cpo row exists with a
// readable name and records the push heartbeat. Caller holds the per-CPO lock.
func (e *Engine) seedAndHeartbeat(ctx context.Context, cpoID, operator string, creator datex.AFIRCreator) {
	// Prefer the readable operator name from a table push; don't let a status
	// push (no operator) downgrade an already-seeded name — fall back to the raw
	// NAP id only on a cold start.
	name := operator
	if name == "" {
		if cur, ok, _ := e.Store.GetCPO(ctx, cpoID); ok && cur.Name != "" {
			name = cur.Name
		} else if creator.NationalIdentifier != "" {
			name = creator.NationalIdentifier
		} else {
			name = cpoID
		}
	}
	if serr := e.Store.SeedCPO(ctx, store.CPO{
		ID: cpoID, Name: name, OCPIBaseURL: "push://" + cpoID,
		Country: creator.Country, SourceType: "mobilithek", Enabled: false,
	}); serr != nil {
		e.Log.Warn("mobilithek: seed cpo", "cpo", cpoID, "err", serr)
	}
	if berr := e.Store.BumpCPOPush(ctx, cpoID); berr != nil {
		e.Log.Warn("mobilithek: bump push heartbeat", "cpo", cpoID, "err", berr)
	}
}

// applyTable upserts a table snapshot's connectors + ad-hoc tariffs. Resilient:
// a bad row is logged and skipped. Caller holds the per-CPO lock.
func (e *Engine) applyTable(ctx context.Context, cpoID string, conns []model.Connector, tariffs map[string]model.Tariff) (n int) {
	var pricedSeen []string
	for _, conn := range conns {
		conn.CPOID = cpoID
		id, uerr := e.upsertConnector(ctx, conn)
		if uerr != nil {
			e.Log.Error("mobilithek upsert connector", "cpo", cpoID, "evse", conn.EVSEUID, "err", uerr)
			continue
		}
		if conn.TariffID != "" {
			if _, ok := tariffs[conn.TariffID]; ok {
				pricedSeen = append(pricedSeen, conn.EVSEUID)
			}
		}
		if ch, perr := e.processTariff(ctx, id, conn, tariffs); perr != nil {
			e.Log.Error("mobilithek process tariff", "cpo", cpoID, "evse", conn.EVSEUID, "err", perr)
		} else if ch {
			n++
		}
	}
	if cerr := e.Store.ConfirmTariffsSeen(ctx, cpoID, pricedSeen); cerr != nil {
		e.Log.Error("mobilithek confirm tariffs", "cpo", cpoID, "err", cerr)
	}
	return n
}

// applyStatuses applies live availability (+ optional price) updates by refill-
// point id. Caller holds the per-CPO lock.
func (e *Engine) applyStatuses(ctx context.Context, cpoID string, updates []datex.AFIRStatusUpdate) (n int) {
	for _, u := range updates {
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
			if u.Tariff != nil && u.Tariff.OCPIID != "" {
				conn := model.Connector{
					CPOID: cpoID, EVSEUID: u.EVSEUID, ConnectorID: row.ConnectorID,
					PowerKW: row.PowerKW, CurrentType: row.CurrentType, TariffID: u.Tariff.OCPIID,
				}
				if _, perr := e.processTariff(ctx, row.ID, conn, map[string]model.Tariff{u.Tariff.OCPIID: *u.Tariff}); perr != nil {
					e.Log.Error("mobilithek status tariff", "id", row.ID, "err", perr)
				} else if cerr := e.Store.ConfirmTariff(ctx, row.ID); cerr != nil {
					e.Log.Error("mobilithek confirm tariff", "id", row.ID, "err", cerr)
				}
			}
			n++
		}
	}
	return n
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
