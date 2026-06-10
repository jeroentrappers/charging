package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/source"
	"github.com/appmire/charging/internal/store"
)

// adminAuth gates the control plane with a static bearer token. If ADMIN_TOKEN
// is unset, the admin surface is disabled entirely (503) rather than open.
func (s *server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.adminToken == "" {
			writeErr(w, http.StatusServiceUnavailable, "admin disabled (set ADMIN_TOKEN)")
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) != 1 {
			writeErr(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next.ServeHTTP(w, r)
	})
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

func (s *server) adminListSources(w http.ResponseWriter, r *http.Request) {
	cpos, err := s.st.ListAllCPOs(r.Context())
	if err != nil {
		s.log.Error("admin list sources", "err", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	views := make([]sourceView, 0, len(cpos))
	for _, c := range cpos {
		views = append(views, toView(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": views})
}

func (s *server) adminUpsertSource(w http.ResponseWriter, r *http.Request) {
	var in store.CPO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.ID == "" || in.OCPIBaseURL == "" {
		writeErr(w, http.StatusBadRequest, "id and ocpi_base_url are required")
		return
	}
	if err := s.st.UpsertCPO(r.Context(), in); err != nil {
		s.log.Error("admin upsert source", "err", err)
		writeErr(w, http.StatusInternalServerError, "upsert failed")
		return
	}
	c, _, _ := s.st.GetCPO(r.Context(), in.ID)
	writeJSON(w, http.StatusOK, toView(c))
}

func (s *server) adminDeleteSource(w http.ResponseWriter, r *http.Request) {
	ok, err := s.st.DeleteCPO(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *server) adminEnable(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.st.SetEnabled(r.Context(), chi.URLParam(r, "id"), enabled)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "update failed")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "source not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": chi.URLParam(r, "id"), "enabled": enabled})
	}
}

func (s *server) adminSetToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ok, err := s.st.SetToken(r.Context(), chi.URLParam(r, "id"), body.Token)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": chi.URLParam(r, "id"), "has_token": body.Token != ""})
}

// adminRunIngest triggers an ingestion pass asynchronously and returns 202.
// kind=price (default) or availability. Result is observable via /admin/runs.
func (s *server) adminRunIngest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = ingest.KindPrice
	}
	if kind != ingest.KindPrice && kind != ingest.KindAvailability {
		writeErr(w, http.StatusBadRequest, "kind must be 'price' or 'availability'")
		return
	}
	c, found, err := s.st.GetCPO(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	src := source.Resolve([]store.CPO{c})[0]
	if !src.Ready() {
		writeErr(w, http.StatusBadRequest, "source needs a token; set one first")
		return
	}

	// Run detached from the request so it survives the response.
	go func() {
		ctx := context.Background()
		var err error
		if kind == ingest.KindAvailability {
			err = s.engine.RunAvailability(ctx, src)
		} else {
			err = s.engine.RunPrices(ctx, src)
		}
		if err != nil {
			s.log.Error("admin-triggered ingest", "cpo", id, "kind", kind, "err", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "kind": kind, "status": "started"})
}

func (s *server) adminRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.st.RecentRuns(r.Context(), r.URL.Query().Get("cpo"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}
