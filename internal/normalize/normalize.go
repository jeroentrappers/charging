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
					EVSEStatus:  evse.Status,
					TariffID:    con.TariffID,
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

// connectorPowerKW estimates max power from voltage/amperage/phases, since
// OCPI 2.1.1 connectors carry no explicit max-power field.
func connectorPowerKW(c ocpi.Connector) float64 {
	if c.Voltage <= 0 || c.Amperage <= 0 {
		return 0
	}
	w := float64(c.Voltage * c.Amperage)
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
