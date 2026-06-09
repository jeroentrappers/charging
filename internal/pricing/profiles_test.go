package pricing

import (
	"math"
	"testing"

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
