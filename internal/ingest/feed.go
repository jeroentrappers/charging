package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/appmire/charging/internal/bnetza"
	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/irve"
	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/monta"
	"github.com/appmire/charging/internal/normalize"
	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/source"
)

// feed abstracts a data source so the engine treats OCPI and DATEX II uniformly.
type feed interface {
	// Availability returns connectors with current status (light path).
	Availability(ctx context.Context) ([]model.Connector, error)
	// Full returns connectors plus the tariffs needed for price history.
	Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error)
}

// feedFor builds the right feed for a source based on its SourceType. For DATEX
// sources the feed URL is taken from the OCPIBaseURL column.
func feedFor(src source.Source) feed {
	switch src.CPO.SourceType {
	case "datex":
		// The Belgian NAP DATEX II feeds (Eco-Movement) authenticate with a
		// ?token= query param, not a Bearer header. Fold the resolved token into
		// the URL; open feeds (Indigo) carry no token and pass through unchanged.
		return datexFeed{cpoID: src.CPO.ID, url: datexURL(src.CPO.OCPIBaseURL, src.Token)}
	case "datex_afir":
		// AFIR DATEX II table+status pair over plain Bearer auth (EnergyVision).
		// OCPIBaseURL = "<table-url>|<status-url>".
		return newAFIRPairFeed(src.CPO.ID, src.CPO.OCPIBaseURL, src.Token)
	case "mobilithek":
		// DE Mobilithek AFIR DATEX II (mutual-TLS). OCPIBaseURL = "<static>|<status>".
		return newMobilithekFeed(src.CPO.ID, src.CPO.OCPIBaseURL)
	case "bnetza":
		return locFeed{cpoID: src.CPO.ID, url: src.CPO.OCPIBaseURL, token: src.Token, fetch: bnetza.Fetch}
	case "irve":
		return locFeed{cpoID: src.CPO.ID, url: src.CPO.OCPIBaseURL, token: src.Token, fetch: irve.Fetch}
	case "ocpi_file":
		return fileFeed{cpoID: src.CPO.ID, base: src.CPO.OCPIBaseURL, token: src.Token}
	case "ocpi_file_gz":
		// OCPIBaseURL is the full locations .json.gz URL; the tariffs URL is the
		// same with "locations" → "tariffs" (NL DOT-NL / NDW naming).
		return gzFileFeed{cpoID: src.CPO.ID, locURL: src.CPO.OCPIBaseURL, token: src.Token}
	case "monta":
		// src.Token holds "clientId:clientSecret".
		id, secret, _ := strings.Cut(src.Token, ":")
		return montaFeed{cpoID: src.CPO.ID, country: "BE", client: monta.New(id, secret)}
	default:
		return ocpiFeed{cpoID: src.CPO.ID, client: src.Client()}
	}
}

// ---- OCPI ----

type ocpiFeed struct {
	cpoID  string
	client *ocpi.Client
}

func (f ocpiFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	locs, err := f.client.Locations(ctx)
	if err != nil {
		return nil, err
	}
	return normalize.FromOCPI(f.cpoID, locs, nil).Connectors, nil
}

