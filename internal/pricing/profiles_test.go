package pricing

import (
	"math"
	"testing"
	"time"

	"github.com/appmire/charging/internal/model"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.05 }

func tariffEFT() model.Tariff {
	return model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{
			{Type: "ENERGY", Price: 0.40},
			{Type: "FLAT", Price: 0.25},
			{Type: "TIME", Price: 6.00},
		}},
	}}
}

func TestProfiles_CountAndMeteredEnergy(t *testing.T) {
	ps := Profiles(DefaultVehicle)
	if len(ps) != 10 {
		t.Fatalf("want 10 profiles, got %d", len(ps))
	}
	by := map[string]ResolvedProfile{}
	for _, p := range ps {
		by[p.Key] = p
	}
	// 100 km AC: 18 kWh / 0.89 = 20.22
	if got := by["topup100_ac11"].MeteredKW; !approx(got, 20.22) {
		t.Fatalf("topup100_ac11 metered: want ~20.22, got %v", got)
	}
	// 10–80% (42 kWh) AC: /0.89 = 47.19 ; DC: /0.94 = 44.68
	if got := by["charge1080_ac11"].MeteredKW; !approx(got, 47.19) {
		t.Fatalf("charge1080_ac11 metered: want ~47.19, got %v", got)
	}
	if got := by["charge1080_dc150"].MeteredKW; !approx(got, 44.68) {
		t.Fatalf("charge1080_dc150 metered: want ~44.68, got %v", got)
	}
}

func TestProfiles_ConfigurableVehicle(t *testing.T) {
	big := Vehicle{UsableKWh: 80, ConsumptionKWh100: 20}
	var small, large float64
	for _, p := range Profiles(DefaultVehicle) {
		if p.Key == "charge1080_ac11" {
			small = p.MeteredKW
		}
	}
	for _, p := range Profiles(big) {
		if p.Key == "charge1080_ac11" {
			large = p.MeteredKW
		}
	}
	if !(large > small) {
		t.Fatalf("larger battery should need more energy: small=%v large=%v", small, large)
	}
}

func TestAllPrices_CapabilityFilter(t *testing.T) {
	tar := tariffEFT()

	// A DC 150 kW charger: DC tiers up to 150 only, no AC, no DC300.
	dc := AllPrices(tar, 150, model.CurrentDC, DefaultVehicle)
	mustHave := []string{"topup100_dc150", "charge1080_dc150"}
	mustNot := []string{"topup100_dc300", "charge1080_dc300", "charge1080_ac11", "urban_ac22"}
	for _, k := range mustHave {
		if _, ok := dc[k]; !ok {
			t.Fatalf("DC150 charger should price %s", k)
		}
	}
	for _, k := range mustNot {
		if _, ok := dc[k]; ok {
			t.Fatalf("DC150 charger should NOT price %s", k)
		}
	}

	// An AC 22 kW charger: AC tiers up to 22, no DC.
	ac := AllPrices(tar, 22, model.CurrentAC, DefaultVehicle)
	for _, k := range []string{"charge1080_ac11", "charge1080_ac22", "urban_ac22", "overnight_ac11"} {
		if _, ok := ac[k]; !ok {
			t.Fatalf("AC22 charger should price %s", k)
		}
	}
	for k := range ac {
		if k[:2] == "to" && (k == "topup100_dc150" || k == "topup100_dc300") {
			t.Fatalf("AC charger leaked a DC profile: %s", k)
		}
	}
}

func TestAllPrices_DCFasterThanACOnTime(t *testing.T) {
	tar := tariffEFT()
	dc := AllPrices(tar, 150, model.CurrentDC, DefaultVehicle)["charge1080_dc150"]
	// Same energy basket on AC costs more because the TIME component runs longer.
	acTar := tar
	acPrice, _ := Evaluate(acTar, Session{KWh: 44.68, Power: 11, AvgPower: 11, At: referenceTime()})
	if !(dc < acPrice) {
		t.Fatalf("DC (taper avg) should cost less on time than slow AC: dc=%v ac=%v", dc, acPrice)
	}
}

// TestHeadlineAt_TimeOfDayChangesRanking is the core check behind time-aware
// sorting: charger A is cheap by day / dear by night, charger B the reverse, so
// the cheaper of the two flips between peak and off-peak. The API sorts on
// exactly this HeadlineAt value, so the result order flips with it.
func TestHeadlineAt_TimeOfDayChangesRanking(t *testing.T) {
	// A: 0.25 day, 0.60 night (22:00–06:00).
	a := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{
			PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.60}},
			Restrictions:    &model.Restrictions{StartTime: "22:00", EndTime: "06:00"},
		},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.25}}},
	}}
	// B: 0.45 day, 0.20 night (cheap off-peak).
	b := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{
			PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.20}},
			Restrictions:    &model.Restrictions{StartTime: "22:00", EndTime: "06:00"},
		},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.45}}},
	}}

	noon := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	night := time.Date(2024, 1, 3, 23, 0, 0, 0, time.UTC)

	aDay, _ := HeadlineAt(a, 11, model.CurrentAC, DefaultVehicle, noon)
	bDay, _ := HeadlineAt(b, 11, model.CurrentAC, DefaultVehicle, noon)
	if !(aDay < bDay) {
		t.Fatalf("by day A should be cheaper: a=%v b=%v", aDay, bDay)
	}

	aNight, _ := HeadlineAt(a, 11, model.CurrentAC, DefaultVehicle, night)
	bNight, _ := HeadlineAt(b, 11, model.CurrentAC, DefaultVehicle, night)
	if !(bNight < aNight) {
		t.Fatalf("at night B should be cheaper (ranking flips): a=%v b=%v", aNight, bNight)
	}
}

