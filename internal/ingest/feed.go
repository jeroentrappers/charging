package ingest

import (
	"context"
	"strings"

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
		return datexFeed{cpoID: src.CPO.ID, url: src.CPO.OCPIBaseURL, token: src.Token}
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
	tars, err := f.client.Tariffs(ctx)
	if err != nil {
		return nil, nil, err
	}
	r := normalize.FromOCPI(f.cpoID, locs, tars)
	return r.Connectors, r.Tariffs, nil
}

// ---- DATEX II ----

type datexFeed struct {
	cpoID string
	url   string
	token string
}

func (f datexFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := datex.Fetch(ctx, f.cpoID, f.url, f.token)
	return conns, err
}

func (f datexFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return datex.Fetch(ctx, f.cpoID, f.url, f.token)
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
