package ocpi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to a single CPO's OCPI sender interface (2.1.1 or 2.2.1).
type Client struct {
	BaseURL    string // version base, e.g. https://ocpi.energyvision.be/cpo/2.1.1/
	Token      string // OCPI auth token
	Version    string // "2.1.1" (default) or "2.2.1"
	HTTP       *http.Client
	PageLimit  int           // page size requested; 0 -> 100
	MaxRetries int           // per-request retries on 5xx/network errors; 0 -> 3
	RetryWait  time.Duration // base backoff; 0 -> 500ms

	discoverOnce sync.Once
	endpoints    map[string]string // module identifier -> URL (from discovery)
}

func New(baseURL, token string) *Client { return NewVersioned(baseURL, token, "2.1.1") }

func NewVersioned(baseURL, token, version string) *Client {
	if version == "" {
		version = "2.1.1"
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/") + "/",
		Token:      token,
		Version:    version,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		PageLimit:  100,
		MaxRetries: 3,
		RetryWait:  500 * time.Millisecond,
	}
}

func (c *Client) isV2_2() bool { return strings.HasPrefix(c.Version, "2.2") }

// authValue returns the Authorization header value. OCPI 2.2+ requires the
// token to be base64-encoded; 2.1.1 sends it raw. Both use the "Token" scheme.
func (c *Client) authValue() string {
	if c.Token == "" {
		return ""
	}
	tok := c.Token
	if c.isV2_2() {
		tok = base64.StdEncoding.EncodeToString([]byte(c.Token))
	}
	return "Token " + tok
}

// Locations fetches all pages of the Locations module.
func (c *Client) Locations(ctx context.Context) ([]Location, error) {
	return fetchAll[Location](ctx, c, "locations")
}

// Tariffs fetches all pages of the Tariffs module.
func (c *Client) Tariffs(ctx context.Context) ([]Tariff, error) {
	return fetchAll[Tariff](ctx, c, "tariffs")
}

// moduleURL resolves a module's base URL. For 2.2+ it first tries the OCPI
// version-details discovery (mapping identifiers to URLs); it always falls back
// to {BaseURL}{module}. The trailing-slash convention matches fetchPage.
func (c *Client) moduleURL(ctx context.Context, module string) string {
	if c.isV2_2() {
		c.discoverOnce.Do(func() { c.discover(ctx) })
		if u, ok := c.endpoints[module]; ok && u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return c.BaseURL + module // BaseURL has a trailing slash
}

// discover queries the version-details endpoint (the BaseURL) and caches the
// module endpoint URLs. Failures are non-fatal: callers fall back to {base}{module}.
func (c *Client) discover(ctx context.Context) {
	body, _, err := c.doWithRetry(ctx, strings.TrimRight(c.BaseURL, "/"))
	if err != nil {
		return
	}
	var env ObjectEnvelope[VersionDetails]
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.StatusCode != 0 && env.StatusCode != StatusSuccess {
		return
	}
	m := make(map[string]string, len(env.Data.Endpoints))
	for _, ep := range env.Data.Endpoints {
		// Prefer SENDER role (the data-provider interface) when roles are present.
		if existing, ok := m[ep.Identifier]; ok && existing != "" && ep.Role != "" && ep.Role != "SENDER" {
			continue
		}
		m[ep.Identifier] = ep.URL
	}
	if len(m) > 0 {
		c.endpoints = m
	}
}

// fetchAll pages through an OCPI module using offset/limit pagination,
// following the X-Total-Count header (and stopping when a short page returns).
func fetchAll[T any](ctx context.Context, c *Client, module string) ([]T, error) {
	limit := c.PageLimit
	if limit <= 0 {
		limit = 100
	}
	var out []T
	offset := 0
	for {
		page, total, err := fetchPage[T](ctx, c, module, offset, limit)
		if err != nil {
			return nil, fmt.Errorf("ocpi %s offset=%d: %w", module, offset, err)
		}
		out = append(out, page...)
		offset += len(page)
		// Stop when we've collected everything the server advertised, or the
		// server returned a short/empty page (no more data).
		if len(page) == 0 || len(page) < limit || (total >= 0 && offset >= total) {
			break
		}
	}
	return out, nil
}

func fetchPage[T any](ctx context.Context, c *Client, module string, offset, limit int) ([]T, int, error) {
	u, err := url.Parse(c.moduleURL(ctx, module))
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	body, hdr, err := c.doWithRetry(ctx, u.String())
	if err != nil {
		return nil, 0, err
	}

	var env Envelope[T]
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, 0, fmt.Errorf("decode envelope: %w", err)
	}
	if env.StatusCode != 0 && env.StatusCode != StatusSuccess {
		return nil, 0, fmt.Errorf("ocpi status %d: %s", env.StatusCode, env.StatusMsg)
	}

	total := -1
	if v := hdr.Get("X-Total-Count"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			total = n
		}
	}
	return env.Data, total, nil
}

func (c *Client) doWithRetry(ctx context.Context, urlStr string) ([]byte, http.Header, error) {
	retries := c.MaxRetries
	if retries <= 0 {
		retries = 3
	}
	wait := c.RetryWait
	if wait <= 0 {
		wait = 500 * time.Millisecond
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait * time.Duration(attempt)):
			}
		}
		body, hdr, retryable, err := c.do(ctx, urlStr)
		if err == nil {
			return body, hdr, nil
		}
		lastErr = err
		if !retryable {
			return nil, nil, err
		}
	}
	return nil, nil, fmt.Errorf("exhausted retries: %w", lastErr)
}

// do performs one request. The bool reports whether the error is retryable.
func (c *Client) do(ctx context.Context, urlStr string) ([]byte, http.Header, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, nil, false, err
	}
	if v := c.authValue(); v != "" {
		req.Header.Set("Authorization", v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, true, err // network errors are retryable
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, nil, true, err
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return body, resp.Header, false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, nil, true, fmt.Errorf("http %d: %s", resp.StatusCode, snippet(body))
	default:
		return nil, nil, false, fmt.Errorf("http %d: %s", resp.StatusCode, snippet(body))
	}
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
