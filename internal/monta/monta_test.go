package monta

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Live test against the Monta Public API. Set MONTA_CREDS="clientId:clientSecret".
func TestLive_ListAndStatus(t *testing.T) {
	creds := os.Getenv("MONTA_CREDS")
	if creds == "" {
		t.Skip("set MONTA_CREDS=clientId:clientSecret to run")
	}
	id, secret, _ := strings.Cut(creds, ":")
	c := New(id, secret)
	ctx := context.Background()

	conns, err := c.Locations(ctx, "monta", "BE")
	if err != nil {
		t.Fatal(err)
	}
	mon := 0
	for _, cn := range conns {
		if IsMonta(cn.EVSEUID) {
			mon++
		}
	}
	t.Logf("BE connectors: %d (Monta-party: %d)", len(conns), mon)
	if len(conns) < 100 || mon == 0 {
		t.Fatalf("expected many connectors incl. Monta-party, got %d / %d", len(conns), mon)
	}

	// Status for the first few Monta EVSEs: expect availability + (often) a price.
	checked, priced := 0, 0
	for _, cn := range conns {
		if !IsMonta(cn.EVSEUID) {
			continue
		}
		status, tar, err := c.Status(ctx, cn.EVSEUID)
		if err != nil {
			t.Fatalf("status %s: %v", cn.EVSEUID, err)
		}
		if status == "" {
			t.Fatalf("status %s: empty availability", cn.EVSEUID)
		}
		checked++
		if tar != nil {
			priced++
			t.Logf("  %s status=%s tariff: %s %+v", cn.EVSEUID, status, tar.Currency, tar.Elements[0].PriceComponents)
		} else {
			t.Logf("  %s status=%s (no ad-hoc price)", cn.EVSEUID, status)
		}
		if checked >= 4 {
			break
		}
	}
	if checked == 0 {
		t.Fatal("no Monta EVSE statuses checked")
	}
	t.Logf("checked %d statuses, %d with ad-hoc price", checked, priced)
}
