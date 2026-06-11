package ocpi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Party is our own OCPI identity (we act as an eMSP / data consumer).
type Party struct {
	CountryCode string
	PartyID     string
	Name        string
}

// Sink receives pushed objects (and deletions) from a registered CPO.
type Sink interface {
	PutLocation(ctx context.Context, cpoID string, loc Location) error
	DeleteLocation(ctx context.Context, cpoID, locationID string) error
	PutTariff(ctx context.Context, cpoID string, t Tariff) error
	DeleteTariff(ctx context.Context, cpoID, tariffID string) error
}

// Server implements the eMSP side of OCPI 2.2.1: the versions/credentials
// handshake endpoints and the Locations/Tariffs receiver interface (push).
type Server struct {
	Party     Party
	PublicURL string                            // public base incl. API prefix, e.g. https://host/api
	Authorize func(token string) (string, bool) // incoming token -> cpo id
	Sink      Sink
	Log       *slog.Logger
}

// Routes returns the OCPI sub-router (mount it at /ocpi).
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.auth)
	r.Get("/versions", s.versions)
	r.Get("/2.2.1", s.versionDetails)
	r.Get("/2.2.1/credentials", s.getCredentials)
	r.Post("/2.2.1/credentials", s.postCredentials)
	r.Delete("/2.2.1/credentials", s.deleteCredentials)

	// Receiver interface: a CPO pushes full objects. We accept location- and
	// tariff-level PUT/PATCH/DELETE (deeper EVSE/connector paths fold into the
	// location). object_id is the last path segment.
	r.Put("/2.2.1/locations/{cc}/{pid}/{lid}", s.putLocation)
	r.Patch("/2.2.1/locations/{cc}/{pid}/{lid}", s.putLocation)
	r.Delete("/2.2.1/locations/{cc}/{pid}/{lid}", s.deleteLocation)
	r.Put("/2.2.1/tariffs/{cc}/{pid}/{tid}", s.putTariff)
	r.Patch("/2.2.1/tariffs/{cc}/{pid}/{tid}", s.putTariff)
	r.Delete("/2.2.1/tariffs/{cc}/{pid}/{tid}", s.deleteTariff)
	return r
}

type ctxKey int

const cpoKey ctxKey = 0

// auth validates the OCPI "Token" credential (raw for 2.1.1, base64 for 2.2+)
// against the registered incoming tokens and stashes the matched cpo id.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Token "))
		if raw == "" {
			s.fail(w, http.StatusUnauthorized, 2001, "missing token")
			return
		}
		cands := []string{raw}
		if dec, err := base64.StdEncoding.DecodeString(raw); err == nil {
			cands = append(cands, string(dec))
		}
		for _, c := range cands {
			if id, ok := s.Authorize(c); ok {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cpoKey, id)))
				return
			}
		}
		s.fail(w, http.StatusUnauthorized, 2001, "invalid token")
	})
}

func (s *Server) versions(w http.ResponseWriter, r *http.Request) {
	s.ok(w, []Version{{Version: "2.2.1", URL: s.base(r) + "/2.2.1"}})
}

func (s *Server) versionDetails(w http.ResponseWriter, r *http.Request) {
	b := s.base(r)
	s.okObj(w, VersionDetails{Version: "2.2.1", Endpoints: []Endpoint{
		{Identifier: "credentials", Role: "RECEIVER", URL: b + "/2.2.1/credentials"},
		{Identifier: "locations", Role: "RECEIVER", URL: b + "/2.2.1/locations"},
		{Identifier: "tariffs", Role: "RECEIVER", URL: b + "/2.2.1/tariffs"},
	}})
}

func (s *Server) getCredentials(w http.ResponseWriter, r *http.Request) {
	s.okObj(w, s.ourCredentials("", r))
}

// postCredentials handles a CPO-initiated (re)registration: they present a token
// we issued out-of-band and their credentials; we ACK with ours.
func (s *Server) postCredentials(w http.ResponseWriter, r *http.Request) {
	var theirs Credentials
	_ = json.NewDecoder(r.Body).Decode(&theirs)
	s.okObj(w, s.ourCredentials("", r))
}

func (s *Server) deleteCredentials(w http.ResponseWriter, r *http.Request) {
	s.ok(w, []any{})
}

func (s *Server) ourCredentials(token string, r *http.Request) Credentials {
	return Credentials{
		Token: token,
		URL:   s.base(r) + "/versions",
		Roles: []CredentialsRole{{
			Role:            "EMSP",
			PartyID:         s.Party.PartyID,
			CountryCode:     s.Party.CountryCode,
			BusinessDetails: BusinessDetails{Name: s.Party.Name},
		}},
	}
}

func (s *Server) putLocation(w http.ResponseWriter, r *http.Request) {
	var loc Location
	if err := json.NewDecoder(r.Body).Decode(&loc); err != nil {
		s.fail(w, http.StatusBadRequest, 2001, "invalid body")
		return
	}
	if loc.ID == "" {
		loc.ID = chi.URLParam(r, "lid")
	}
	if err := s.Sink.PutLocation(r.Context(), cpoFrom(r), loc); err != nil {
		s.Log.Error("ocpi push location", "err", err)
		s.fail(w, http.StatusOK, 3000, "could not store")
		return
	}
	s.okObj(w, loc)
}

func (s *Server) deleteLocation(w http.ResponseWriter, r *http.Request) {
	_ = s.Sink.DeleteLocation(r.Context(), cpoFrom(r), chi.URLParam(r, "lid"))
	s.ok(w, []any{})
}

func (s *Server) putTariff(w http.ResponseWriter, r *http.Request) {
	var t Tariff
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		s.fail(w, http.StatusBadRequest, 2001, "invalid body")
		return
	}
	if t.ID == "" {
		t.ID = chi.URLParam(r, "tid")
	}
	if err := s.Sink.PutTariff(r.Context(), cpoFrom(r), t); err != nil {
		s.Log.Error("ocpi push tariff", "err", err)
		s.fail(w, http.StatusOK, 3000, "could not store")
		return
	}
	s.okObj(w, t)
}

func (s *Server) deleteTariff(w http.ResponseWriter, r *http.Request) {
	_ = s.Sink.DeleteTariff(r.Context(), cpoFrom(r), chi.URLParam(r, "tid"))
	s.ok(w, []any{})
}

// base derives our public OCPI base URL (so advertised URLs are absolute and
// reachable). Prefers the configured PublicURL; else derives from the request.
func (s *Server) base(r *http.Request) string {
	if s.PublicURL != "" {
		return strings.TrimRight(s.PublicURL, "/") + "/ocpi"
	}
	scheme := "https"
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	} else if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/ocpi"
}

func (s *Server) ok(w http.ResponseWriter, data any) {
	writeEnv(w, map[string]any{"data": data, "status_code": StatusSuccess, "status_message": "Success", "timestamp": time.Now().UTC()})
}
func (s *Server) okObj(w http.ResponseWriter, data any) {
	writeEnv(w, map[string]any{"data": data, "status_code": StatusSuccess, "status_message": "Success", "timestamp": time.Now().UTC()})
}
func (s *Server) fail(w http.ResponseWriter, httpCode, ocpiCode int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(map[string]any{"status_code": ocpiCode, "status_message": msg, "timestamp": time.Now().UTC()})
}

func writeEnv(w http.ResponseWriter, env map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(env)
}

func cpoFrom(r *http.Request) string {
	id, _ := r.Context().Value(cpoKey).(string)
	return id
}
