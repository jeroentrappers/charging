package ocpi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchArray retrieves an OCPI module published as a static JSON file. Some NAP
// publishers (e.g. Road) expose a bare top-level array rather than the paginated
// {data:[...]} envelope, so this accepts either form. token is optional.
func FetchArray[T any](ctx context.Context, url, token string) ([]T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, snippet(body))
	}
	// Transparently decompress gzip-published files (e.g. NL DOT-NL .json.gz),
	// detected by the gzip magic bytes rather than the URL/headers.
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, zerr := gzip.NewReader(bytes.NewReader(body))
		if zerr != nil {
			return nil, fmt.Errorf("gunzip: %w", zerr)
		}
		defer zr.Close()
		body, err = io.ReadAll(io.LimitReader(zr, 1<<30)) // up to 1 GiB decompressed
		if err != nil {
			return nil, fmt.Errorf("gunzip read: %w", err)
		}
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' { // enveloped
		var env Envelope[T]
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return nil, fmt.Errorf("decode envelope: %w", err)
		}
		return env.Data, nil
	}
	var arr []T
	if err := json.Unmarshal(trimmed, &arr); err != nil {
		return nil, fmt.Errorf("decode array: %w", err)
	}
	return arr, nil
}
