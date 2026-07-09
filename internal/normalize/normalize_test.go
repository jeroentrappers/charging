package normalize

import (
	"testing"

	"github.com/appmire/charging/internal/ocpi"
)

func TestConnectorPowerKW(t *testing.T) {
	cases := []struct {
		name string
		conn ocpi.Connector
		want float64
	}{
		{
			name: "explicit watts, plausible",
			conn: ocpi.Connector{MaxElectricPower: 22000, PowerType: "AC_3_PHASE", MaxVoltage: 230, MaxAmperage: 32},
			want: 22.0,
		},
		{
			name: "DC fast charger, explicit below V*A ceiling",
			conn: ocpi.Connector{MaxElectricPower: 300000, PowerType: "DC", MaxVoltage: 1000, MaxAmperage: 500},
			want: 300.0,
		},
		{
			// NL DOT-NL / Eneco 1000x bug: 230*33*3 = 22.8 kW, but max_electric_power
			// arrives as (230*33)*1000. Must fall back to the V*A estimate.
			name: "eneco 1000x corruption falls back to estimate",
			conn: ocpi.Connector{MaxElectricPower: 7590000, PowerType: "AC_3_PHASE", MaxVoltage: 230, MaxAmperage: 33},
			want: 22.8,
		},
		{
			name: "no explicit power, estimate from V*A single phase",
			conn: ocpi.Connector{PowerType: "AC_1_PHASE", MaxVoltage: 230, MaxAmperage: 32},
			want: 7.4,
		},
		{
			name: "no explicit power, three phase",
			conn: ocpi.Connector{PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32},
			want: 38.4,
		},
		{
			// Corrupt explicit value with no V*A to cross-check: kept as-is
			// (nothing to fall back to); other guards must catch it downstream.
			name: "corrupt explicit, no V*A available",
			conn: ocpi.Connector{MaxElectricPower: 7590000},
			want: 7590.0,
		},
		{
			name: "legacy 43kW three-phase AC is preserved",
			conn: ocpi.Connector{MaxElectricPower: 43000, PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 63},
			want: 43.0,
		},
		{
			name: "nothing usable",
			conn: ocpi.Connector{},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectorPowerKW(tc.conn); got != tc.want {
				t.Errorf("connectorPowerKW() = %v, want %v", got, tc.want)
			}
		})
	}
}
