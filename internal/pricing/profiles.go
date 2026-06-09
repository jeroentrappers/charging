package pricing

import "github.com/appmire/charging/internal/model"

// Vehicle is the reference car the session energy figures are based on. It is
// configurable so the comparison can be tuned to a fleet or a typical local EV.
type Vehicle struct {
	UsableKWh         float64 // usable battery capacity
	ConsumptionKWh100 float64 // energy at the battery per 100 km
}

// DefaultVehicle is a mid-size EV (60 kWh usable, 18 kWh/100km).
var DefaultVehicle = Vehicle{UsableKWh: 60, ConsumptionKWh100: 18}

// Charging efficiency (energy into battery / energy metered at the charger).
// AC loses more (onboard charger + cabling) than DC.
const (
	effAC = 0.89
	effDC = 0.94
)

// profileDef is a session template, independent of the reference vehicle.
type profileDef struct {
	Key     string
	Label   string
	Current string  // AC | DC
	TierKW  float64 // rated charger power this session targets (capability + restrictions)
	AvgKW   float64 // effective average power (accounts for DC taper) -> duration
	// battery returns the energy added to the battery (kWh) for this vehicle.
	battery func(v Vehicle) float64
}

func kmEnergy(km float64) func(Vehicle) float64 {
	return func(v Vehicle) float64 { return v.ConsumptionKWh100 * km / 100 }
}
func socWindow(frac float64) func(Vehicle) float64 {
	return func(v Vehicle) float64 { return v.UsableKWh * frac }
}

// profileDefs are the 10 comparison sessions. AvgKW encodes the DC charging
// curve: a 150 kW charger averages ~110 kW across a 10–80% session but nearer
// peak on a small low-SoC top-up.
var profileDefs = []profileDef{
	{"topup100_ac11", "100 km top-up @ AC 11 kW", model.CurrentAC, 11, 11, kmEnergy(100)},
	{"topup100_ac22", "100 km top-up @ AC 22 kW", model.CurrentAC, 22, 22, kmEnergy(100)},
	{"topup100_dc150", "100 km top-up @ DC 150 kW", model.CurrentDC, 150, 135, kmEnergy(100)},
	{"topup100_dc300", "100 km top-up @ DC 300 kW", model.CurrentDC, 300, 240, kmEnergy(100)},

	{"charge1080_ac11", "10→80% @ AC 11 kW", model.CurrentAC, 11, 11, socWindow(0.70)},
	{"charge1080_ac22", "10→80% @ AC 22 kW", model.CurrentAC, 22, 22, socWindow(0.70)},
	{"charge1080_dc150", "10→80% @ DC 150 kW", model.CurrentDC, 150, 110, socWindow(0.70)},
	{"charge1080_dc300", "10→80% @ DC 300 kW", model.CurrentDC, 300, 180, socWindow(0.70)},

	{"urban_ac22", "Quick urban top-up (~40 km) @ AC 22 kW", model.CurrentAC, 22, 22, kmEnergy(40)},
	{"overnight_ac11", "Destination 10→100% @ AC 11 kW", model.CurrentAC, 11, 11, socWindow(0.90)},
}

// ResolvedProfile is a profile concretized for a vehicle: the metered energy and
// the session parameters used for pricing.
type ResolvedProfile struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Current   string  `json:"current"`
	TierKW    float64 `json:"tier_kw"`
	AvgKW     float64 `json:"avg_kw"`
	MeteredKW float64 `json:"metered_kwh"`
}

func efficiency(current string) float64 {
	if current == model.CurrentDC {
		return effDC
	}
	return effAC
}

// Profiles resolves the 10 session templates for a vehicle.
func Profiles(v Vehicle) []ResolvedProfile {
	out := make([]ResolvedProfile, 0, len(profileDefs))
	for _, d := range profileDefs {
		metered := d.battery(v) / efficiency(d.Current)
		out = append(out, ResolvedProfile{
			Key: d.Key, Label: d.Label, Current: d.Current,
			TierKW: d.TierKW, AvgKW: d.AvgKW, MeteredKW: round2(metered),
		})
	}
	return out
}

// IsProfile reports whether key names one of the defined session profiles.
func IsProfile(key string) bool {
	for _, d := range profileDefs {
		if d.Key == key {
			return true
		}
	}
	return false
}

// session builds the pricing Session for a resolved profile.
func (p ResolvedProfile) session() Session {
	return Session{KWh: p.MeteredKW, Power: p.TierKW, AvgPower: p.AvgKW, At: referenceTime()}
}

// supportedBy reports whether a charger can actually deliver this session.
func (p ResolvedProfile) supportedBy(chargerPowerKW float64, currentType string) bool {
	return currentType == p.Current && chargerPowerKW >= p.TierKW-0.5
}

// AllPrices returns the price of every session the charger can serve, keyed by
// profile key. Profiles the charger cannot deliver (wrong current type or
// insufficient power) are omitted. Entries are also omitted when the tariff has
// no priceable component for that session.
func AllPrices(t model.Tariff, chargerPowerKW float64, currentType string, v Vehicle) map[string]float64 {
	prices := make(map[string]float64)
	for _, p := range Profiles(v) {
		if !p.supportedBy(chargerPowerKW, currentType) {
			continue
		}
		if cost, ok := Evaluate(t, p.session()); ok {
			prices[p.Key] = cost
		}
	}
	return prices
}

// Headline is the single representative price used for the default sort: a
// 10→80% session at the charger's actual rated power (so every charger gets a
// value, even ones whose power doesn't match a standard tier). DC duration uses
// a taper factor.
func Headline(t model.Tariff, chargerPowerKW float64, currentType string, v Vehicle) (float64, bool) {
	if chargerPowerKW <= 0 {
		// No power info: fall back to a 30 kWh energy-only basket.
		return Comparable(t, 0)
	}
	metered := socWindow(0.70)(v) / efficiency(currentType)
	avg := chargerPowerKW
	if currentType == model.CurrentDC {
		avg = chargerPowerKW * 0.73 // representative 10–80% taper
	}
	return Evaluate(t, Session{KWh: metered, Power: chargerPowerKW, AvgPower: avg, At: referenceTime()})
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
