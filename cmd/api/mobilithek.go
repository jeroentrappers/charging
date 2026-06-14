package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"hash/fnv"
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
			// Name by a content hash so distinct payloads (per source/kind) land
			// in distinct files; identical re-pushes dedupe. (Last-Modified is
			// usually empty here, which made every push overwrite one file.)
			h := fnv.New32a()
			h.Write(body)
			name := fmt.Sprintf("push-%08x.json", h.Sum32())
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

// mobilithekPing is a GET/HEAD reachability check (no token) so the endpoint URL
// can be verified from a browser / the Mobilithek "test" tooling and broker.
func (s *server) mobilithekPing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enabled := s.mobilithekToken != ""
	_ = json.NewEncoder(w).Encode(map[string]any{"service": "mobilithek-push", "enabled": enabled, "at": time.Now().UTC()})
}

// statusRecorder captures the response status for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(c int) { sr.status = c; sr.ResponseWriter.WriteHeader(c) }

// logMobilithekRequest records EVERY request to the push endpoint — POST pushes,
// HEAD reachability probes, bad-token attempts, all of it — as one JSON line in
// MOBILITHEK_CAPTURE_DIR/requests.jsonl, so inbound traffic can be analysed
// later. Bodies of accepted pushes are saved alongside (see mobilithekPush).
// No-op unless a capture dir is configured.
func (s *server) logMobilithekRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.mobilithekCaptureDir == "" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		line, _ := json.Marshal(map[string]any{
			"ts":        start.UTC().Format(time.RFC3339Nano),
			"method":    r.Method,
			"status":    rec.status,
			"remote":    ip,
			"ua":        r.UserAgent(),
			"bytes":     r.ContentLength,
			"encoding":  r.Header.Get("Content-Encoding"),
			"has_token": r.URL.Query().Get("token") != "",
			"ms":        time.Since(start).Milliseconds(),
		})
		// O_APPEND keeps concurrent small writes atomic on local fs.
		if f, err := os.OpenFile(filepath.Join(s.mobilithekCaptureDir, "requests.jsonl"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	})
}

