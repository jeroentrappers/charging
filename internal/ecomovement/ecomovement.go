// Package ecomovement reads the Belgian National Access Point's AFIR charging
// publication, operated by Eco-Movement (https://nap-be.eco-movement.com).
//
// It replaces the old api.eco-movement.com DATEX II XML export: same aggregator,
// a wholly new interface. The feed is DATEX II v3 JSON, profile "AFIR Energy
// Infrastructure" 01-00-00, and each page carries BOTH publications — the static
// table (locations, connectors, power) and the status publication for exactly
// those sites (live availability + ad-hoc price) — so one pass yields coverage,
// status and price together. There is no bulk status endpoint: the only other
// route is /status/{evse_id}, one request per EVSE, which is why the pages are
// the status source here.
//
// Pagination is 1,000 sites per page via ?limit=&offset=; the response's Link
// rel="next" header is followed when present. Belgium is ~17 pages / ~104 MB
// (the server does not compress), so poll it at an hourly-ish cadence, not per
// minute.
//
// Identity: the publisher keys refill points by an internal idG and carries the
// roaming (eMI3) EVSE id on the connector's externalIdentifier. We key chargers
// by that roaming id — it is stable, it is what every other Belgian source uses,
// and it is what the old XML export published, so chargers keep their identity
// across the interface switch. The status publication references the idG, so
// statuses are joined before the re-key.
package ecomovement

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/model"
)

const (
	pageSize = 1000 // sites per page (the publisher's own page size)
	maxPages = 500  // runaway guard: ~500k sites, far beyond any national feed
	// The pages are large and generated on demand; cancellation still honors
	// the caller's context, this is only an upper bound.
	pageTimeout = 300 * time.Second
)

// Fetch walks every page of the feed and returns the connectors (with live
// status) plus the ad-hoc tariffs they reference. Signature matches the
// ingest locFeed fetcher.
func Fetch(ctx context.Context, cpoID, base, token string) ([]model.Connector, map[string]model.Tariff, error) {
	client := &http.Client{Timeout: pageTimeout}
	tariffs := map[string]model.Tariff{}
	var conns []model.Connector
	var updated []time.Time // when the publisher last touched each connector's status

	next := pageURL(base, 0)
	for page := 0; page < maxPages && next != ""; page++ {
		body, link, err := get(ctx, client, next, token)
		if err != nil {
			return nil, nil, fmt.Errorf("ecomovement page %d: %w", page, err)
		}
		doc, err := datex.ParseAFIRJSON(body)
		if err != nil {
			return nil, nil, fmt.Errorf("ecomovement parse page %d: %w", page, err)
		}
		if len(doc.Connectors) == 0 {
			break // past the last page
		}
		pc, pu := pageConnectors(cpoID, doc, tariffs)
		conns = append(conns, pc...)
		updated = append(updated, pu...)

		if link != "" {
			next = resolve(next, link) // the publisher sends it absolute; relative still works
		} else {
			next = pageURL(base, (page+1)*pageSize)
		}
	}
	return collapseDuplicates(conns, updated), tariffs, nil
}

// collapseDuplicates keeps one connector per roaming EVSE id + connector id.
//
// The feed sometimes publishes the same physical EVSE twice under two internal
// refill-point ids: a live record and a decommissioned one left behind, each
// with its own tariff. Lidl's Belgian sites are the clearest case — 19 EVSE ids
// appear as an "inoperative" copy at the operator's OLD price (€0.242/kWh AC)
// next to an "available" copy at the current one (€0.50). Both collapse onto the
// same charger row, so without this whichever copy the walk happened to read
// last decided both the price and the availability we published.
//
// Preference: a usable status beats a dead one, then a priced record beats an
// unpriced one, then the more recently updated record wins — the leftover copy's
// status timestamp stands still (Lidl's ghosts have not moved since the day they
// were retired) while its live twin is touched whenever a driver plugs in. That
// tie-break matters: a live charger reads "inoperative" now and then, and
// without it the ghost's old price won those moments on page order alone.
// It also protects against offset pagination overlapping while the publisher
// regenerates a page.
func collapseDuplicates(conns []model.Connector, updated []time.Time) []model.Connector {
	type key struct{ evse, conn string }
	at := func(i int) time.Time {
		if i < len(updated) {
			return updated[i]
		}
		return time.Time{}
	}
	best := make(map[key]int, len(conns))
	order := make([]key, 0, len(conns))
	for i, c := range conns {
		k := key{c.EVSEUID, c.ConnectorID}
		j, seen := best[k]
		if !seen {
			best[k] = i
			order = append(order, k)
			continue
		}
		ri, rj := rank(conns[i]), rank(conns[j])
		if ri > rj || (ri == rj && at(i).After(at(j))) {
			best[k] = i
		}
	}
	if len(order) == len(conns) {
		return conns
	}
	out := make([]model.Connector, 0, len(order))
	for _, k := range order {
		out = append(out, conns[best[k]])
	}
	return out
}

