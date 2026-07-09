// Package normalize maps OCPI 2.1.1 wire types into the canonical model.
package normalize

import (
	"strconv"

	"github.com/appmire/charging/internal/model"
	"github.com/appmire/charging/internal/ocpi"
)

// Result is the normalized output of one CPO feed.
type Result struct {
	Connectors []model.Connector
	Tariffs    map[string]model.Tariff // keyed by OCPI tariff id
}

// FromOCPI flattens Locations/EVSEs/Connectors into canonical connectors and
// indexes tariffs by id. cpoID is our stable slug for the operator.
func FromOCPI(cpoID string, locations []ocpi.Location, tariffs []ocpi.Tariff) Result {
	res := Result{Tariffs: make(map[string]model.Tariff, len(tariffs))}
	for _, t := range tariffs {
		res.Tariffs[t.ID] = normalizeTariff(t)
	}

	for _, loc := range locations {
		for _, evse := range loc.EVSEs {
			lat, lon := coords(loc, evse)
			for _, con := range evse.Connectors {
				res.Connectors = append(res.Connectors, model.Connector{
					CPOID:       cpoID,
					EVSEUID:     evse.UID,
					ConnectorID: con.ID,
					Lat:         lat,
					Lon:         lon,
					PowerKW:     connectorPowerKW(con),
					PlugType:    con.Standard,
					CurrentType: currentType(con.PowerType),
					Name:        loc.Name,
					Address:     address(loc),
					PostalCode:  loc.PostalCode,
					City:        loc.City,
					EVSEStatus:  evse.Status,
					TariffID:    con.Tariff(),
				})
			}
		}
	}
	return res
}

func normalizeTariff(t ocpi.Tariff) model.Tariff {
	out := model.Tariff{
		OCPIID:      t.ID,
		Currency:    t.Currency,
		LastUpdated: t.LastUpdated,
		Elements:    make([]model.TariffElement, 0, len(t.Elements)),
	}
	for _, el := range t.Elements {
		me := model.TariffElement{}
		for _, pc := range el.PriceComponents {
			me.PriceComponents = append(me.PriceComponents, model.PriceComponent{
				Type:     pc.Type,
				Price:    pc.Price,
				StepSize: pc.StepSize,
			})
		}
		if el.Restrictions != nil {
			r := el.Restrictions
			me.Restrictions = &model.Restrictions{
				StartTime: r.StartTime, EndTime: r.EndTime,
				StartDate: r.StartDate, EndDate: r.EndDate,
				MinKWh: r.MinKWh, MaxKWh: r.MaxKWh,
				MinPower: r.MinPower, MaxPower: r.MaxPower,
				MinDuration: r.MinDuration, MaxDuration: r.MaxDuration,
				DayOfWeek: r.DayOfWeek,
			}
		}
		out.Elements = append(out.Elements, me)
	}
	return out
}

// coords prefers EVSE-level coordinates, falling back to the location.
func coords(loc ocpi.Location, evse ocpi.EVSE) (lat, lon float64) {
	g := loc.Coordinates
	if evse.Coordinates != nil {
		g = *evse.Coordinates
	}
	lat = parseFloat(g.Latitude)
	lon = parseFloat(g.Longitude)
	return
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func currentType(powerType string) string {
	if powerType == "DC" {
		return model.CurrentDC
	}
	return model.CurrentAC
}

// connectorPowerKW prefers the explicit max_electric_power (OCPI 2.2.1, watts);
// otherwise it estimates from voltage/amperage/phases (OCPI 2.1.1 carries no
// explicit max-power field).
//
// Some feeds report max_electric_power 1000x too large: NL DOT-NL / Eneco send
// voltage*amperage*1000 instead of watts, so a 22 kW three-phase AC post
// (230 V * 33 A * 3 = 22.8 kW) arrives as max_electric_power=7590000 → 7590 kW.
// A connector can never deliver more than voltage*amperage*phases (its physical
// ceiling), so when the explicit value clearly exceeds that estimate we discard
// it and use the estimate instead. The 4x margin tolerates a mislabelled phase
// count (up to 3x) while still catching the real corruption (observed 5x-1000x).
func connectorPowerKW(c ocpi.Connector) float64 {
	est := estimatedPowerKW(c)

	if c.MaxElectricPower > 0 {
		p := round1(float64(c.MaxElectricPower) / 1000)
		if est > 0 && p > est*4 {
			return est
		}
		return p
	}
	return est
}

// estimatedPowerKW is the physical power ceiling derived from voltage, amperage
// and phase count. Returns 0 when voltage or amperage is unavailable.
// OCPI 2.1.1 uses voltage/amperage; 2.2.1 uses max_voltage/max_amperage.
func estimatedPowerKW(c ocpi.Connector) float64 {
	v, a := c.Voltage, c.Amperage
	if v <= 0 {
		v = c.MaxVoltage
	}
	if a <= 0 {
		a = c.MaxAmperage
	}
	if v <= 0 || a <= 0 {
		return 0
	}
	w := float64(v * a)
	if c.PowerType == "AC_3_PHASE" {
		w *= 3
	}
	return round1(w / 1000)
}

func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}

func address(loc ocpi.Location) string {
	s := loc.Address
	if loc.PostalCode != "" || loc.City != "" {
		if s != "" {
			s += ", "
		}
		s += loc.PostalCode
		if loc.City != "" {
			if loc.PostalCode != "" {
				s += " "
			}
			s += loc.City
		}
	}
	return s
}
