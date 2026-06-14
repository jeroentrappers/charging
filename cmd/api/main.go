// Command api serves the public-facing endpoints: find the cheapest available
// charger nearby, read a charger's ad-hoc price history, and a small admin
// control plane. The HTTP surface is described by an OpenAPI 3.1 document
// generated from the typed handlers (huma) so the spec can never drift; an
// interactive reference (Scalar) is served at /docs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/appmire/charging/internal/config"
	"github.com/appmire/charging/internal/export"
	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/metrics"
	"github.com/appmire/charging/internal/monta"
	"github.com/appmire/charging/internal/ocpi"
	"github.com/appmire/charging/internal/pricing"
	"github.com/appmire/charging/internal/routing"
	"github.com/appmire/charging/internal/store"
)

const apiVersion = "1.0.0"

type server struct {
	st              *store.Store
	log             *slog.Logger
	vehicle         pricing.Vehicle
	staleAfter      time.Duration
	priceStaleAfter time.Duration
	adminToken      string
	exportDir       string
	apiBasePath     string
	engine          *ingest.Engine
	live            *liveService
	reportLimiter   *ipLimiter
	publicURL       string
	ocpiParty       ocpi.Party
	router          routing.Router // optional; nil disables route/corridor search

	mobilithekToken      string // shared token for the inbound Mobilithek webhook
	mobilithekCaptureDir string // where to save pushed payloads (optional)
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local /healthz endpoint and exit (for container healthchecks)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	if *healthcheck {
		os.Exit(runHealthcheck(cfg.APIAddr))
	}

	st, err := store.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Error("connect db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	s := &server{
		st:  st,
		log: log,
		vehicle: pricing.Vehicle{
			UsableKWh:         cfg.VehicleUsableKWh,
			ConsumptionKWh100: cfg.VehicleConsumption,
		},
		staleAfter:           cfg.AvailabilityStaleAfter,
		priceStaleAfter:      cfg.PriceStaleAfter,
		adminToken:           cfg.AdminToken,
		exportDir:            cfg.ExportDir,
		apiBasePath:          cfg.APIBasePath,
		publicURL:            cfg.PublicURL,
		ocpiParty:            ocpi.Party{CountryCode: cfg.OCPICountry, PartyID: cfg.OCPIPartyID, Name: cfg.OCPIPartyName},
		mobilithekToken:      cfg.MobilithekPushToken,
		mobilithekCaptureDir: cfg.MobilithekCaptureDir,
	}
	if cfg.OSRMURL != "" {
		s.router = routing.New(cfg.OSRMURL)
		log.Info("route/corridor search enabled", "osrm", cfg.OSRMURL)
	}
	s.engine = ingest.NewEngine(st, log)
	s.engine.Vehicle = s.vehicle

	// On-demand live status: build a Monta client if creds are available (DB
	// token for the "monta" source, else MONTA_CREDS). Without them the
	// /chargers/{id}/live endpoint just serves the last stored status.
	var montaClient *monta.Client
	if creds := montaCreds(context.Background(), st); creds != "" {
		if id, secret, ok := strings.Cut(creds, ":"); ok {
			montaClient = monta.New(id, secret)
			log.Info("live status: Monta client configured")
		}
	}
	s.live = newLiveService(montaClient, s.vehicle, log, s.engine)
	// Public report submissions: per-IP token bucket (burst then ~1 / 3s).
	s.reportLimiter = newIPLimiter(3*time.Second, 8)

	// Bulk dataset export: regenerate the open static dumps on a schedule and
	// serve them from exportDir (see routes()).
	if cfg.ExportDir != "" {
		snap := &export.Snapshotter{
			Store: st, Dir: cfg.ExportDir, Log: log,
			FullEvery: cfg.ExportFullEvery, AvailEvery: cfg.ExportAvailEvery,
		}
		go snap.Run(context.Background())
	}

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           s.routes(cfg.CORSOrigins),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("api listening", "addr", cfg.APIAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// routes builds the HTTP handler: infra endpoints (health/ready/metrics) on
// plain chi, the documented API + admin control plane registered with huma so
// they appear in the generated OpenAPI 3.1 spec, and a Scalar docs page.
func (s *server) routes(corsOrigins string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(corsMiddleware(corsOrigins))

	// When the API is reverse-proxied under a path prefix (e.g. nginx maps
	// /api/ -> this server with the prefix stripped), the served paths stay at
	// root but the *advertised* spec server URL and the docs' spec link must
	// carry the public prefix. API_BASE_PATH sets it ("" for root mounting).
	basePath := strings.TrimRight(s.apiBasePath, "/")

	// Operational endpoints — deliberately outside the API contract.
	r.Get("/healthz", s.health)
	r.Get("/readyz", s.ready)
	r.Handle("/metrics", metrics.Handler())

	cfg := huma.DefaultConfig("Charging API", apiVersion)
	cfg.Info.Description = "Compare ad-hoc public EV charging prices across Belgian " +
		"operators: find the cheapest available charger nearby (with time-of-day and " +
		"user-defined sessions), read a charger's versioned price history, and browse " +
		"market statistics. Built on open AFIR/transportdata.be data.\n\n" +
		"The full dataset is also published as open, periodically-rotated static " +
		"dumps (NDJSON, GeoJSON, OCPI Locations+Tariffs), split by region — " +
		"[**browse the files**](" + basePath + "/export/) (human-readable) or see " +
		"[index.json](" + basePath + "/export/index.json) for the manifest, sizes, checksums and licence."
	cfg.DocsPath = "" // served below with Scalar instead of the bundled renderer
	if basePath != "" {
		cfg.OpenAPI.Servers = []*huma.Server{{URL: basePath, Description: "Public base path"}}
	}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"adminToken": {
			Type:        "http",
			Scheme:      "bearer",
			Description: "Static ADMIN_TOKEN bearer for the control plane.",
		},
	}
	api := humachi.New(r, cfg)
	s.registerPublic(api)
	s.registerReports(api)
	s.registerReportsAdmin(api, s.adminGuard(api))
	s.registerAdmin(api)

	r.Get("/docs", scalarDocs(basePath))

	// OCPI eMSP: credentials-handshake + push-receiver endpoints (own token auth).
	r.Mount("/ocpi", s.ocpiHandler())

	// Mobilithek consumer-push (webhook): the broker POSTs AFIR DATEX II JSON
	// here. Auth is the shared token in the URL (?token=…) — only Mobilithek,
	// which we hand the tokenized URL to, can post. GET is an open reachability
	// ping for the "test" tooling.
	// All three methods are logged (see logMobilithekRequest) so every inbound
	// request — pushes, HEAD probes, bad-token attempts — is recorded.
	r.With(s.logMobilithekRequest).Post("/mobilithek/push", s.mobilithekPush)
	r.With(s.logMobilithekRequest).Get("/mobilithek/push", s.mobilithekPing)
	// Mobilithek's broker HEAD-probes the callback for reachability; without a
	// HEAD route it gets 405, which gates pushing for that subscription (this is
	// why newer subscriptions like Audi never delivered).
	r.With(s.logMobilithekRequest).Head("/mobilithek/push", s.mobilithekPing)

	// Open bulk dataset dumps (static files regenerated on a schedule). Served
	// with gzip + short caching so a CDN can absorb "give me everything" load.
	if s.exportDir != "" {
		fs := http.StripPrefix("/export", http.FileServer(http.Dir(s.exportDir)))
		r.Route("/export", func(er chi.Router) {
			er.Use(middleware.Compress(5))
			er.Use(exportCacheControl)
			er.Handle("/*", fs)
			er.Handle("/", fs)
		})
	}
	return r
}

// exportCacheControl marks the static dumps publicly cacheable for a short
// window (the availability delta rotates ~every minute) and sets accurate
// content types for the .ndjson/.geojson extensions the file server doesn't
// know (http.ServeContent only fills Content-Type when it's unset).
func exportCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=30")
		switch {
		case strings.HasSuffix(r.URL.Path, ".ndjson"):
			w.Header().Set("Content-Type", "application/x-ndjson")
		case strings.HasSuffix(r.URL.Path, ".geojson"):
			w.Header().Set("Content-Type", "application/geo+json")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Pool.Ping(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /readyz — ready only if the DB is reachable and every enabled source has
// produced a recent successful availability and price ingest. Returns 503 with
// per-source detail otherwise. (No enabled sources => ready, with a note.)
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.st.Pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false, "reason": "database unavailable"})
		return
	}
	cpos, err := s.st.ListEnabledCPOs(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false, "reason": "cannot list sources"})
		return
	}

	availWindow := 2 * s.staleAfter
	sources := []map[string]any{}
	ready := true
	for _, c := range cpos {
		a := s.freshness(ctx, c.ID, ingest.KindAvailability, availWindow)
		p := s.freshness(ctx, c.ID, ingest.KindPrice, s.priceStaleAfter)
		if !a.OK || !p.OK {
			ready = false
		}
		sources = append(sources, map[string]any{"cpo": c.ID, "availability": a, "price": p})
	}

	code := http.StatusOK
	if !ready {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"ready":          ready,
		"enabled_source": len(cpos),
		"sources":        sources,
	})
}

