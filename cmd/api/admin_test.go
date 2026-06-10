package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/appmire/charging/internal/ingest"
	"github.com/appmire/charging/internal/store"
)

func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://charging:charging@localhost:5433/charging?sslmode=disable"
}

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.New(context.Background(), dsn())
	if err != nil {
		t.Skipf("no database (%v)", err)
	}
	if _, err := st.Pool.Exec(context.Background(),
		`TRUNCATE tariff_version, charger_status, charger, ingest_run, cpo RESTART IDENTITY CASCADE`); err != nil {
		t.Skipf("schema not migrated (%v)", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &server{st: st, log: log, adminToken: "test-admin"}
	s.engine = ingest.NewEngine(st, log)
	srv := httptest.NewServer(s.routes("*"))
	t.Cleanup(func() { srv.Close(); st.Close() })
	return srv, st
}

func req(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	rq, _ := http.NewRequest(method, url, r)
	if token != "" {
		rq.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(rq)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAdmin_AuthAndSourceLifecycle(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	// TokenEnv names an (unset) env var, so the source genuinely needs a token
	// until one is set via the admin API — otherwise Ready() treats a tokenless
	// source as an open feed and the "run without token" step below would succeed.
	if err := st.SeedCPO(ctx, store.CPO{ID: "ev", Name: "EV", OCPIBaseURL: "http://x/", OCPIVersion: "2.1.1", TokenEnv: "EV_TEST_TOKEN_UNSET"}); err != nil {
		t.Fatal(err)
	}

	// No token -> 401.
	if r := req(t, "GET", srv.URL+"/admin/sources", "", nil); r.StatusCode != 401 {
		t.Fatalf("no token: want 401, got %d", r.StatusCode)
	}
	// Wrong token -> 401.
	if r := req(t, "GET", srv.URL+"/admin/sources", "nope", nil); r.StatusCode != 401 {
		t.Fatalf("wrong token: want 401, got %d", r.StatusCode)
	}

	// List with auth.
	r := req(t, "GET", srv.URL+"/admin/sources", "test-admin", nil)
	if r.StatusCode != 200 {
		t.Fatalf("list: want 200, got %d", r.StatusCode)
	}
	var listed struct {
		Sources []map[string]any `json:"sources"`
	}
	json.NewDecoder(r.Body).Decode(&listed)
	// Assert on our own source; other packages may share the test DB.
	var ev map[string]any
	for _, src := range listed.Sources {
		if src["id"] == "ev" {
			ev = src
		}
	}
	if ev == nil || ev["has_token"].(bool) {
		t.Fatalf("expected source 'ev' with no token, got: %+v", listed.Sources)
	}

	// Trigger before token -> 400.
	if r := req(t, "POST", srv.URL+"/admin/ingest/ev/run", "test-admin", nil); r.StatusCode != 400 {
		t.Fatalf("run without token: want 400, got %d", r.StatusCode)
	}

	// Set token.
	if r := req(t, "PUT", srv.URL+"/admin/sources/ev/token", "test-admin", map[string]string{"token": "k"}); r.StatusCode != 200 {
		t.Fatalf("set-token: want 200, got %d", r.StatusCode)
	}
	// Enable.
	if r := req(t, "POST", srv.URL+"/admin/sources/ev/enable", "test-admin", nil); r.StatusCode != 200 {
		t.Fatalf("enable: want 200, got %d", r.StatusCode)
	}

	// Verify via DB: enabled + token present, but token never leaked by the API.
	c, _, _ := st.GetCPO(ctx, "ev")
	if !c.Enabled || c.Token != "k" {
		t.Fatalf("expected enabled with token, got enabled=%v token=%q", c.Enabled, c.Token)
	}
	r = req(t, "GET", srv.URL+"/admin/sources", "test-admin", nil)
	raw, _ := io.ReadAll(r.Body)
	if bytes.Contains(raw, []byte(`"k"`)) || bytes.Contains(raw, []byte("\"token\"")) {
		t.Fatalf("admin list must not expose the token value: %s", raw)
	}

	// Delete.
	if r := req(t, "DELETE", srv.URL+"/admin/sources/ev", "test-admin", nil); r.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", r.StatusCode)
	}
	if _, found, _ := st.GetCPO(ctx, "ev"); found {
		t.Fatal("source should be gone after delete")
	}
}

func TestAdmin_DisabledWithoutToken(t *testing.T) {
	st, err := store.New(context.Background(), dsn())
	if err != nil {
		t.Skipf("no database (%v)", err)
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &server{st: st, log: log, adminToken: ""} // admin disabled
	s.engine = ingest.NewEngine(st, log)
	srv := httptest.NewServer(s.routes("*"))
	defer srv.Close()

	if r := req(t, "GET", srv.URL+"/admin/sources", "anything", nil); r.StatusCode != 503 {
		t.Fatalf("admin disabled: want 503, got %d", r.StatusCode)
	}
}
