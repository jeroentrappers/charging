package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIfNoneMatch(t *testing.T) {
	etag := `W/"abc123"`
	cases := []struct {
		header string
		match  bool
	}{
		{"", false},
		{"*", true},
		{`W/"abc123"`, true},
		{`"abc123"`, true}, // weak comparison ignores W/
		{`W/"nope"`, false},
		{`W/"x", W/"abc123"`, true}, // list
		{`W/"x", "y"`, false},
	}
	for _, c := range cases {
		if got := ifNoneMatch(c.header, etag); got != c.match {
			t.Errorf("ifNoneMatch(%q, %q) = %v, want %v", c.header, etag, got, c.match)
		}
	}
}

func TestValidateCallbackURL(t *testing.T) {
	cases := []struct {
		name, url string
		ok        bool
	}{
		{"loopback", "http://127.0.0.1/hook", false},
		{"private-10", "http://10.0.0.5/hook", false},
		{"private-192", "https://192.168.1.1/hook", false},
		{"bad-scheme", "ftp://93.184.216.34/hook", false},
		{"no-host", "https:///hook", false},
		{"public-ip", "https://93.184.216.34/hook", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateCallbackURL(c.url)
			if c.ok && err != nil {
				t.Errorf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Errorf("want rejection for %q", c.url)
			}
		})
	}
}

func TestVerifyCallbackEchoes(t *testing.T) {
	s := &server{datexVerifyClient: &http.Client{Timeout: 2 * time.Second}}

	// Endpoint that echoes the challenge header in the body → verified.
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Header.Get("X-Datex-Hook-Challenge"))
	}))
	defer good.Close()
	if err := s.verifyCallback(t.Context(), good.URL, "", "abc123"); err != nil {
		t.Errorf("echo endpoint should verify: %v", err)
	}

	// Endpoint that does not echo → rejected.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "nope")
	}))
	defer bad.Close()
	if err := s.verifyCallback(t.Context(), bad.URL, "", "abc123"); err == nil {
		t.Error("non-echoing endpoint should be rejected")
	}

	// Endpoint that errors → rejected.
	err := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer err.Close()
	if e := s.verifyCallback(t.Context(), err.URL, "", "abc123"); e == nil {
		t.Error("5xx endpoint should be rejected")
	}
}