type freshness struct {
	OK     bool       `json:"ok"`
	LastAt *time.Time `json:"last_success_at"`
}

// freshness reports whether the last successful run of kind is within window.
// A zero window disables the check (always ok).
func (s *server) freshness(ctx context.Context, cpoID, kind string, window time.Duration) freshness {
	t, found, err := s.st.LastSuccess(ctx, cpoID, kind)
	if err != nil || !found {
		return freshness{OK: window <= 0}
	}
	ok := window <= 0 || time.Since(t) <= window
	return freshness{OK: ok, LastAt: &t}
}

// runHealthcheck probes the local /healthz endpoint; used by the container
// healthcheck since the distroless image has no shell or curl.
func runHealthcheck(addr string) int {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// corsMiddleware allows browser clients on other origins to call the read API.
// origins is comma-separated; "*" allows any. Echoes a matching Origin so that
// credentialed requests still work if locked down later.
func corsMiddleware(origins string) func(http.Handler) http.Handler {
	allowAny := strings.TrimSpace(origins) == "*" || origins == ""
	allowed := map[string]bool{}
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			switch {
			case allowAny:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case origin != "" && allowed[origin]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "300")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// scalarDocs serves the interactive API reference (Scalar), pointed at the
// huma-generated OpenAPI document. basePath is the public prefix the API is
// reverse-proxied under ("" when mounted at root).
func scalarDocs(basePath string) http.HandlerFunc {
	html := `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Charging API reference</title>
  </head>
  <body>
    <script id="api-reference" data-url="` + basePath + `/openapi.yaml"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}
}
