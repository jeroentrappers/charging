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
	"github.com/appmire/charging/internal/econtrol"
	"github.com/appmire/charging/internal/eipa"
	"github.com/appmire/charging/internal/fintraffic"
	"github.com/appmire/charging/internal/irve"
	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/monta"
	"github.com/appmire/charging/internal/normalize"
	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/oicp"
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
		// FR: "<static-geojson>" alone, or "<static-geojson>|<dynamic-csv>" to
		// overlay the national dynamic file's availability on the static base.
		if st, dyn, ok := strings.Cut(src.CPO.OCPIBaseURL, "|"); ok {
			return irveFeed{
				cpoID:      src.CPO.ID,
				staticURL:  strings.TrimSpace(st),
				dynamicURL: strings.TrimSpace(dyn),
				token:      src.Token,
			}
		}
		return locFeed{cpoID: src.CPO.ID, url: src.CPO.OCPIBaseURL, token: src.Token, fetch: irve.Fetch}
	case "oicp":
		// CH SFOE ich-tanke-strom: OICP JSON pair "<data>|<status>", open.
		return locFeed{cpoID: src.CPO.ID, url: src.CPO.OCPIBaseURL, token: src.Token, fetch: oicp.Fetch}
	case "fintraffic":
		// FI Fintraffic AFIR: OCPI-shaped JSON (locations + statuses + tariffs).
		return fintrafficFeed{cpoID: src.CPO.ID, client: fintraffic.New(src.CPO.OCPIBaseURL)}
	case "eipa":
		// PL UDT EIPA: five static JSON files + one dynamic, token in the path.
		return eipaFeed{cpoID: src.CPO.ID, client: eipa.New(src.CPO.OCPIBaseURL, src.Token)}
	case "econtrol":
		// AT E-Control: keyed REST crawl (operators -> stations -> points).
		return econtrolFeed{cpoID: src.CPO.ID, country: countryOf(src), client: econtrol.New(src.CPO.OCPIBaseURL, src.Token)}
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

// ---- FI Fintraffic AFIR (OCPI-shaped national JSON) ----
// Fintraffic republishes the Finnish CPOs' OCPI as one open national feed:
// locations, per-EVSE statuses and ad-hoc tariffs on three endpoints. The
// adapter hands us OCPI wire types, so normalization is the shared OCPI path.

type fintrafficFeed struct {
	cpoID  string
	client *fintraffic.Client
}

func (f fintrafficFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	locs, _, err := f.client.Snapshot(ctx, false) // no tariffs on the light path
	if err != nil {
		return nil, err
	}
	return normalize.FromOCPI(f.cpoID, locs, nil).Connectors, nil
}

