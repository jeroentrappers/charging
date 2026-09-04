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
	// Idle fees: the feed states the grace threshold structurally
	// (timeBasedApplicability), and a tariff must never carry the free tier of
	// that schedule as a second TIME component.
	var withGrace, feeFromZero, zeroOnly, doubleTime int
	for _, tar := range tariffs {
		var times, paidTimes int
		priceable := false
		for _, el := range tar.Elements {
			for _, pc := range el.PriceComponents {
				switch {
				case pc.Type == "TIME":
					times++
					if pc.Price > 0 {
						paidTimes++
						if pc.AfterMinutes > 0 {
							withGrace++
						} else {
							feeFromZero++
						}
					}
				}
				if pc.Type == "ENERGY" || pc.Price > 0 {
					priceable = true
				}
			}
		}
		if times > 1 {
			doubleTime++
		}
		if !priceable {
			zeroOnly++
		}
	}
	t.Logf("connectors: %d (status %d, priced %d, power %d), tariffs: %d",
		len(conns), withStatus, priced, withPower, len(tariffs))
	t.Logf("idle fees: %d with a grace threshold, %d charged from minute 0", withGrace, feeFromZero)

	if withGrace == 0 {
		t.Error("no tariff carries a grace threshold — timeBasedApplicability not parsed?")
	}
	if doubleTime > 0 {
		t.Errorf("%d tariffs carry more than one TIME component; the evaluator only reads the first", doubleTime)
	}
	if zeroOnly > 0 {
		t.Errorf("%d tariffs price nothing (e.g. a €0 flat fee) and would rank as free chargers", zeroOnly)
	}

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
