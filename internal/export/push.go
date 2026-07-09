package export

// Outbound DATEX II push: the mirror of how the German sources reach us (they
// POST DATEX II to our /mobilithek/push webhook). After each snapshot we POST
// the freshly-written publications to configured subscriber callbacks. Each push
// carries the full current publication (self-superseding, stateless) gzip-
// compressed, with bearer and/or mutual-TLS auth like the inbound side.
//
// Pushes run in the background so a slow or failing subscriber never blocks or
// aborts snapshot generation; a dropped push self-heals on the next rotation.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/appmire/charging/internal/metrics"
	"github.com/appmire/charging/internal/store"
)

// PushTarget is one subscriber callback endpoint.
type PushTarget struct {
	URL      string // POST destination
	Token    string // optional bearer token
	Encoding string // "xml" (default) or "json"
}

// contentType/ext resolve the encoding to its media type and file suffix.
func (t PushTarget) contentType() string {
	if t.Encoding == "json" {
		return "application/json"
	}
	return "application/xml"
}

func (t PushTarget) ext() string {
	if t.Encoding == "json" {
		return ".json"
	}
	return ".xml"
}

// DatexPusher POSTs published DATEX II files to a set of subscriber callbacks:
// the static Targets from config plus any live self-service subscriptions in the
// store, resolved fresh on every push.
type DatexPusher struct {
	Targets  []PushTarget
	Store    *store.Store // optional; source of self-service subscriptions
	Client   *http.Client
	Log      *slog.Logger
	Attempts int              // total attempts per file; <=0 → 3
	Now      func() time.Time // injectable for tests
}

// NewDatexPusher builds a pusher. certFile/keyFile enable mutual-TLS (same PEM
// convention as the inbound Mobilithek client); caFile is optional. Returns nil
// only when there are neither static targets nor a store to read subscriptions
// from (push fully disabled).
func NewDatexPusher(targets []PushTarget, st *store.Store, log *slog.Logger, timeout time.Duration, certFile, keyFile, caFile string) (*DatexPusher, error) {
	if len(targets) == 0 && st == nil {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	tr := &http.Transport{}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("datex push client cert: %w", err)
		}
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
		if caFile != "" {
			pem, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("datex push CA: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("datex push CA: no certs in %s", caFile)
			}
			tlsCfg.RootCAs = pool
		}
		tr.TLSClientConfig = tlsCfg
	}
	return &DatexPusher{
		Targets: targets,
		Store:   st,
		Client:  &http.Client{Timeout: timeout, Transport: tr},
		Log:     log,
	}, nil
}

// resolveTargets returns the static config targets plus the current active
// self-service subscriptions. A store error just falls back to the static set.
func (p *DatexPusher) resolveTargets(ctx context.Context) []PushTarget {
	out := append([]PushTarget(nil), p.Targets...)
	if p.Store == nil {
		return out
	}
	subs, err := p.Store.ListActiveDatexSubscriptions(ctx)
	if err != nil {
		p.Log.Warn("datex push: list subscriptions", "err", err)
		return out
	}
	for _, s := range subs {
		out = append(out, PushTarget{URL: s.URL, Token: s.PushToken, Encoding: s.Encoding})
	}
	return out
}

func (p *DatexPusher) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

func (p *DatexPusher) attempts() int {
	if p.Attempts > 0 {
		return p.Attempts
	}
	return 3
}

// PushTable pushes the per-region table publications to every target, in the
// background. Each region file is a self-contained publication, so a target
// receives one POST per region.
func (p *DatexPusher) PushTable(ctx context.Context, dir string, regions []string) {
	go func() {
		for _, t := range p.resolveTargets(ctx) {
			for _, region := range regions {
				name := "datex/" + region + "-table" + t.ext()
				p.pushFile(ctx, t, "table", filepath.Join(dir, filepath.FromSlash(name)))
			}
		}
	}()
}

// PushStatus pushes the single status publication to every target, in the
// background.
func (p *DatexPusher) PushStatus(ctx context.Context, dir string) {
	go func() {
		for _, t := range p.resolveTargets(ctx) {
			name := "datex/status" + t.ext()
			p.pushFile(ctx, t, "status", filepath.Join(dir, filepath.FromSlash(name)))
		}
	}()
}

// pushFile reads a published file, gzips it, and POSTs it to the target with
// bounded retries. Records a metric and logs on final failure; never panics.
func (p *DatexPusher) pushFile(ctx context.Context, t PushTarget, kind, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		p.Log.Warn("datex push: read file", "path", path, "err", err)
		metrics.DatexPush(p.now(), t.URL, kind, "error")
		return
	}
	body := gzipBytes(raw)

	var lastErr error
	for attempt := 0; attempt < p.attempts(); attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				goto done
			case <-time.After(backoff):
			}
		}
		if lastErr = p.doPost(ctx, t, body); lastErr == nil {
			metrics.DatexPush(p.now(), t.URL, kind, "ok")
			return
		}
	}
done:
	p.Log.Warn("datex push failed", "target", t.URL, "kind", kind, "attempts", p.attempts(), "err", lastErr)
	metrics.DatexPush(p.now(), t.URL, kind, "error")
}

func (p *DatexPusher) doPost(ctx context.Context, t PushTarget, gzBody []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(gzBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", t.contentType())
	req.Header.Set("Content-Encoding", "gzip")
	if t.Token != "" {
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}
