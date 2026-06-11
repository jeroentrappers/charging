package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// registerAdmin wires the control plane onto the OpenAPI document. Every
// operation is gated by a static ADMIN_TOKEN bearer (the adminToken security
// scheme) and consumed by the chargingctl CLI.
func (s *server) registerAdmin(api huma.API) {
	op := func(id, method, path, summary string) huma.Operation {
		return huma.Operation{
			OperationID: id, Method: method, Path: path, Summary: summary,
			Tags:        []string{"Admin"},
			Security:    []map[string][]string{{"adminToken": {}}},
			Middlewares: huma.Middlewares{s.adminGuard(api)},
			Hidden:      true, // control plane: functional but excluded from the public OpenAPI
		}
	}

	huma.Register(api, op("admin-list-sources", http.MethodGet, "/admin/sources", "List sources"), s.opAdminList)
	huma.Register(api, op("admin-upsert-source", http.MethodPost, "/admin/sources", "Create or update a source"), s.opAdminUpsert)
	huma.Register(api, op("admin-delete-source", http.MethodDelete, "/admin/sources/{id}", "Delete a source"), s.opAdminDelete)
	huma.Register(api, op("admin-enable-source", http.MethodPost, "/admin/sources/{id}/enable", "Enable a source"), s.opAdminEnable(true))
	huma.Register(api, op("admin-disable-source", http.MethodPost, "/admin/sources/{id}/disable", "Disable a source"), s.opAdminEnable(false))
	huma.Register(api, op("admin-set-token", http.MethodPut, "/admin/sources/{id}/token", "Set a source's API token"), s.opAdminSetToken)

	runOp := op("admin-run-ingest", http.MethodPost, "/admin/ingest/{id}/run", "Trigger an ingestion pass")
	runOp.DefaultStatus = http.StatusAccepted
	huma.Register(api, runOp, s.opAdminRun)

	huma.Register(api, op("admin-list-runs", http.MethodGet, "/admin/runs", "Recent ingestion runs"), s.opAdminRuns)
	huma.Register(api, op("admin-ocpi-register", http.MethodPost, "/admin/sources/{id}/ocpi/register", "Run the OCPI credentials handshake with a CPO"), s.opOCPIRegister)
}

// adminGuard gates the control plane with a static bearer token. If ADMIN_TOKEN
// is unset, the admin surface is disabled entirely (503) rather than open.
func (s *server) adminGuard(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if s.adminToken == "" {
			_ = huma.WriteErr(api, ctx, http.StatusServiceUnavailable, "admin disabled (set ADMIN_TOKEN)")
			return
		}
		got := strings.TrimPrefix(ctx.Header("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) != 1 {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next(ctx)
	}
}

// sourceView is the safe (token-free) representation of a source.
type sourceView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OCPIBaseURL string `json:"ocpi_base_url"`
	OCPIVersion string `json:"ocpi_version"`
	SourceType  string `json:"source_type"`
	PollCron    string `json:"poll_cron"`
	StatusCron  string `json:"status_cron"`
	TokenEnv    string `json:"token_env"`
	HasToken    bool   `json:"has_token"`
	Enabled     bool   `json:"enabled"`
}

func toView(c store.CPO) sourceView {
	return sourceView{
		ID: c.ID, Name: c.Name, OCPIBaseURL: c.OCPIBaseURL, OCPIVersion: c.OCPIVersion,
		SourceType: c.SourceType, PollCron: c.PollCron, StatusCron: c.StatusCron,
		TokenEnv: c.TokenEnv, HasToken: c.Token != "", Enabled: c.Enabled,
	}
}

type adminIDIn struct {
	ID string `path:"id" doc:"Source (CPO) id"`
}

// ---- list ----

type adminListOut struct {
	Body struct {
		Sources []sourceView `json:"sources"`
	}
}