func TestSessionPriceAt_TimeOfDay(t *testing.T) {
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{
			PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.20}},
			Restrictions:    &model.Restrictions{StartTime: "22:00", EndTime: "06:00"},
		},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.50}}},
	}}
	noon := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	night := time.Date(2024, 1, 3, 23, 0, 0, 0, time.UTC)

	day, ok := SessionPriceAt(tar, 22, model.CurrentAC, "charge1080_ac22", DefaultVehicle, noon)
	if !ok {
		t.Fatal("AC22 charger should serve charge1080_ac22")
	}
	nite, _ := SessionPriceAt(tar, 22, model.CurrentAC, "charge1080_ac22", DefaultVehicle, night)
	if !(nite < day) {
		t.Fatalf("off-peak session should be cheaper: day=%v night=%v", day, nite)
	}
	// A charger that can't serve the session yields no price.
	if _, ok := SessionPriceAt(tar, 11, model.CurrentAC, "charge1080_ac22", DefaultVehicle, noon); ok {
		t.Fatal("11kW charger cannot serve a 22kW session")
	}
}

func TestCustomPriceAt_EnergyAndPower(t *testing.T) {
	// Flat energy tariff: 0.30/kWh. "Charge 75 kWh at AC 11 kW" on a 22 kW AC
	// charger -> metered 75/0.89 = 84.27 kWh * 0.30 = 25.28.
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.30}}},
	}}
	got, ok := CustomPriceAt(tar, 22, model.CurrentAC, CustomSession{BatteryKWh: 75, PowerKW: 11}, DefaultVehicle, referenceTime())
	if !ok || !approx(got, 25.28) {
		t.Fatalf("75kWh @ AC11: want ~25.28, got %v ok=%v", got, ok)
	}

	// A charger slower than the requested power cannot serve the session.
	if _, ok := CustomPriceAt(tar, 7.4, model.CurrentAC, CustomSession{BatteryKWh: 75, PowerKW: 11}, DefaultVehicle, referenceTime()); ok {
		t.Fatal("7.4kW charger should not serve an 11kW session")
	}
}

func TestCustomPriceAt_AsFastAsPossible(t *testing.T) {
	// With a TIME component, "as fast as possible" (no power cap) should make a
	// faster charger cheaper for the same energy, because it finishes sooner.
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{PriceComponents: []model.PriceComponent{
			{Type: "ENERGY", Price: 0.30},
			{Type: "TIME", Price: 9.00}, // per hour
		}},
	}}
	c := CustomSession{BatteryKWh: 50} // PowerKW omitted -> charger's rated power
	slow, ok1 := CustomPriceAt(tar, 50, model.CurrentDC, c, DefaultVehicle, referenceTime())
	fast, ok2 := CustomPriceAt(tar, 250, model.CurrentDC, c, DefaultVehicle, referenceTime())
	if !ok1 || !ok2 {
		t.Fatalf("both should price: slow ok=%v fast ok=%v", ok1, ok2)
	}
	if !(fast < slow) {
		t.Fatalf("as-fast-as-possible: faster charger should cost less on time: slow=%v fast=%v", slow, fast)
	}
}

func TestCustomPriceAt_TimeOfDay(t *testing.T) {
	tar := model.Tariff{Currency: "EUR", Elements: []model.TariffElement{
		{
			PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.18}},
			Restrictions:    &model.Restrictions{StartTime: "22:00", EndTime: "06:00"},
		},
		{PriceComponents: []model.PriceComponent{{Type: "ENERGY", Price: 0.45}}},
	}}
	c := CustomSession{BatteryKWh: 75, PowerKW: 11}
	noon := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	night := time.Date(2024, 1, 3, 23, 0, 0, 0, time.UTC)
	day, _ := CustomPriceAt(tar, 22, model.CurrentAC, c, DefaultVehicle, noon)
	nite, _ := CustomPriceAt(tar, 22, model.CurrentAC, c, DefaultVehicle, night)
	if !(nite < day) {
		t.Fatalf("custom session should follow time-of-day: day=%v night=%v", day, nite)
	}
}

func TestHeadline_AlwaysPricedWithPower(t *testing.T) {
	tar := tariffEFT()
	// A 50 kW DC charger matches no standard DC tier (lowest is 150) but must
	// still get a headline price.
	if _, ok := AllPrices(tar, 50, model.CurrentDC, DefaultVehicle)["charge1080_dc150"]; ok {
		t.Fatal("50kW charger should not match the 150kW profile")
	}
	if _, ok := Headline(tar, 50, model.CurrentDC, DefaultVehicle); !ok {
		t.Fatal("headline should still be computed at the charger's actual power")
	}
}
