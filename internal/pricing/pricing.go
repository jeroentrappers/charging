// Package pricing turns a structured OCPI tariff into a single comparable
// number: the cost of a fixed "standard session" at a given connector. This is
// what makes "cheapest charger nearby" sortable, since raw tariffs mix per-kWh,
// per-minute and per-session components that aren't directly comparable.
package pricing

import (
	"strings"
	"time"

	"github.com/appmire/charging/internal/model"
)

// StandardKWh is the energy basket used for the comparable price: the cost to
// add this many kWh. Using a fixed basket makes AC and DC chargers comparable
// (a DC charger delivers it faster, so its time-based components cost less).
const StandardKWh = 30.0

// Session describes the basket to price.
type Session struct {
	KWh   float64
	Power float64 // kW, used to derive charging duration for TIME components
	At    time.Time
}

// referenceTime is a fixed, deterministic moment (a Wednesday, 12:00 UTC) used
// when computing the stored comparable price, so historical trend statistics
// compare like-for-like over time regardless of when ingestion ran. The live
// "cheapest right now" path may instead evaluate at the actual request time.
func referenceTime() time.Time {
	return time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
}

// Comparable returns the cost of the standard session for a connector under the
// given tariff, evaluated at the deterministic reference time. ok is false when
// the tariff yields no priceable components (treated as "unknown", never zero).
func Comparable(t model.Tariff, powerKW float64) (cost float64, ok bool) {
	return Evaluate(t, Session{KWh: StandardKWh, Power: powerKW, At: referenceTime()})
}

// Evaluate computes the total cost of a session under a tariff.
//
// OCPI semantics: tariff elements are ordered; for each dimension (ENERGY,
// FLAT, TIME) the first element whose restrictions match and that carries a
// component of that dimension provides the price. PARKING_TIME is excluded
// from the charging-session comparable.
func Evaluate(t model.Tariff, s Session) (float64, bool) {
	var hours float64
	if s.Power > 0 {
		hours = s.KWh / s.Power
	}

	energyPrice, energyOK := firstPrice(t, s, hours, "ENERGY")
	flatPrice, flatOK := firstPrice(t, s, hours, "FLAT")
	timePrice, timeOK := firstPrice(t, s, hours, "TIME")

	if !energyOK && !flatOK && !timeOK {
		return 0, false
	}

	total := 0.0
	if energyOK {
		total += energyPrice * s.KWh
	}
	if flatOK {
		total += flatPrice
	}
	if timeOK {
		total += timePrice * hours
	}
	return round4(total), true
}

// firstPrice finds the price of the first matching element carrying dimension.
func firstPrice(t model.Tariff, s Session, hours float64, dimension string) (float64, bool) {
	for _, el := range t.Elements {
		if !matches(el.Restrictions, s, hours) {
			continue
		}
		for _, pc := range el.PriceComponents {
			if pc.Type == dimension {
				return pc.Price, true
			}
		}
	}
	return 0, false
}

func matches(r *model.Restrictions, s Session, hours float64) bool {
	if r == nil {
		return true
	}
	if len(r.DayOfWeek) > 0 && !containsDay(r.DayOfWeek, s.At.Weekday()) {
		return false
	}
	if !withinTimeOfDay(r.StartTime, r.EndTime, s.At) {
		return false
	}
	if r.MinKWh != nil && s.KWh < *r.MinKWh {
		return false
	}
	if r.MaxKWh != nil && s.KWh > *r.MaxKWh {
		return false
	}
	if r.MinPower != nil && s.Power < *r.MinPower {
		return false
	}
	if r.MaxPower != nil && s.Power > *r.MaxPower {
		return false
	}
	durSecs := hours * 3600
	if r.MinDuration != nil && durSecs < float64(*r.MinDuration) {
		return false
	}
	if r.MaxDuration != nil && durSecs > float64(*r.MaxDuration) {
		return false
	}
	return true
}

var weekdayCode = map[time.Weekday]string{
	time.Monday: "MONDAY", time.Tuesday: "TUESDAY", time.Wednesday: "WEDNESDAY",
	time.Thursday: "THURSDAY", time.Friday: "FRIDAY", time.Saturday: "SATURDAY",
	time.Sunday: "SUNDAY",
}

func containsDay(days []string, wd time.Weekday) bool {
	want := weekdayCode[wd]
	for _, d := range days {
		if strings.EqualFold(strings.TrimSpace(d), want) {
			return true
		}
	}
	return false
}

// withinTimeOfDay reports whether t's HH:MM falls in [start,end). Empty bounds
// mean "no restriction". An end <= start is treated as an overnight window.
func withinTimeOfDay(start, end string, t time.Time) bool {
	if start == "" && end == "" {
		return true
	}
	cur := t.Hour()*60 + t.Minute()
	s := parseHM(start, 0)
	e := parseHM(end, 24*60)
	if e <= s { // overnight window, e.g. 22:00-06:00
		return cur >= s || cur < e
	}
	return cur >= s && cur < e
}

func parseHM(s string, def int) int {
	if len(s) < 4 || !strings.Contains(s, ":") {
		return def
	}
	parts := strings.SplitN(s, ":", 2)
	h := atoi(parts[0], -1)
	m := atoi(parts[1], -1)
	if h < 0 || m < 0 {
		return def
	}
	return h*60 + m
}

func atoi(s string, def int) int {
	n := 0
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func round4(f float64) float64 {
	return float64(int64(f*10000+0.5)) / 10000
}
