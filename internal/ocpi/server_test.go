package ocpi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCPO stands in for a CPO's OCPI sender during the handshake.
func fakeCPO(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/versions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Envelope[Version]{Data: []Version{{Version: "2.2.1", URL: base + "/2.2.1"}}, StatusCode: 1000})
	})
	mux.HandleFunc("/2.2.1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ObjectEnvelope[VersionDetails]{StatusCode: 1000, Data: VersionDetails{Version: "2.2.1", Endpoints: []Endpoint{
			{Identifier: "credentials", Role: "RECEIVER", URL: base + "/2.2.1/credentials"},
			{Identifier: "locations", Role: "SENDER", URL: base + "/2.2.1/locations"},
			{Identifier: "tariffs", Role: "SENDER", URL: base + "/2.2.1/tariffs"},
		}}})
	})
	mux.HandleFunc("/2.2.1/credentials", func(w http.ResponseWriter, r *http.Request) {
		var ours Credentials
		json.NewDecoder(r.Body).Decode(&ours)
		if ours.Token == "" || len(ours.Roles) == 0 {
			t.Errorf("CPO received empty credentials: %+v", ours)
		}
		json.NewEncoder(w).Encode(ObjectEnvelope[Credentials]{StatusCode: 1000, Data: Credentials{Token: "TOKEN_C", URL: base + "/versions"}})
	})
	ts := httptest.NewServer(mux)
	base = ts.URL
	return ts
}

func TestRegister_Handshake(t *testing.T) {
	cpo := fakeCPO(t)
	defer cpo.Close()

	ours := Credentials{Token: "our-token-B", URL: "https://us/ocpi/versions", Roles: []CredentialsRole{{Role: "EMSP", PartyID: "APM", CountryCode: "BE"}}}
	res, err := Register(context.Background(), cpo.URL+"/versions", "TOKEN_A", "2.2.1", ours)
	if err != nil {
		t.Fatal(err)
	}
	if res.TokenC != "TOKEN_C" {
		t.Fatalf("want TOKEN_C, got %q", res.TokenC)
	}
	if res.Version != "2.2.1" || res.VersionDetailsURL != cpo.URL+"/2.2.1" {
		t.Fatalf("unexpected version/url: %+v", res)
	}
	if res.Endpoints["locations"] == "" || res.Endpoints["tariffs"] == "" {
		t.Fatalf("missing module endpoints: %+v", res.Endpoints)
	}
}

type stubSink struct {
	cpo string
	loc *Location
	tar *Tariff
}

func (s *stubSink) PutLocation(_ context.Context, cpo string, l Location) error { s.cpo, s.loc = cpo, &l; return nil }
func (s *stubSink) DeleteLocation(context.Context, string, string) error        { return nil }
func (s *stubSink) PutTariff(_ context.Context, cpo string, t Tariff) error     { s.cpo, s.tar = cpo, &t; return nil }
func (s *stubSink) DeleteTariff(context.Context, string, string) error          { return nil }

func TestServer_ReceiverAndAuth(t *testing.T) {
	sink := &stubSink{}
	srv := &Server{
		Party:     Party{CountryCode: "BE", PartyID: "APM", Name: "Appmire"},
		Authorize: func(tok string) (string, bool) { return "energyvision", tok == "secretB" },
		Sink:      sink,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()
	auth := "Token " + base64.StdEncoding.EncodeToString([]byte("secretB"))

	do := func(method, path, body, authHdr string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, stringsReader(body))
		if authHdr != "" {
			req.Header.Set("Authorization", authHdr)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// versions requires a valid token
	if r := do("GET", "/versions", "", ""); r.StatusCode != 401 {
		t.Fatalf("no token: want 401, got %d", r.StatusCode)
	}
	r := do("GET", "/versions", "", auth)
	if r.StatusCode != 200 {
		t.Fatalf("versions: want 200, got %d", r.StatusCode)
	}
	var vers Envelope[Version]
	json.NewDecoder(r.Body).Decode(&vers)
	if len(vers.Data) != 1 || vers.Data[0].Version != "2.2.1" {
		t.Fatalf("bad versions: %+v", vers.Data)
	}

	// push a location
	if r := do("PUT", "/2.2.1/locations/BE/CPO/L1", `{"id":"L1","evses":[{"uid":"E1","status":"AVAILABLE","connectors":[{"id":"1","standard":"IEC_62196_T2"}]}]}`, auth); r.StatusCode != 200 {
		t.Fatalf("put location: want 200, got %d", r.StatusCode)
	}
	if sink.loc == nil || sink.loc.ID != "L1" || sink.cpo != "energyvision" {
		t.Fatalf("location not delivered to sink: cpo=%q loc=%+v", sink.cpo, sink.loc)
	}

	// push a tariff
	if r := do("PUT", "/2.2.1/tariffs/BE/CPO/T1", `{"id":"T1","currency":"EUR","elements":[]}`, auth); r.StatusCode != 200 {
		t.Fatalf("put tariff: want 200, got %d", r.StatusCode)
	}
	if sink.tar == nil || sink.tar.ID != "T1" {
		t.Fatalf("tariff not delivered: %+v", sink.tar)
	}
}

func stringsReader(s string) io.Reader {
	if s == "" {
		return http.NoBody
	}
	return strings.NewReader(s)
}
