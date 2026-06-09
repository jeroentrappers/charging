package ingest

import (
	"context"

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
	if src.CPO.SourceType == "datex" {
		return datexFeed{cpoID: src.CPO.ID, url: src.CPO.OCPIBaseURL, token: src.Token}
	}
	return ocpiFeed{cpoID: src.CPO.ID, client: src.Client()}
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