func (f ocpiFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	locs, err := f.client.Locations(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Tariffs are optional: some CPOs (e.g. Tesla) expose only Locations and
	// return "module not supported" for tariffs. Skip the fetch when discovery
	// says the module is absent, so the price poll still ingests locations+status.
	var tars []ocpi.Tariff
	if f.client.HasModule(ctx, "tariffs") {
		if tars, err = f.client.Tariffs(ctx); err != nil {
			return nil, nil, err
		}
	}
	r := normalize.FromOCPI(f.cpoID, locs, tars)
	return r.Connectors, r.Tariffs, nil
}

// ---- DATEX II ----

type datexFeed struct {
	cpoID string
	url   string // feed URL with any auth token already folded in (see datexURL)
}

func (f datexFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := datex.Fetch(ctx, f.cpoID, f.url, "")
	return conns, err
}

func (f datexFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return datex.Fetch(ctx, f.cpoID, f.url, "")
}

// datexURL folds a NAP token into the feed URL as a ?token= query param — the
// auth scheme the Belgian DATEX II feeds (Eco-Movement) use. An empty token
// (open feeds such as Indigo, or a URL that already embeds its token) returns
// the URL unchanged.
func datexURL(base, token string) string {
	if token == "" {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "token=" + url.QueryEscape(token)
}

// ---- AFIR DATEX II table+status pairs over Bearer auth (e.g. EnergyVision) ----
// The static EnergyInfrastructureTablePublication carries identity (sites,
// refill points, connector type, power); the EnergyInfrastructureStatusPublication
// carries live availability + ad-hoc price updates. Both authenticate with an
// "Authorization: Bearer <key>" header.

type afirPairFeed struct {
	cpoID     string
	staticURL string
	statusURL string
	token     string
}

func newAFIRPairFeed(cpoID, baseURL, token string) afirPairFeed {
	st, dyn, _ := strings.Cut(baseURL, "|")
	return afirPairFeed{cpoID: cpoID, staticURL: strings.TrimSpace(st), statusURL: strings.TrimSpace(dyn), token: token}
}

func (f afirPairFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := f.load(ctx)
	return conns, err
}

func (f afirPairFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return f.load(ctx)
}

// afirStaticCache remembers the parsed table per feed URL. The publishers
// regenerate the table at most daily (EnergyVision serves it from a 24h cache
// and asks consumers to avoid unnecessary traffic), but the engine rebuilds the
// feed for every availability pass — without this, each 5-minute status poll
// would re-download a multi-MB table that hasn't changed.
var (
	afirStaticMu    sync.Mutex
	afirStaticCache = map[string]afirStaticEntry{}
)

const afirStaticTTL = time.Hour

type afirStaticEntry struct {
	fetched time.Time
	conns   []model.Connector
	tariffs map[string]model.Tariff
}

// load fetches (or reuses) the static table, then overlays the status
// publication's live availability + price updates.
func (f afirPairFeed) load(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	conns, tariffs, err := f.static(ctx)
	if err != nil {
		return nil, nil, err
	}
	if f.statusURL != "" {
		statusXML, serr := fetchBearerXML(ctx, f.statusURL, f.token)
		if serr != nil {
			return nil, nil, fmt.Errorf("afir status %s: %w", f.cpoID, serr)
		}
		st, perr := datex.ParseAFIRStatus(statusXML)
		if perr != nil {
			return nil, nil, fmt.Errorf("parse afir status: %w", perr)
		}
		for i := range conns {
			s, ok := st[conns[i].EVSEUID]
			if !ok {
				continue
			}
			if s.Status != "" {
				conns[i].EVSEStatus = s.Status
			}
			if s.Tariff != nil { // live price update wins
				if conns[i].TariffID == "" {
					conns[i].TariffID = conns[i].EVSEUID
				}
				tariffs[conns[i].TariffID] = *s.Tariff
			}
		}
	}
	// Safety net: drop coordinate-less rows so a publisher regression (e.g.
	// EnergyVision's first table revision shipped without any locations) puts
	// nothing on Null Island.
	kept := conns[:0]
	for _, c := range conns {
		if c.Lat != 0 || c.Lon != 0 {
			kept = append(kept, c)
		}
	}
	return kept, tariffs, nil
}

// static returns a fresh copy of the parsed table, from cache when younger than
// afirStaticTTL. Copies protect the cached slice/map from the status overlay.
func (f afirPairFeed) static(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	afirStaticMu.Lock()
	e, ok := afirStaticCache[f.staticURL]
	afirStaticMu.Unlock()
	if !ok || time.Since(e.fetched) > afirStaticTTL {
		staticXML, err := fetchBearerXML(ctx, f.staticURL, f.token)
		if err != nil {
			return nil, nil, fmt.Errorf("afir static %s: %w", f.cpoID, err)
		}
		conns, tariffs, err := datex.ParseAFIRStatic(f.cpoID, staticXML)
		if err != nil {
			return nil, nil, fmt.Errorf("parse afir static: %w", err)
		}
		e = afirStaticEntry{fetched: time.Now(), conns: conns, tariffs: tariffs}
		afirStaticMu.Lock()
		afirStaticCache[f.staticURL] = e
		afirStaticMu.Unlock()
	}
	conns := append([]model.Connector(nil), e.conns...)
	tariffs := make(map[string]model.Tariff, len(e.tariffs))
	for k, v := range e.tariffs {
		tariffs[k] = v
	}
	return conns, tariffs, nil
}

// fetchBearerXML GETs a DATEX II feed with Bearer auth. Generous timeout: NAP
// feeds are large and can be generated on demand.
func fetchBearerXML(ctx context.Context, feedURL, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/xml")
	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return body, nil
}

// ---- Static OCPI JSON files (e.g. Road) ----
// base hosts {base}/locations.json and (optionally) {base}/tariffs.json, each a
// bare OCPI array. Tariffs are best-effort: if absent, locations still ingest.

type fileFeed struct {
	cpoID string
	base  string
	token string
}

func (f fileFeed) urls() (locations, tariffs string) {
	b := strings.TrimRight(f.base, "/")
	return b + "/locations.json", b + "/tariffs.json"
}

func (f fileFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	locURL, _ := f.urls()
	locs, err := ocpi.FetchArray[ocpi.Location](ctx, locURL, f.token)
	if err != nil {
		return nil, err
	}
	return normalize.FromOCPI(f.cpoID, locs, nil).Connectors, nil
}

func (f fileFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	locURL, tarURL := f.urls()
	locs, err := ocpi.FetchArray[ocpi.Location](ctx, locURL, f.token)
	if err != nil {
		return nil, nil, err
	}
	tars, terr := ocpi.FetchArray[ocpi.Tariff](ctx, tarURL, f.token) // best-effort
	if terr != nil {
		tars = nil
	}
	r := normalize.FromOCPI(f.cpoID, locs, tars)
	return r.Connectors, r.Tariffs, nil
}

// ---- Location-only feeds (DE BNetzA CSV, FR IRVE GeoJSON) ----
// These national registries carry locations + power + plug but NO price and NO
// live status, so they share one adapter parameterised by a fetch function with
// the datex signature. Availability == Full (minus the always-empty tariffs).

type locFeed struct {
	cpoID string
	url   string
	token string
	fetch func(ctx context.Context, cpoID, url, token string) ([]model.Connector, map[string]model.Tariff, error)
}

func (f locFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := f.fetch(ctx, f.cpoID, f.url, f.token)
	return conns, err
}

func (f locFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return f.fetch(ctx, f.cpoID, f.url, f.token)
}

// ---- Gzipped static OCPI JSON files (e.g. NL DOT-NL / NDW) ----
// locURL is the full locations .json.gz URL; the tariffs file is the same URL
// with "locations" → "tariffs". FetchArray transparently gunzips. These feeds
// are large (NL ≈ 18 MB gz / ~150 MB JSON), so poll them sparingly.

type gzFileFeed struct {
	cpoID  string
	locURL string
	token  string
}

func (f gzFileFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	locs, err := ocpi.FetchArray[ocpi.Location](ctx, f.locURL, f.token)
	if err != nil {
		return nil, err
	}
	return normalize.FromOCPI(f.cpoID, locs, nil).Connectors, nil
}

func (f gzFileFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	locs, err := ocpi.FetchArray[ocpi.Location](ctx, f.locURL, f.token)
	if err != nil {
		return nil, nil, err
	}
	tarURL := strings.Replace(f.locURL, "locations", "tariffs", 1)
	tars, terr := ocpi.FetchArray[ocpi.Tariff](ctx, tarURL, f.token) // best-effort
	if terr != nil {
		tars = nil
	}
	r := normalize.FromOCPI(f.cpoID, locs, tars)
	return r.Connectors, r.Tariffs, nil
}

// ---- Monta Public API (open list + authed per-EVSE status) ----
// Locations come from the open list; live availability + ad-hoc price come from
// the per-EVSE status endpoint (Monta-party EVSEs only, rate-limited).

type montaFeed struct {
	cpoID   string
	country string
	client  *monta.Client
}

// Bulk ingestion is LOCATIONS ONLY: price + availability are per-EVSE and
// rate-limited (100 req/10 min), so fetching them for every Monta EVSE in a
// scheduled pass is infeasible (thousands of EVSEs ≈ hours). Live price comes
// from client.Status on demand (e.g. when a user opens a Monta charger).
func (f montaFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	return f.client.Locations(ctx, f.cpoID, f.country)
}

func (f montaFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	conns, err := f.client.Locations(ctx, f.cpoID, f.country)
	return conns, map[string]model.Tariff{}, err
}
