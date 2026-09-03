package ecomovement

import (
	"context"
	"os"
	"testing"
)

const liveURL = "https://nap-be.eco-movement.com/datex2/v1/locations"

// Live test against the Belgian NAP feed. Set ECOMOVEMENT_TOKEN. It walks the
// whole feed (~17 pages / ~104 MB), so it takes about a minute.
func TestLive_WalkBelgianNAP(t *testing.T) {
	token := os.Getenv("ECOMOVEMENT_TOKEN")
	if token == "" {
		t.Skip("set ECOMOVEMENT_TOKEN to run")
	}

	conns, tariffs, err := Fetch(context.Background(), "ecomovement", liveURL, token)
	if err != nil {
		t.Fatal(err)
	}

	var priced, withStatus, withPower int
	for _, c := range conns {
		if c.TariffID != "" {
			priced++
		}
		if c.EVSEStatus != "" {
			withStatus++
		}
		if c.PowerKW > 0 {
			withPower++
		}
	}
	t.Logf("connectors: %d (status %d, priced %d, power %d), tariffs: %d",
		len(conns), withStatus, priced, withPower, len(tariffs))

	// Measured 2026-09-03: 69,408 connectors, all with status, ~82% priced,
	// power on all but two. Thresholds are loose enough to tolerate the feed
	// growing or a network going quiet, tight enough to catch a broken parse.
	if len(conns) < 50_000 {
		t.Fatalf("connectors = %d, want >= 50k", len(conns))
	}
	if withStatus < len(conns)*9/10 {
		t.Errorf("connectors with status = %d of %d, want >= 90%%", withStatus, len(conns))
	}
	if priced < len(conns)/2 {
		t.Errorf("priced connectors = %d of %d, want >= 50%%", priced, len(conns))
	}
	if withPower < len(conns)*95/100 {
		t.Errorf("connectors with power = %d of %d, want >= 95%%", withPower, len(conns))
	}
	if len(tariffs) < 100 {
		t.Errorf("tariffs = %d, want >= 100", len(tariffs))
	}
}
