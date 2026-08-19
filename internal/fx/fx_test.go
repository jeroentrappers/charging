package fx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func feed(date string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
	<gesmes:subject>Reference rates</gesmes:subject>
	<Cube>
		<Cube time='` + date + `'>
			<Cube currency='USD' rate='1.1576'/>
			<Cube currency='DKK' rate='7.4759'/>
			<Cube currency='PLN' rate='4.3190'/>
			<Cube currency='CHF' rate='0.9406'/>
			<Cube currency='BAD' rate='nonsense'/>
		</Cube>
	</Cube>
</gesmes:Envelope>`
}

func TestParseAndConvert(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	r, err := Parse([]byte(feed(today)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Rate["PLN"] != 4.3190 {
		t.Errorf("PLN rate = %v", r.Rate["PLN"])
	}
	// A malformed line is skipped, not fatal.
	if _, ok := r.Rate["BAD"]; ok {
		t.Error("malformed rate should be skipped")
	}

	// Rates are units per euro, so converting to euros divides.
	if got, ok := r.ToEUR(4.3190, "PLN"); !ok || !closeTo(got, 1) {
		t.Errorf("4.3190 PLN = %v (%v), want 1 EUR", got, ok)
	}
	if got, ok := r.ToEUR(2.19, "PLN"); !ok || !closeTo(got, 0.507) {
		t.Errorf("2.19 PLN = %v, want ~0.507 EUR", got)
	}
	// Euros pass through; unknown currencies are refused.
	for _, c := range []string{"EUR", "eur", ""} {
		if got, ok := r.ToEUR(5, c); !ok || got != 5 {
			t.Errorf("ToEUR(5, %q) = %v, %v", c, got, ok)
		}
	}
	if _, ok := r.ToEUR(5, "XYZ"); ok {
		t.Error("unknown currency should not convert")
	}
}

// A rate set older than MaxAge must stop converting rather than price a charger
// off a rate that may have moved.
func TestStaleRatesRefuseConversion(t *testing.T) {
	old := time.Now().Add(-MaxAge - 24*time.Hour).Format("2006-01-02")
	r, err := Parse([]byte(feed(old)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.ToEUR(4.319, "PLN"); ok {
		t.Error("stale rates should refuse a foreign conversion")
	}
	// Euro amounts never depend on the feed's freshness.
	if got, ok := r.ToEUR(5, "EUR"); !ok || got != 5 {
		t.Errorf("EUR should pass through stale rates, got %v %v", got, ok)
	}
}

func TestNilCacheConvertsOnlyEuros(t *testing.T) {
	var c *Cache
	if got, ok := c.ToEUR(context.Background(), 7, "EUR"); !ok || got != 7 {
		t.Errorf("nil cache should pass euros through, got %v %v", got, ok)
	}
	if _, ok := c.ToEUR(context.Background(), 7, "PLN"); ok {
		t.Error("nil cache must not convert foreign currency")
	}
}

// The cache fetches once, reuses the result, and keeps serving the last good set
// when a later refresh fails.
func TestCacheReusesAndSurvivesFailure(t *testing.T) {
	var hits int32
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(feed(time.Now().Format("2006-01-02"))))
	}))
	defer srv.Close()

	c := &Cache{URL: srv.URL}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, ok := c.ToEUR(ctx, 4.319, "PLN"); !ok {
			t.Fatalf("conversion %d failed", i)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("fetched %d times for 3 conversions, want 1", got)
	}

	// Force a refresh that fails: the last good set must still convert.
	fail.Store(true)
	c.mu.Lock()
	c.nextTry = time.Now().Add(-time.Minute)
	c.mu.Unlock()
	if _, ok := c.ToEUR(ctx, 4.319, "PLN"); !ok {
		t.Error("a failed refresh should fall back to the last good rates")
	}
	if c.Err() == nil {
		t.Error("want the refresh error to be reported")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not xml")); err == nil {
		t.Error("want an error for non-XML input")
	}
	if _, err := Parse([]byte(`<gesmes:Envelope xmlns:gesmes="x"></gesmes:Envelope>`)); err == nil {
		t.Error("want an error when no daily cube is present")
	}
}

func closeTo(a, b float64) bool { return a-b < 1e-3 && b-a < 1e-3 }
