package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// defaultM2MBase is the Mobilithek M2M consumer-pull endpoint. The per-subscription
// OpenAPI spec Mobilithek generates points here (NOT mobilithek.info:8443, which is
// a different/legacy host that 400s). Override with MOBILITHEK_M2M_BASE.
const defaultM2MBase = "https://m2m.mobilithek.info/mobilithek/api/v1.0/subscription"

// ReconcileMobilithekStatic pulls one source's STATIC ("table") publication from
// the Mobilithek M2M pull API over mutual-TLS and ingests it via the same path as
// a consumer push (full snapshot: locations + ad-hoc tariffs). This backstops the
// delta-based push feed, which can drop the static snapshot entirely — leaving a
// source with only status deltas and zero chargers to attach them to.
//
// Status is intentionally NOT pulled here: it arrives in real time via push, and
// a slow status pull would only be a stale snapshot.
func (e *Engine) ReconcileMobilithekStatic(ctx context.Context, cpoID, staticID string) error {
	cl, err := mobilithekClient()
	if err != nil {
		return err
	}
	base := os.Getenv("MOBILITHEK_M2M_BASE")
	if base == "" {
		base = defaultM2MBase
	}
	u := base + "?subscriptionId=" + url.QueryEscape(staticID)
	body, err := fetchM2MGzipJSON(ctx, cl, u)
	if err != nil {
		return fmt.Errorf("mobilithek pull static %s (%s): %w", cpoID, staticID, err)
	}
	if len(body) == 0 { // 204 no new packet / 304 not modified
		e.Log.Info("mobilithek static reconcile: no new data", "cpo", cpoID)
		return nil
	}
	// Ingest in-process via the proven push path (sniffs JSON/XML, table → upsert
	// connectors + tariffs, routes to the cpo by publicationCreator). Bypasses the
	// push spool, so a large table doesn't compete with live status pushes there.
	kind, n, err := e.IngestMobilithekPush(ctx, body)
	if err != nil {
		return fmt.Errorf("mobilithek ingest static %s: %w", cpoID, err)
	}
	e.Log.Info("mobilithek static reconcile", "cpo", cpoID, "kind", kind, "changes", n, "bytes", len(body))
	return nil
}

// fetchM2MGzipJSON GETs an M2M subscription. The response is always gzip JSON
// (Content-Encoding: gzip); we set Accept-Encoding ourselves so Go won't
// auto-decompress, then gunzip by magic bytes. 204/304 return empty, not error.
func fetchM2MGzipJSON(ctx context.Context, cl *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, zerr := gzip.NewReader(bytes.NewReader(body))
		if zerr != nil {
			return nil, fmt.Errorf("gunzip: %w", zerr)
		}
		defer zr.Close()
		if body, err = io.ReadAll(io.LimitReader(zr, 1<<30)); err != nil {
			return nil, fmt.Errorf("gunzip read: %w", err)
		}
	}
	return body, nil
}
