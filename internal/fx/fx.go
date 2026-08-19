// Package fx converts foreign-currency prices to euros using the European
// Central Bank's daily reference rates.
//
// Most sources we ingest quote euros, but some national registers do not —
// Poland's EIPA publishes PLN — and the comparable price every ranking and
// statistic is built on is a euro amount. Without conversion a 2.19 PLN/kWh
// tariff would be ranked as if it were €2.19/kWh, more than four times its real
// cost, so a non-euro tariff is either converted here or left unranked.
//
// The ECB feed is open (no key), tiny (~1.5 KB) and published once per working
// day around 16:00 CET:
//
//	https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml
//
// Rates are quoted as units of the foreign currency per EUR (PLN 4.3190 = €1),
// so converting to euros divides.
package fx

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the ECB's daily reference-rate feed.
const DefaultURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"

// MaxAge is how stale a fetched rate set may be and still be used. The ECB
// publishes only on working days, so a Monday morning legitimately serves
// Friday's rates; a long weekend plus a holiday can reach four days. Beyond a
// week we would rather rank a charger as unpriced than price it off a rate that
// may have moved materially.
const MaxAge = 7 * 24 * time.Hour

// refreshEvery is how often a cache re-fetches on the happy path. The feed
// changes at most once a working day.
const refreshEvery = 6 * time.Hour

// retryAfter is how soon a cache retries after a failed fetch, so a transient
// outage doesn't leave conversion switched off for hours.
const retryAfter = 15 * time.Minute

// ---- ECB XML ----

type envelope struct {
	Days []struct {
		Time  string `xml:"time,attr"`
		Rates []struct {
			Currency string `xml:"currency,attr"`
			Rate     string `xml:"rate,attr"`
		} `xml:"Cube"`
	} `xml:"Cube>Cube"`
}

// Rates is one published set of reference rates, keyed by ISO 4217 code, in
// units of that currency per euro.
type Rates struct {
	Date  time.Time
	Rate  map[string]float64
	Fetch time.Time // when we retrieved it
}

// ToEUR converts an amount in the given currency to euros. ok is false when the
// currency is unknown or the rate set is too old to trust; callers must then
// treat the price as not comparable rather than assume parity.
func (r *Rates) ToEUR(amount float64, currency string) (float64, bool) {
	if r == nil {
		return 0, false
	}
	code := strings.ToUpper(strings.TrimSpace(currency))
	if code == "" || code == "EUR" {
		return amount, true
	}
	if time.Since(r.Date) > MaxAge {
		return 0, false
	}
	rate, ok := r.Rate[code]
	if !ok || rate <= 0 {
		return 0, false
	}
	return amount / rate, true
}

// Parse reads an ECB daily reference-rate document.
func Parse(data []byte) (*Rates, error) {
	var env envelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode ecb rates: %w", err)
	}
	if len(env.Days) == 0 {
		return nil, fmt.Errorf("ecb rates: no daily cube found")
	}
	day := env.Days[0]
	out := &Rates{Rate: make(map[string]float64, len(day.Rates)), Fetch: time.Now()}
	if t, err := time.Parse("2006-01-02", day.Time); err == nil {
		out.Date = t
	} else {
		return nil, fmt.Errorf("ecb rates: bad date %q", day.Time)
	}
	for _, c := range day.Rates {
		v, err := strconv.ParseFloat(c.Rate, 64)
		if err != nil || v <= 0 {
			continue // skip a malformed line rather than reject the whole set
		}
		out.Rate[strings.ToUpper(c.Currency)] = v
	}
	if len(out.Rate) == 0 {
		return nil, fmt.Errorf("ecb rates: no usable rates")
	}
	return out, nil
}

// Fetch retrieves and parses the feed at url (DefaultURL when empty).
func Fetch(ctx context.Context, url string) (*Rates, error) {
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/xml")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ecb rates: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return Parse(body)
}

// ---- cache ----

// Cache serves the latest rates, refreshing lazily in the caller's goroutine.
// It keeps the last good set on a failed refresh, so a feed outage degrades to
// slightly stale rates rather than to no conversion at all (until MaxAge).
//
// A nil *Cache is usable and simply never converts, which is what a deployment
// without FX configured wants.
type Cache struct {
	URL string

	mu       sync.Mutex
	rates    *Rates
	nextTry  time.Time
	lastErr  error
	OnUpdate func(*Rates, error) // optional hook for logging/metrics
}

// Rates returns the current set, fetching or refreshing when due. It returns nil
// when no set has ever been retrieved.
func (c *Cache) Rates(ctx context.Context) *Rates {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rates != nil && time.Now().Before(c.nextTry) {
		return c.rates
	}
	r, err := Fetch(ctx, c.URL)
	if c.OnUpdate != nil {
		c.OnUpdate(r, err)
	}
	if err != nil {
		c.lastErr = err
		c.nextTry = time.Now().Add(retryAfter)
		return c.rates // last good set, if any
	}
	c.rates, c.lastErr = r, nil
	c.nextTry = time.Now().Add(refreshEvery)
	return c.rates
}

// ToEUR converts through the cached rates.
func (c *Cache) ToEUR(ctx context.Context, amount float64, currency string) (float64, bool) {
	if c == nil {
		// No FX configured: euro amounts pass through, everything else is
		// explicitly not comparable.
		if code := strings.ToUpper(strings.TrimSpace(currency)); code == "" || code == "EUR" {
			return amount, true
		}
		return 0, false
	}
	return c.Rates(ctx).ToEUR(amount, currency)
}

// Err reports the last refresh error, if the most recent attempt failed.
func (c *Cache) Err() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}
