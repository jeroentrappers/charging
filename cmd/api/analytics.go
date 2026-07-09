package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/appmire/charging/internal/store"
)

// visitorHash anonymizes (clientID + ip) with a daily-rotated secret salt, so
// events can be counted per unique visitor within a day without ever storing or
// being able to reverse an IP. onlyIP=true hashes the IP alone (consumer key).
func (s *server) visitorHash(clientID, ip string, onlyIP bool) string {
	if ip == "" || ip == "unknown" {
		return ""
	}
	day := time.Now().UTC().Format("2006-01-02")
	seed := s.analyticsSalt + "|" + day + "|"
	if onlyIP {
		seed += ip
	} else {
		seed += clientID + "|" + ip
	}
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16])
}

// uaClass buckets a User-Agent without storing it.
func uaClass(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case l == "":
		return "other"
	case strings.Contains(l, "bot") || strings.Contains(l, "crawl") || strings.Contains(l, "spider") || strings.Contains(l, "slurp"):
		return "bot"
	case strings.Contains(l, "curl") || strings.Contains(l, "wget") || strings.Contains(l, "go-http") ||
		strings.Contains(l, "python") || strings.Contains(l, "okhttp") || strings.Contains(l, "java") || strings.Contains(l, "axios"):
		return "api"
	case strings.Contains(l, "mozilla"):
		return "browser"
	default:
		return "other"
	}
}

func refererHost(ref string) string {
	if ref == "" {
		return ""
	}
	ref = strings.TrimPrefix(strings.TrimPrefix(ref, "https://"), "http://")
	if i := strings.IndexAny(ref, "/?"); i >= 0 {
		ref = ref[:i]
	}
	return ref
}

// apiEvents maps a matched chi route to a product-analytics event name. Routes
// not in the map are recorded generically; noise routes return "".
var apiEvents = map[string]string{
	"GET /chargers/cheapest":           "search.cheapest",
	"GET /chargers/nearby":             "search.nearby",
	"GET /chargers/along-route":        "search.along_route",
	"GET /chargers":                    "explorer.list",
	"GET /chargers/{id}":               "charger.view",
	"GET /chargers/{id}/price-history": "charger.price_history",
	"GET /chargers/{id}/reports":       "charger.reports",
	"POST /chargers/{id}/reports":      "charger.report_submit",
	"GET /stats":                       "insights.view",
}

// recordAPI is a middleware that records one product-analytics event per matched
// API request, off the request path (the recorder never blocks). It skips infra,
// docs, admin, the export tree (recorded separately as feeds), and unmatched
// routes.
func (s *server) recordAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if s.analytics == nil {
			return
		}
		pattern := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			pattern = rc.RoutePattern()
		}
		if pattern == "" || skipAnalyticsPath(pattern) {
			return
		}
		key := r.Method + " " + pattern
		event := apiEvents[key]
		if event == "" {
			event = "api." + strings.Trim(strings.ReplaceAll(pattern, "/", "."), ".")
		}
		ip := clientIP(r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"))
		s.analytics.Record(store.AnalyticsEvent{
			Kind: "api", Event: event, Path: pattern, Status: sw.status,
			ClientHash:  s.visitorHash("", ip, false),
			IPHash:      s.visitorHash("", ip, true),
			UAClass:     uaClass(r.UserAgent()),
			RefererHost: refererHost(r.Referer()),
		})
	})
}

func skipAnalyticsPath(p string) bool {
	for _, pre := range []string{"/healthz", "/readyz", "/metrics", "/openapi", "/docs", "/admin", "/export", "/ocpi", "/mobilithek", "/events", "/schemas"} {
		if p == pre || strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// recordFeed records one event per served export/feed file (kind=feed), used for
// consumer/integrator analytics. Only real file fetches (200/304) are counted.
func (s *server) recordFeed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if s.analytics == nil || (sw.status != http.StatusOK && sw.status != http.StatusNotModified) {
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/export/")
		if name == "" || strings.HasSuffix(name, "/") {
			return // directory listing / index
		}
		ip := clientIP(r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"))
		s.analytics.Record(store.AnalyticsEvent{
			Kind: "feed", Event: "feed.pull", Path: "/export", Status: sw.status,
			IPHash:  s.visitorHash("", ip, true),
			UAClass: uaClass(r.UserAgent()),
			Props:   map[string]any{"format": feedFormat(name), "file": name},
		})
	})
}

// feedFormat classifies an export path into a coarse format label.
func feedFormat(name string) string {
	switch {
	case strings.HasPrefix(name, "datex/") && strings.HasSuffix(name, ".xml"):
		return "datex-xml"
	case strings.HasPrefix(name, "datex/") && strings.HasSuffix(name, ".json"):
		return "datex-json"
	case strings.HasPrefix(name, "ndjson/"):
		return "ndjson"
	case strings.HasPrefix(name, "geojson/"):
		return "geojson"
	case strings.HasPrefix(name, "ocpi/"):
		return "ocpi"
	case name == "availability.json":
		return "availability"
	case name == "index.json" || name == "index.html":
		return "manifest"
	default:
		return "other"
	}
}

// ---- first-party client events ----

type clientEventIn struct {
	XForwardedFor string `header:"X-Forwarded-For"`
	XRealIP       string `header:"X-Real-IP"`
	UserAgent     string `header:"User-Agent"`
	Referer       string `header:"Referer"`
	Body          struct {
		Event    string         `json:"event" doc:"Client event name (recorded under a 'client.' prefix), e.g. 'map_move', 'install'"`
		ClientID string         `json:"client_id,omitempty" doc:"Stable anonymous client id (never stored raw)"`
		Props    map[string]any `json:"props,omitempty"`
	}
}

type clientEventOut struct{}

func (s *server) registerAnalytics(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "post-event", Method: http.MethodPost, Path: "/events",
		Summary: "Record a first-party client analytics event", Tags: []string{"Analytics"},
		DefaultStatus: http.StatusNoContent,
	}, s.opClientEvent)
}

func (s *server) opClientEvent(ctx context.Context, in *clientEventIn) (*clientEventOut, error) {
	ip := clientIP(in.XForwardedFor, in.XRealIP)
	if !s.analyticsLimiter.allow(ip) {
		return nil, huma.Error429TooManyRequests("slow down")
	}
	name := strings.TrimSpace(in.Body.Event)
	if name == "" || len(name) > 64 {
		return nil, huma.Error400BadRequest("event required (max 64 chars)")
	}
	s.analytics.Record(store.AnalyticsEvent{
		Kind: "client", Event: "client." + name, Status: 0,
		ClientHash:  s.visitorHash(in.Body.ClientID, ip, false),
		IPHash:      s.visitorHash("", ip, true),
		UAClass:     uaClass(in.UserAgent),
		RefererHost: refererHost(in.Referer),
		Props:       in.Body.Props,
	})
	return &clientEventOut{}, nil
}

// ---- admin rollup ----

type analyticsAdminIn struct {
	Days int `query:"days" default:"7" doc:"Trailing window in days"`
}

type analyticsAdminOut struct {
	Body store.AnalyticsSummary
}

func (s *server) opAdminAnalytics(ctx context.Context, in *analyticsAdminIn) (*analyticsAdminOut, error) {
	days := in.Days
	if days <= 0 || days > 365 {
		days = 7
	}
	sum, err := s.st.Analytics(ctx, time.Duration(days)*24*time.Hour)
	if err != nil {
		return nil, huma.Error500InternalServerError("analytics query failed")
	}
	return &analyticsAdminOut{Body: sum}, nil
}
