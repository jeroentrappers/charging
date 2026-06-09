package pricing

import (
	"testing"
	"time"

	"github.com/appmire/charging/internal/model"
)

func f(v float64) *float64 { return &v }

func TestEvaluate_EnergyOnly(t *testing.T) {
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.45}}},
	}}
	got, ok := Comparable(tar, 11) // 30 kWh * 0.45
	if !ok {
		t.Fatal("expected ok")
	}
	if got != 13.5 {
		t.Fatalf("want 13.5, got %v", got)
	}
}

func TestEvaluate_EnergyFlatTime(t *testing.T) {
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{
			{Type: "ENERGY", Price: 0.40},
			{Type: "FLAT", Price: 0.50},
			{Type: "TIME", Price: 6.00}, // per hour
		}},
	}}
	// 30 kWh at 60 kW DC -> 0.5 h. energy 12.0 + flat 0.5 + time 3.0 = 15.5
	got, ok := Evaluate(tar, Session{KWh: 30, Power: 60, At: referenceTime()})
	if !ok || got != 15.5 {
		t.Fatalf("want 15.5, got %v ok=%v", got, ok)
	}
}

func TestEvaluate_TimeFavorsFasterCharger(t *testing.T) {
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{
			{Type: "ENERGY", Price: 0.30},
			{Type: "TIME", Price: 6.00},
		}},
	}}
	ac, _ := Evaluate(tar, Session{KWh: 30, Power: 11, At: referenceTime()})
	dc, _ := Evaluate(tar, Session{KWh: 30, Power: 60, At: referenceTime()})
	if !(dc < ac) {
		t.Fatalf("faster charger should cost less on time component: ac=%v dc=%v", ac, dc)
	}
}

func TestEvaluate_TimeOfDayRestriction(t *testing.T) {
	// Off-peak 0.20 between 22:00-06:00, peak 0.50 otherwise. First match wins,
	// so order the off-peak element first with its restriction.
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{
			PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.20}},
			Restrictions:    &model.Restrictions{StartTime: "22:00", EndTime: "06:00"},
		},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.50}}},
	}}
	day := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)   // noon -> peak
	night := time.Date(2024, 1, 3, 23, 0, 0, 0, time.UTC) // 23:00 -> off-peak
	if got, _ := Evaluate(tar, Session{KWh: 10, Power: 11, At: day}); got != 5.0 {
		t.Fatalf("peak: want 5.0, got %v", got)
	}
	if got, _ := Evaluate(tar, Session{KWh: 10, Power: 11, At: night}); got != 2.0 {
		t.Fatalf("off-peak: want 2.0, got %v", got)
	}
}

func TestEvaluate_KWhThreshold(t *testing.T) {
	// First 5 kWh free, then 0.40/kWh. With first-match-per-dimension we model
	// this as: cheap element restricted to max 5 kWh comes first.
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{
			PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.0}},
			Restrictions:    &model.Restrictions{MaxKWh: f(5)},
		},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.40}}},
	}}
	// Standard 30 kWh basket exceeds 5 -> falls through to 0.40 -> 12.0
	if got, _ := Comparable(tar, 11); got != 12.0 {
		t.Fatalf("want 12.0, got %v", got)
	}
}

func TestEvaluate_NoPriceableComponents(t *testing.T) {
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "PARKING_TIME", Price: 2.0}}},
	}}
	if _, ok := Comparable(tar, 11); ok {
		t.Fatal("parking-only tariff should yield no comparable charging price")
	}
}

func TestHash_OrderIndependent(t *testing.T) {
	a := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.40}}},
		{PriceComponents: []model.PriceComponent{{Type: "FLAT", Price: 0.50}}},
	}}
	b := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "FLAT", Price: 0.50}}},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.40}}},
	}}
	if a.Hash() != b.Hash() {
		t.Fatal("hash should be order-independent")
	}
	c := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.41}}},
	}}
	if a.Hash() == c.Hash() {
		t.Fatal("different prices must hash differently")
	}
}
