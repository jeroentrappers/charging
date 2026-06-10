package ingest

import (
	"context"
	"strings"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/model"
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
	case "ocpi_file":
		return fileFeed{cpoID: src.CPO.ID, base: src.CPO.OCPIBaseURL, token: src.Token}
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