func (s *server) opAdminList(ctx context.Context, _ *struct{}) (*adminListOut, error) {
	cpos, err := s.st.ListAllCPOs(ctx)
	if err != nil {
		s.log.Error("admin list sources", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	views := make([]sourceView, 0, len(cpos))
	for _, c := range cpos {
		views = append(views, toView(c))
	}
	out := &adminListOut{}
	out.Body.Sources = views
	return out, nil
}

// ---- upsert ----

type adminUpsertIn struct {
	Body store.CPO
}

type adminSourceOut struct {
	Body sourceView
}

func (s *server) opAdminUpsert(ctx context.Context, in *adminUpsertIn) (*adminSourceOut, error) {
	if in.Body.ID == "" || in.Body.OCPIBaseURL == "" {
		return nil, huma.Error400BadRequest("id and ocpi_base_url are required")
	}
	if err := s.st.UpsertCPO(ctx, in.Body); err != nil {
		s.log.Error("admin upsert source", "err", err)
		return nil, huma.Error500InternalServerError("upsert failed")
	}
	c, _, _ := s.st.GetCPO(ctx, in.Body.ID)
	return &adminSourceOut{Body: toView(c)}, nil
}

// ---- delete ----

type deletedOut struct {
	Body struct {
		Deleted bool `json:"deleted"`
	}
}

func (s *server) opAdminDelete(ctx context.Context, in *adminIDIn) (*deletedOut, error) {
	ok, err := s.st.DeleteCPO(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("delete failed")
	}
	if !ok {
		return nil, huma.Error404NotFound("source not found")
	}
	out := &deletedOut{}
	out.Body.Deleted = true
	return out, nil
}

// ---- enable / disable ----

type enabledOut struct {
	Body struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
}

func (s *server) opAdminEnable(enabled bool) func(context.Context, *adminIDIn) (*enabledOut, error) {
	return func(ctx context.Context, in *adminIDIn) (*enabledOut, error) {
		ok, err := s.st.SetEnabled(ctx, in.ID, enabled)
		if err != nil {
			return nil, huma.Error500InternalServerError("update failed")
		}
		if !ok {
			return nil, huma.Error404NotFound("source not found")
		}
		out := &enabledOut{}
		out.Body.ID = in.ID
		out.Body.Enabled = enabled
		return out, nil
	}
}

// ---- set token ----

type adminTokenIn struct {
	ID   string `path:"id" doc:"Source (CPO) id"`
	Body struct {
		Token string `json:"token"`
	}
}

type tokenOut struct {
	Body struct {
		ID       string `json:"id"`
		HasToken bool   `json:"has_token"`
	}
}

func (s *server) opAdminSetToken(ctx context.Context, in *adminTokenIn) (*tokenOut, error) {
	ok, err := s.st.SetToken(ctx, in.ID, in.Body.Token)
	if err != nil {
		return nil, huma.Error500InternalServerError("update failed")
	}
	if !ok {
		return nil, huma.Error404NotFound("source not found")
	}
	out := &tokenOut{}
	out.Body.ID = in.ID
	out.Body.HasToken = in.Body.Token != ""
	return out, nil
}

// ---- trigger ingest ----

type adminRunIn struct {
	ID   string `path:"id" doc:"Source (CPO) id"`
	Kind string `query:"kind" enum:"price,availability" default:"price" doc:"Ingestion pass to run"`
}

type runStartedOut struct {
	Body struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	}
}

// opAdminRun triggers an ingestion pass asynchronously and returns 202. The
// result is observable via GET /admin/runs.
func (s *server) opAdminRun(ctx context.Context, in *adminRunIn) (*runStartedOut, error) {
	kind := in.Kind
	if kind == "" {
		kind = ingest.KindPrice
	}
	c, found, err := s.st.GetCPO(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("lookup failed")
	}
	if !found {
		return nil, huma.Error404NotFound("source not found")
	}
	src := source.Resolve([]store.CPO{c})[0]
	if !src.Ready() {
		return nil, huma.Error400BadRequest("source needs a token; set one first")
	}

	// Run detached from the request so it survives the response.
	go func() {
		bg := context.Background()
		var err error
		if kind == ingest.KindAvailability {
			err = s.engine.RunAvailability(bg, src)
		} else {
			err = s.engine.RunPrices(bg, src)
		}
		if err != nil {
			s.log.Error("admin-triggered ingest", "cpo", in.ID, "kind", kind, "err", err)
		}
	}()

	out := &runStartedOut{}
	out.Body.ID = in.ID
	out.Body.Kind = kind
	out.Body.Status = "started"
	return out, nil
}

// ---- runs ----

type adminRunsIn struct {
	CPO   string `query:"cpo" doc:"Filter by source id"`
	Limit int    `query:"limit" doc:"Maximum runs to return"`
}

type runsOut struct {
	Body struct {
		Runs []store.Run `json:"runs"`
	}
}

func (s *server) opAdminRuns(ctx context.Context, in *adminRunsIn) (*runsOut, error) {
	runs, err := s.st.RecentRuns(ctx, in.CPO, in.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("query failed")
	}
	if runs == nil {
		runs = []store.Run{}
	}
	out := &runsOut{}
	out.Body.Runs = runs
	return out, nil
}
