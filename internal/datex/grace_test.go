package datex

import (
	"encoding/json"
	"testing"

	"github.com/appmire/charging/internal/model"
)

func findType(comps []model.PriceComponent, typ string) (model.PriceComponent, bool) {
	for _, c := range comps {
		if c.Type == typ {
			return c, true
		}
	}
	return model.PriceComponent{}, false
}

func TestGraceMinutes(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		// Real phrasings observed in the live German NAP feeds.
		{"Der Minutenpreis wird erst nach 240 Minuten erhoben", 240},
		{"Der Minutenpreis wird erst nach 180 Minuten erhoben", 180},
		{"Der Minutenpreis wird erst nach 90 Minuten erhoben", 90},
		{"Der Minutenpreis wird erst nach 0 Minuten erhoben", 0},
		{"Der Minutenpreis wird erst nach 240 Minuten erhoben und auch nur in der Zeit von 06:00 bis 22:00", 240},
		// Other languages / phrasings we defensively accept.
		{"after 30 min", 30},
		{"charged from 60 minutes", 60},
		{"à partir de 90 minutes", 90},
		// No threshold stated → 0 (fee applies from the start).
		{"", 0},
		{"Ad-hoc charging, VAT included", 0},
	}
	for _, c := range cases {
		if got := graceMinutes(c.in); got != c.want {
			t.Errorf("graceMinutes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPriceComponents_GraceThreshold_XML(t *testing.T) {
	comps := priceComponents([]afirPrice{
		{PriceType: "pricePerKWh", Value: 0.63},
		{PriceType: "pricePerMinute", Value: 0.07, AddInfo: []string{"Der Minutenpreis wird erst nach 240 Minuten erhoben"}},
	})
	timeC, ok := findType(comps, "TIME")
	if !ok {
		t.Fatalf("no TIME component built; got %+v", comps)
	}
	if timeC.AfterMinutes != 240 {
		t.Errorf("AfterMinutes = %d, want 240", timeC.AfterMinutes)
	}
	if timeC.Price != 4.2 { // 0.07 €/min * 60
		t.Errorf("Price = %v, want 4.2 €/h", timeC.Price)
	}
}

func TestPriceComponents_GraceThreshold_JSON(t *testing.T) {
	var ep jafirEnergyPrice
	if err := json.Unmarshal([]byte(`{"priceType":{"value":"pricePerMinute"},"value":0.07,
		"additionalInformation":{"values":[{"lang":"de","value":"Der Minutenpreis wird erst nach 180 Minuten erhoben"}]}}`), &ep); err != nil {
		t.Fatal(err)
	}
	comps := jafirPriceComponents([]jafirEnergyPrice{ep})
	timeC, ok := findType(comps, "TIME")
	if !ok {
		t.Fatalf("no TIME component built; got %+v", comps)
	}
	if timeC.AfterMinutes != 180 {
		t.Errorf("AfterMinutes = %d, want 180", timeC.AfterMinutes)
	}
}
