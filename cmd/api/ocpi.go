package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/normalize"
	"github.com/appmire/charging/internal/ocpi"
)

// ocpiSink maps pushed OCPI objects into the store via the shared ingest path.
// It keeps a small per-CPO cache of pushed locations + tariffs so a connector's
// tariff can be resolved regardless of push order. Periodic PULL (the scheduler)
// remains the authoritative copy; push provides real-time deltas between pulls.
type ocpiSink struct {
	engine *ingest.Engine
	log    *slog.Logger
	mu     sync.Mutex
	locs   map[string]map[string]ocpi.Location
	tars   map[string]map[string]ocpi.Tariff
}

func newOCPISink(engine *ingest.Engine, log *slog.Logger) *ocpiSink {
	return &ocpiSink{engine: engine, log: log, locs: map[string]map[string]ocpi.Location{}, tars: map[string]map[string]ocpi.Tariff{}}
}

func (k *ocpiSink) PutLocation(ctx context.Context, cpoID string, loc ocpi.Location) error {
	k.mu.Lock()
	if k.locs[cpoID] == nil {
		k.locs[cpoID] = map[string]ocpi.Location{}
	}
	k.locs[cpoID][loc.ID] = loc
	tars := tariffSlice(k.tars[cpoID])
	k.mu.Unlock()

	r := normalize.FromOCPI(cpoID, []ocpi.Location{loc}, tars)
	_, err := k.engine.IngestOCPI(ctx, r.Connectors, r.Tariffs)
	return err
}

func (k *ocpiSink) PutTariff(ctx context.Context, cpoID string, t ocpi.Tariff) error {
	k.mu.Lock()
	if k.tars[cpoID] == nil {
		k.tars[cpoID] = map[string]ocpi.Tariff{}
	}
	k.tars[cpoID][t.ID] = t
	var affected []ocpi.Location
	for _, loc := range k.locs[cpoID] {
		if locationUsesTariff(loc, t.ID) {
			affected = append(affected, loc)
		}
	}
	tars := tariffSlice(k.tars[cpoID])
	k.mu.Unlock()

	if len(affected) == 0 {
		return nil // cached; will link on the next location push or pull
	}
	r := normalize.FromOCPI(cpoID, affected, tars)
	_, err := k.engine.IngestOCPI(ctx, r.Connectors, r.Tariffs)
	return err
}

func (k *ocpiSink) DeleteLocation(_ context.Context, cpoID, id string) error {
	k.mu.Lock()
	delete(k.locs[cpoID], id)
	k.mu.Unlock()
	return nil // the next pull reconciles removals
}

func (k *ocpiSink) DeleteTariff(_ context.Context, cpoID, id string) error {
	k.mu.Lock()
	delete(k.tars[cpoID], id)
	k.mu.Unlock()
	return nil
}

func tariffSlice(m map[string]ocpi.Tariff) []ocpi.Tariff {
	out := make([]ocpi.Tariff, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	return out
}

func locationUsesTariff(loc ocpi.Location, tariffID string) bool {
	for _, e := range loc.EVSEs {
		for _, c := range e.Connectors {
			if c.Tariff() == tariffID {
				return true
			}
			for _, id := range c.TariffIDs {
				if id == tariffID {
					return true
				}
			}
		}
	}
	return false
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- admin: initiate the OCPI credentials handshake with a CPO ----

type ocpiRegisterIn struct {
	ID   string `path:"id"`
	Body struct {
		VersionsURL string `json:"versions_url" doc:"The CPO's OCPI versions endpoint URL"`
		Token       string `json:"token" doc:"Token A the CPO shared out-of-band"`
		Version     string `json:"version,omitempty" doc:"Preferred OCPI version (default 2.2.1)"`
	}
}

type ocpiRegisterOut struct {
	Body struct {
		ID        string            `json:"id"`
		Version   string            `json:"version"`
		BaseURL   string            `json:"base_url"`
		Endpoints map[string]string `json:"endpoints"`
		Enabled   bool              `json:"enabled"`
	}
}

func (s *server) opOCPIRegister(ctx context.Context, in *ocpiRegisterIn) (*ocpiRegisterOut, error) {
	if s.publicURL == "" {
		return nil, huma.Error400BadRequest("PUBLIC_URL is not configured; cannot advertise our OCPI endpoints")
	}
	if in.Body.VersionsURL == "" || in.Body.Token == "" {
		return nil, huma.Error400BadRequest("versions_url and token are required")
	}
	c, found, err := s.st.GetCPO(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("lookup failed")
	}
	if !found {
		return nil, huma.Error404NotFound("source not found; create it first")
	}
	version := in.Body.Version
	if version == "" {
		version = "2.2.1"
	}
	tokenB := randToken()
	ours := ocpi.Credentials{
		Token: tokenB,
		URL:   s.publicURL + "/ocpi/versions",
		Roles: []ocpi.CredentialsRole{{
			Role: "EMSP", PartyID: s.ocpiParty.PartyID, CountryCode: s.ocpiParty.CountryCode,
			BusinessDetails: ocpi.BusinessDetails{Name: s.ocpiParty.Name},
		}},
	}
	res, err := ocpi.Register(ctx, in.Body.VersionsURL, in.Body.Token, version, ours)
	if err != nil {
		s.log.Error("ocpi register", "cpo", in.ID, "err", err)
		return nil, huma.Error502BadGateway("handshake failed: " + err.Error())
	}

	// Persist: Token C (to call them), Token B (they push to us), their
	// version-details URL as the pull base, and enable as an OCPI source.
	if _, err := s.st.SetToken(ctx, in.ID, res.TokenC); err != nil {
		return nil, huma.Error500InternalServerError("store token failed")
	}
	if _, err := s.st.SetOCPIIncomingToken(ctx, in.ID, tokenB); err != nil {
		return nil, huma.Error500InternalServerError("store incoming token failed")
	}
	c.OCPIBaseURL = res.VersionDetailsURL
	c.OCPIVersion = res.Version
	c.SourceType = "ocpi"
	c.Enabled = true
	if err := s.st.UpsertCPO(ctx, c); err != nil {
		return nil, huma.Error500InternalServerError("update source failed")
	}

	out := &ocpiRegisterOut{}
	out.Body.ID = in.ID
	out.Body.Version = res.Version
	out.Body.BaseURL = res.VersionDetailsURL
	out.Body.Endpoints = res.Endpoints
	out.Body.Enabled = true
	return out, nil
}

// mountOCPI builds the eMSP server (handshake + push receiver) and returns its
// handler to mount at /ocpi.
func (s *server) ocpiHandler() http.Handler {
	srv := &ocpi.Server{
		Party:     s.ocpiParty,
		PublicURL: s.publicURL,
		Authorize: func(token string) (string, bool) {
			id, ok, _ := s.st.CPOByIncomingToken(context.Background(), token)
			return id, ok
		},
		Sink: newOCPISink(s.engine, s.log),
		Log:  s.log,
	}
	return srv.Routes()
}
