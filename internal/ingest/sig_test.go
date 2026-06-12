package ingest

import (
	"testing"

	"github.com/appmire/charging/internal/model"
)

func TestConnectorSig(t *testing.T) {
	base := model.Connector{
		EVSEUID: "E1", ConnectorID: "1", EVSEStatus: "AVAILABLE",
		PowerKW: 22, PlugType: "IEC_62196_T2", CurrentType: "AC",
		Name: "Site", Lat: 52.1, Lon: 4.2, TariffID: "t1",
	}
	want := connectorSig(base, "h1")

	// Stable: same input → same signature.
	if got := connectorSig(base, "h1"); got != want {
		t.Fatalf("signature not stable: %d vs %d", got, want)
	}

	// Each field that a pass would persist must change the signature.
	cases := map[string]func(*model.Connector, *string){
		"status":     func(c *model.Connector, _ *string) { c.EVSEStatus = "CHARGING" },
		"power":      func(c *model.Connector, _ *string) { c.PowerKW = 50 },
		"plug":       func(c *model.Connector, _ *string) { c.PlugType = "CHADEMO" },
		"name":       func(c *model.Connector, _ *string) { c.Name = "Other" },
		"lat":        func(c *model.Connector, _ *string) { c.Lat = 52.2 },
		"tariffID":   func(c *model.Connector, _ *string) { c.TariffID = "t2" },
		"tariffHash": func(_ *model.Connector, h *string) { *h = "h2" },
	}
	for name, mutate := range cases {
		c := base
		h := "h1"
		mutate(&c, &h)
		if got := connectorSig(c, h); got == want {
			t.Errorf("signature unchanged after mutating %s", name)
		}
	}
}