func (f fintrafficFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	locs, tars, err := f.client.Snapshot(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	r := normalize.FromOCPI(f.cpoID, locs, tars)
	return r.Connectors, r.Tariffs, nil
}

// ---- FR IRVE static + national dynamic file ----
// The static consolidated GeoJSON carries identity (and is ~585 MB); the
// consolidated dynamic CSV (~8 MB) carries operational state per point de
// charge, joined on id_pdc_itinerance. Availability passes therefore reuse a
// cached parse of the static base and only re-fetch the small dynamic file; the
// price pass (monthly) always re-fetches the static and refreshes that cache.

type irveFeed struct {
	cpoID      string
	staticURL  string
	dynamicURL string
	token      string
}

// irveStaticTTL caps how long an availability pass may reuse the cached static
// base. The identity data changes slowly and the authoritative refresh is the
// monthly price pass, so a week keeps daily availability cheap (one 8 MB file)
// without letting the base drift for long.
const irveStaticTTL = 7 * 24 * time.Hour

var (
	irveStaticMu    sync.Mutex
	irveStaticCache = map[string]irveStaticEntry{}
)

type irveStaticEntry struct {
	fetched time.Time
	conns   []model.Connector
}

// Availability is the pass the dynamic file exists for, so a failure to read it
// fails the pass rather than quietly reporting the static base as unchanged.
func (f irveFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	return f.load(ctx, false, true)
}

// Full refreshes identity, which is worth ingesting on its own: a dynamic-file
// failure only costs the status overlay, so it does not fail the pass.
func (f irveFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	conns, err := f.load(ctx, true, false)
	// France publishes no structured price, so the tariff map stays empty.
	return conns, map[string]model.Tariff{}, err
}

// load returns the static base with the dynamic file's statuses overlaid.
func (f irveFeed) load(ctx context.Context, fresh, dynamicRequired bool) ([]model.Connector, error) {
	conns, err := f.static(ctx, fresh)
	if err != nil {
		return nil, err
	}
	if f.dynamicURL == "" {
		return conns, nil
	}
	statuses, err := irve.FetchDynamic(ctx, f.dynamicURL, f.token)
	if err != nil {
		if dynamicRequired {
			return nil, fmt.Errorf("irve dynamic %s: %w", f.cpoID, err)
		}
		return conns, nil
	}
	for i := range conns {
		if st, ok := statuses[conns[i].EVSEUID]; ok {
			conns[i].EVSEStatus = st
		}
	}
	return conns, nil
}

// static returns a copy of the parsed static base, re-fetching when asked for a
// fresh read or when the cached copy has aged past irveStaticTTL.
func (f irveFeed) static(ctx context.Context, fresh bool) ([]model.Connector, error) {
	irveStaticMu.Lock()
	e, ok := irveStaticCache[f.staticURL]
	irveStaticMu.Unlock()
	if fresh || !ok || time.Since(e.fetched) > irveStaticTTL {
		conns, _, err := irve.Fetch(ctx, f.cpoID, f.staticURL, f.token)
		if err != nil {
			return nil, fmt.Errorf("irve static %s: %w", f.cpoID, err)
		}
		e = irveStaticEntry{fetched: time.Now(), conns: conns}
		irveStaticMu.Lock()
		irveStaticCache[f.staticURL] = e
		irveStaticMu.Unlock()
	}
	return append([]model.Connector(nil), e.conns...), nil
}

// countryOf returns the source's ISO country code for feeds whose API is
// country-scoped, defaulting to AT for the Austrian register.
func countryOf(src source.Source) string {
	if c := strings.ToUpper(strings.TrimSpace(src.CPO.Country)); c != "" {
		return c
	}
	return "AT"
}

// ---- PL UDT EIPA (five static JSON files + one dynamic) ----
// EIPA enforces per-account download limits — 10/hour for the static files and
// 240/hour for the dynamic one — so the static half is parsed once and reused,
// and each availability pass costs a single dynamic fetch.

type eipaFeed struct {
	cpoID  string
	client *eipa.Client
}

// eipaStaticTTL keeps the static half well inside the 10/hour budget while still
// picking up new sites daily. A price pass always refreshes it.
const eipaStaticTTL = 12 * time.Hour

var (
	eipaStaticMu    sync.Mutex
	eipaStaticCache = map[string]eipaStaticEntry{}
)

type eipaStaticEntry struct {
	fetched time.Time
	static  *eipa.Static
}

func (f eipaFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := f.load(ctx, false)
	return conns, err
}

func (f eipaFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return f.load(ctx, true)
}

func (f eipaFeed) load(ctx context.Context, fresh bool) ([]model.Connector, map[string]model.Tariff, error) {
	st, err := f.static(ctx, fresh)
	if err != nil {
		return nil, nil, fmt.Errorf("eipa static %s: %w", f.cpoID, err)
	}
	dyn, err := f.client.Dynamic(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("eipa dynamic %s: %w", f.cpoID, err)
	}
	conns, tariffs := eipa.Build(f.cpoID, st, dyn, time.Now())
	return conns, tariffs, nil
}

func (f eipaFeed) static(ctx context.Context, fresh bool) (*eipa.Static, error) {
	key := f.cpoID
	eipaStaticMu.Lock()
	e, ok := eipaStaticCache[key]
	eipaStaticMu.Unlock()
	if fresh || !ok || time.Since(e.fetched) > eipaStaticTTL {
		st, err := f.client.Static(ctx)
		if err != nil {
			return nil, err
		}
		e = eipaStaticEntry{fetched: time.Now(), static: st}
		eipaStaticMu.Lock()
		eipaStaticCache[key] = e
		eipaStaticMu.Unlock()
	}
	return e.static, nil
}

// ---- AT E-Control (keyed REST crawl) ----
// There is no bulk export: a pass walks operators -> stations -> points, which is
// thousands of small requests. The operator/station tree changes slowly, so it is
// cached and only the (much larger) point level is re-read for availability.

type econtrolFeed struct {
	cpoID   string
	country string
	client  *econtrol.Client
}

// econtrolTreeTTL is how long the operator/station tree may be reused. It saves
// the ~1,100 operator requests on every availability pass; new sites appear with
// the daily price pass, which always re-walks it.
const econtrolTreeTTL = 24 * time.Hour

var (
	econtrolTreeMu    sync.Mutex
	econtrolTreeCache = map[string]econtrolTreeEntry{}
)

type econtrolTreeEntry struct {
	fetched  time.Time
	stations []econtrol.Station
}

func (f econtrolFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := f.load(ctx, false)
	return conns, err
}

func (f econtrolFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return f.load(ctx, true)
}

// load walks the register. Status and price both live on the point payload, so
// there is no cheaper path for availability than re-reading the points.
func (f econtrolFeed) load(ctx context.Context, fresh bool) ([]model.Connector, map[string]model.Tariff, error) {
	stations, err := f.stations(ctx, fresh)
	if err != nil {
		return nil, nil, fmt.Errorf("econtrol stations %s: %w", f.cpoID, err)
	}
	conns, tariffs, err := f.client.Points(ctx, f.cpoID, f.country, stations)
	if err != nil {
		return nil, nil, fmt.Errorf("econtrol points %s: %w", f.cpoID, err)
	}
	return conns, tariffs, nil
}

func (f econtrolFeed) stations(ctx context.Context, fresh bool) ([]econtrol.Station, error) {
	key := f.cpoID + "/" + f.country
	econtrolTreeMu.Lock()
	e, ok := econtrolTreeCache[key]
	econtrolTreeMu.Unlock()
	if fresh || !ok || time.Since(e.fetched) > econtrolTreeTTL {
		stations, err := f.client.Stations(ctx, f.country)
		if err != nil {
			return nil, err
		}
		e = econtrolTreeEntry{fetched: time.Now(), stations: stations}
		econtrolTreeMu.Lock()
		econtrolTreeCache[key] = e
		econtrolTreeMu.Unlock()
	}
	return e.stations, nil
}
