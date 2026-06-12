package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// mobilithekPush receives Mobilithek consumer-push (webhook) deliveries: the
// broker POSTs an AFIR DATEX II JSON MessageContainer (gzip) here whenever the
// provider publishes. Auth is a shared token embedded in the registered
// callback URL (?token=…). For now this CAPTURES the payload (logs a snippet +
// optionally writes it to MOBILITHEK_CAPTURE_DIR) so we can build the JSON
// parser against real bytes; parsing + ingest is wired once that's ready.
func (s *server) mobilithekPush(w http.ResponseWriter, r *http.Request) {
	if s.mobilithekToken == "" {
		http.Error(w, "mobilithek push not configured", http.StatusServiceUnavailable)
		return
	}
	tok := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.mobilithekToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	gz := len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
	if gz {
		if zr, zerr := gzip.NewReader(bytes.NewReader(body)); zerr == nil {
			if b, rerr := io.ReadAll(io.LimitReader(zr, 1<<30)); rerr == nil {
				body = b
			}
			zr.Close()
		}
	}

	// Optionally persist the full payload (for parser development).
	saved := ""
	if s.mobilithekCaptureDir != "" {
		if err := os.MkdirAll(s.mobilithekCaptureDir, 0o755); err == nil {
			// Stable-ish name from the Last-Modified header (no Date.now reliance).
			name := "push-" + sanitize(r.Header.Get("Last-Modified")) + ".json"
			if name == "push-.json" {
				name = "push-latest.json"
			}
			p := filepath.Join(s.mobilithekCaptureDir, name)
			if werr := os.WriteFile(p, body, 0o644); werr == nil {
				saved = p
			}
		}
	}

	// Ack immediately, then parse + ingest asynchronously: a full static table
	// is ~1 MB / thousands of connectors and would otherwise block past
	// Mobilithek's webhook timeout. The engine serializes pushes internally.
	// Snapshot pushes re-send, so a dropped async ingest self-heals.
	go func(body []byte, gz bool, saved string) {
		kind, n, ierr := s.engine.IngestMobilithekPush(context.Background(), body)
		if ierr != nil {
			snippet := body
			if len(snippet) > 4000 {
				snippet = snippet[:4000]
			}
			s.log.Error("mobilithek push ingest", "bytes", len(body), "gzip", gz, "saved", saved, "err", ierr, "snippet", string(snippet))
		} else {
			s.log.Info("mobilithek push", "bytes", len(body), "gzip", gz, "kind", kind, "rows", n, "saved", saved)
		}
	}(body, gz, saved)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"status":"accepted","bytes":%d}`, len(body))
}

// mobilithekPing is a GET reachability check (no token) so the endpoint URL can
// be verified from a browser / the Mobilithek "test" tooling.
func (s *server) mobilithekPing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enabled := s.mobilithekToken != ""
	_ = json.NewEncoder(w).Encode(map[string]any{"service": "mobilithek-push", "enabled": enabled, "at": time.Now().UTC()})
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == ' ', c == ':', c == ',':
			out = append(out, '-')
		}
	}
	return string(out)
}