// rank scores how much a record deserves to represent its EVSE: a charger that
// can serve a driver outranks a dead one, and a priced record outranks a silent
// one at the same status.
func rank(c model.Connector) int {
	score := 0
	switch c.EVSEStatus {
	case "AVAILABLE", "CHARGING":
		score = 4
	case "OUTOFORDER":
		score = 2
	default: // UNKNOWN or absent
		score = 1
	}
	if c.TariffID != "" {
		score++
	}
	return score
}

// pageConnectors overlays one page's status publication onto its table, re-keys
// the connectors onto their roaming EVSE id, and records the tariffs seen. The
// second return value is each connector's status timestamp, used to break ties
// between duplicate records.
func pageConnectors(cpoID string, doc *datex.AFIRDoc, tariffs map[string]model.Tariff) ([]model.Connector, []time.Time) {
	conns := doc.Connectors
	updated := make([]time.Time, len(conns))
	byPoint := make(map[string][]int, len(conns))
	for i := range conns {
		byPoint[conns[i].EVSEUID] = append(byPoint[conns[i].EVSEUID], i)
	}
	for _, s := range doc.Statuses {
		for _, i := range byPoint[s.EVSEUID] {
			updated[i] = s.LastUpdated
			if s.Status != "" {
				conns[i].EVSEStatus = s.Status
			}
			if s.Tariff == nil {
				continue
			}
			id := s.Tariff.OCPIID
			if id == "" {
				id = s.EVSEUID
			}
			tariffs[id] = *s.Tariff
			conns[i].TariffID = id
		}
	}
	// Tariffs published on the table itself (none today, but the profile allows
	// them) still count.
	for id, t := range doc.Tariffs {
		if _, ok := tariffs[id]; !ok {
			tariffs[id] = t
		}
	}
	for i := range conns {
		conns[i].CPOID = cpoID
		if evseID := doc.EVSEIDs[conns[i].EVSEUID]; evseID != "" {
			conns[i].EVSEUID = evseID
		}
	}
	return conns, updated
}

// pageURL builds the feed URL for one offset, preserving any query the caller
// configured on the base URL.
func pageURL(base string, offset int) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set("limit", strconv.Itoa(pageSize))
	q.Set("offset", strconv.Itoa(offset))
	u.RawQuery = q.Encode()
	return u.String()
}

// get fetches one page and returns its body plus the Link rel="next" URL ("" if
// the publisher sent none).
func get(ctx context.Context, client *http.Client, pageURL, token string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	return body, nextLink(resp.Header.Get("Link")), nil
}

// resolve interprets a Link header URL against the page it came from.
func resolve(base, link string) string {
	b, err := url.Parse(base)
	if err != nil {
		return link
	}
	l, err := b.Parse(link)
	if err != nil {
		return link
	}
	return l.String()
}

// nextLink extracts the URL of a Link header's rel="next" entry.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 {
			continue
		}
		isNext := false
		for _, s := range segs[1:] {
			if strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(s), `"`, ""), "rel=next") {
				isNext = true
			}
		}
		if !isNext {
			continue
		}
		u := strings.TrimSpace(segs[0])
		if strings.HasPrefix(u, "<") && strings.HasSuffix(u, ">") {
			return u[1 : len(u)-1]
		}
	}
	return ""
}
