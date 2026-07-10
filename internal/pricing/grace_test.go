package pricing

import (
	"testing"

	"github.com/appmire/charging/internal/model"
)

// A German-NAP ad-hoc tariff: 0.63 €/kWh + a 4.2 €/h blocking fee that only
// starts after 240 minutes. The standard basket must not pay the fee for a
// normal (sub-grace) session, but a genuinely long session pays the excess.
func TestEvaluate_TimeGrace(t *testing.T) {
	withGrace := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{{PriceComponents: []model.PriceComponent{
		{Type: "ENERGY", Price: 0.63},
		{Type: "TIME", Price: 4.2, AfterMinutes: 240},
	}}}}
	energyOnly := round4(0.63 * StandardKWh)

	// 30 kWh at 120 kW → 0.25 h (15 min) < 240 min grace → energy only.
	if cost, ok := Evaluate(withGrace, Session{KWh: StandardKWh, Power: 120, AvgPower: 120}); !ok || cost != energyOnly {
		t.Errorf("short session cost = %v (ok=%v), want %v (energy only)", cost, ok, energyOnly)
	}

	// 30 kWh at 5 kW → 6 h → billable past grace = 6 - 4 = 2 h → + 4.2*2.
	wantLong := round4(0.63*StandardKWh + 4.2*2)
	if cost, _ := Evaluate(withGrace, Session{KWh: StandardKWh, Power: 5, AvgPower: 5}); cost != wantLong {
		t.Errorf("long session cost = %v, want %v", cost, wantLong)
	}

	// Regression guard: with no grace, the same short session DOES pay the fee.
	noGrace := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{{PriceComponents: []model.PriceComponent{
		{Type: "ENERGY", Price: 0.63},
		{Type: "TIME", Price: 4.2},
	}}}}
	if cost, _ := Evaluate(noGrace, Session{KWh: StandardKWh, Power: 120, AvgPower: 120}); cost <= energyOnly {
		t.Errorf("no-grace short session = %v, should exceed energy-only %v", cost, energyOnly)
	}
}
