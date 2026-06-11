package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/time/rate"

	"github.com/appmire/charging/internal/report"
)

// ---- rate limiting (per client IP) ----

type ipLimiter struct {
	mu    sync.Mutex
	m     map[string]*rate.Limiter
	every time.Duration
	burst int
}

func newIPLimiter(every time.Duration, burst int) *ipLimiter {
	return &ipLimiter{m: map[string]*rate.Limiter{}, every: every, burst: burst}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim := l.m[ip]
	if lim == nil {
		lim = rate.NewLimiter(rate.Every(l.every), l.burst)
		l.m[ip] = lim
	}
	return lim.Allow()
}

func clientIP(xff, xreal string) string {
	if xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xreal != "" {
		return strings.TrimSpace(xreal)
	}
	return "unknown"
}

// clientHash anonymizes (client id + IP) so reports can be deduped/counted per
// submitter without storing raw PII.
func clientHash(clientID, ip string) string {
	sum := sha256.Sum256([]byte(clientID + "|" + ip))
	return hex.EncodeToString(sum[:16])
}

// ---- registration ----

func (s *server) registerReports(api huma.API) {
	huma.Get(api, "/reports/types", s.opReportTypes, tag("Reports"),
		summary("List the structured report types"))
	huma.Get(api, "/chargers/{id}/reports", s.opGetReports, tag("Reports"),
		summary("Active community reports for a charger"))
	huma.Register(api, huma.Operation{
		OperationID: "post-charger-report", Method: http.MethodPost, Path: "/chargers/{id}/reports",
		Summary: "Submit a structured community report", Tags: []string{"Reports"},
		DefaultStatus: http.StatusCreated,
	}, s.opAddReport)
}

func (s *server) registerReportsAdmin(api huma.API, guard func(huma.Context, func(huma.Context))) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-clear-reports", Method: http.MethodDelete, Path: "/admin/chargers/{id}/reports",
		Summary: "Clear all community reports for a charger", Tags: []string{"Admin"},
		Security: []map[string][]string{{"adminToken": {}}}, Middlewares: huma.Middlewares{guard},
		Hidden: true, // control plane: excluded from the public OpenAPI
	}, s.opAdminClearReports)
}

// ---- types ----

type reportTypesOut struct {
	Body struct {
		Types []report.Type `json:"types"`
	}
}

func (s *server) opReportTypes(_ context.Context, _ *struct{}) (*reportTypesOut, error) {
	out := &reportTypesOut{}
	out.Body.Types = report.Types()
	return out, nil
}

// ---- read ----

type reportsOut struct {
	Body struct {
		ChargerID int64        `json:"charger_id"`
		Reports   []report.Agg `json:"reports"`
		Avoid     bool         `json:"avoid"`
	}
}

func (s *server) reportsBody(ctx context.Context, id int64) (*reportsOut, error) {
	raws, err := s.st.ReportsRaw(ctx, id)
	if err != nil {
		s.log.Error("reports: read", "err", err)
		return nil, huma.Error500InternalServerError("query failed")
	}
	aggs := report.Aggregate(time.Now().UTC(), raws)
	if aggs == nil {
		aggs = []report.Agg{}
	}
	out := &reportsOut{}
	out.Body.ChargerID = id
	out.Body.Reports = aggs
	out.Body.Avoid = report.Avoid(aggs)
	return out, nil
}

func (s *server) opGetReports(ctx context.Context, in *historyIn) (*reportsOut, error) {
	if ok, err := s.st.ChargerExists(ctx, in.ID); err != nil {
		return nil, huma.Error500InternalServerError("query failed")
	} else if !ok {
		return nil, huma.Error404NotFound("charger not found")
	}
	return s.reportsBody(ctx, in.ID)
}

// ---- submit ----

type reportIn struct {
	ID            int64  `path:"id"`
	XForwardedFor string `header:"X-Forwarded-For"`
	XRealIP       string `header:"X-Real-IP"`
	Body          struct {
		Type     string          `json:"type" doc:"Report type key (see GET /reports/types)"`
		Value    json.RawMessage `json:"value,omitempty" doc:"Optional typed value, e.g. {\"close\":\"22:00\"}, {\"kw\":50}, {\"price\":0.55}"`
		ClientID string          `json:"client_id,omitempty" doc:"Stable anonymous client id (for dedupe across IP changes)"`
	}
}

func (s *server) opAddReport(ctx context.Context, in *reportIn) (*reportsOut, error) {
	ip := clientIP(in.XForwardedFor, in.XRealIP)
	if !s.reportLimiter.allow(ip) {
		return nil, huma.Error429TooManyRequests("too many reports; please slow down")
	}
	if _, ok := report.Lookup(in.Body.Type); !ok {
		return nil, huma.Error400BadRequest("unknown report type; see GET /reports/types")
	}
	val, err := report.ValidateValue(in.Body.Type, in.Body.Value)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if ok, err := s.st.ChargerExists(ctx, in.ID); err != nil {
		return nil, huma.Error500InternalServerError("query failed")
	} else if !ok {
		return nil, huma.Error404NotFound("charger not found")
	}
	if err := s.st.AddReport(ctx, in.ID, in.Body.Type, clientHash(in.Body.ClientID, ip), val); err != nil {
		s.log.Error("reports: add", "err", err)
		return nil, huma.Error500InternalServerError("could not record report")
	}
	return s.reportsBody(ctx, in.ID)
}

// ---- admin ----

type adminChargerIn struct {
	ID int64 `path:"id"`
}

type clearedOut struct {
	Body struct {
		ChargerID int64 `json:"charger_id"`
		Deleted   int64 `json:"deleted"`
	}
}

func (s *server) opAdminClearReports(ctx context.Context, in *adminChargerIn) (*clearedOut, error) {
	n, err := s.st.DeleteReports(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("delete failed")
	}
	out := &clearedOut{}
	out.Body.ChargerID = in.ID
	out.Body.Deleted = n
	return out, nil
}
