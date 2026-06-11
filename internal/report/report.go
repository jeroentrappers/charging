// Package report defines the taxonomy of structured community feedback on
// chargers and the (pure) rules for turning raw reports into an active,
// corroborated view. Structured-only: every report is one of a fixed set of
// types, optionally carrying a typed value — never free text.
package report

import (
	"encoding/json"
	"errors"
	"time"
)

// ValueKind is the optional typed payload a report may carry.
type ValueKind string

const (
	ValNone  ValueKind = ""
	ValTime  ValueKind = "time"  // {"close":"22:00","open":"08:00"} — site hours
	ValKW    ValueKind = "kw"    // {"kw":50} — observed power
	ValPrice ValueKind = "price" // {"price":0.55} — observed €/kWh
)

// Type describes one report kind.
type Type struct {
	Key       string        `json:"key"`
	Group     string        `json:"group"` // access | status | performance | pricing | data | positive
	Value     ValueKind     `json:"value"` // optional typed value
	Transient bool          `json:"transient"`
	Opposite  string        `json:"opposite,omitempty"` // report that supersedes this one (and vice versa)
	TTL       time.Duration `json:"-"`                  // active window
	TTLSecs   int           `json:"ttl_seconds"`        // serialized TTL
	Flags     bool          `json:"flags"`              // contributes to "avoid this charger" de-prioritisation
}

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// types is the registry. TTLSecs is filled from TTL in init.
var types = []Type{
	// Access & legitimacy
	{Key: "not_public", Group: "access", TTL: 30 * day, Flags: true},
	{Key: "access_blocked", Group: "access", TTL: 30 * day},
	{Key: "site_hours", Group: "access", TTL: 90 * day, Value: ValTime},
	// Operational status — transient, opposites supersede.
	{Key: "out_of_service", Group: "status", Transient: true, Opposite: "back_in_service", TTL: 6 * hour, Flags: true},
	{Key: "back_in_service", Group: "status", Transient: true, Opposite: "out_of_service", TTL: 6 * hour},
	{Key: "spot_blocked", Group: "status", Transient: true, TTL: 3 * hour},
	// Performance & pricing
	{Key: "slower_than_rated", Group: "performance", TTL: 14 * day, Value: ValKW},
	{Key: "price_incorrect", Group: "pricing", TTL: 14 * day, Value: ValPrice},
	// Data quality
	{Key: "wrong_location", Group: "data", TTL: 90 * day},
	{Key: "does_not_exist", Group: "data", TTL: 90 * day, Flags: true},
	// Positive
	{Key: "confirmed_ok", Group: "positive", Transient: true, TTL: 7 * day},
}

var byKey = map[string]Type{}

func init() {
	for i := range types {
		types[i].TTLSecs = int(types[i].TTL / time.Second)
		byKey[types[i].Key] = types[i]
	}
}

// Types returns the registry (for clients / GET /reports/types).
func Types() []Type { return types }

// Lookup returns the type definition for a key.
func Lookup(key string) (Type, bool) { t, ok := byKey[key]; return t, ok }

var (
	ErrUnknownType = errors.New("unknown report type")
	ErrBadValue    = errors.New("invalid value for report type")
)

// ValidateValue checks a report's value against its type's expected shape and
// returns the normalized JSON to store (nil when the type takes no value).
func ValidateValue(key string, raw json.RawMessage) (json.RawMessage, error) {
	t, ok := byKey[key]
	if !ok {
		return nil, ErrUnknownType
	}
	if t.Value == ValNone {
		return nil, nil // value ignored for valueless types
	}
	if len(raw) == 0 {
		return nil, nil // value is optional even where supported
	}
	switch t.Value {
	case ValTime:
		var v struct {
			Close string `json:"close"`
			Open  string `json:"open"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || (!validHHMM(v.Close) && !validHHMM(v.Open)) {
			return nil, ErrBadValue
		}
		return json.Marshal(v)
	case ValKW:
		var v struct {
			KW float64 `json:"kw"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || v.KW <= 0 || v.KW > 1000 {
			return nil, ErrBadValue
		}
		return json.Marshal(v)
	case ValPrice:
		var v struct {
			Price float64 `json:"price"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || v.Price < 0 || v.Price > 10 {
			return nil, ErrBadValue
		}
		return json.Marshal(v)
	}
	return nil, ErrBadValue
}

func validHHMM(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	_, err := time.Parse("15:04", s)
	return err == nil
}

// Raw is a single stored report row.
type Raw struct {
	Type      string          `json:"type"`
	Value     json.RawMessage `json:"value,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Agg is the corroborated, active view of one report type for a charger.
type Agg struct {
	Type   string          `json:"type"`
	Group  string          `json:"group"`
	Count  int             `json:"count"`
	LastAt time.Time       `json:"last_at"`
	Value  json.RawMessage `json:"value,omitempty"` // most recent value, when the type carries one
	Flags  bool            `json:"flags"`
}

// FlagThreshold is the number of distinct corroborating reports a flagging type
// needs before it de-prioritises a charger (a single report only shows a note).
const FlagThreshold = 2

// Aggregate collapses a charger's raw reports into active, corroborated entries:
// drops anything past its type TTL, suppresses a type whose opposite has a more
// recent report, counts distinct submissions (rows are already one-per-client),
// and carries the most recent value. Sorted by group then recency.
func Aggregate(now time.Time, raws []Raw) []Agg {
	type acc struct {
		count   int
		lastAt  time.Time
		value   json.RawMessage
		valueAt time.Time
	}
	active := map[string]*acc{}
	for _, r := range raws {
		t, ok := byKey[r.Type]
		if !ok || now.Sub(r.CreatedAt) > t.TTL {
			continue
		}
		a := active[r.Type]
		if a == nil {
			a = &acc{}
			active[r.Type] = a
		}
		a.count++
		if r.CreatedAt.After(a.lastAt) {
			a.lastAt = r.CreatedAt
		}
		if len(r.Value) > 0 && r.CreatedAt.After(a.valueAt) {
			a.value, a.valueAt = r.Value, r.CreatedAt
		}
	}
	// Opposite suppression: if both a type and its opposite are active, keep only
	// the more recently reported one.
	for key, a := range active {
		t := byKey[key]
		if t.Opposite == "" {
			continue
		}
		if opp := active[t.Opposite]; opp != nil && opp.lastAt.After(a.lastAt) {
			delete(active, key)
		}
	}
	out := make([]Agg, 0, len(active))
	for key, a := range active {
		t := byKey[key]
		out = append(out, Agg{Type: key, Group: t.Group, Count: a.count, LastAt: a.lastAt, Value: a.value, Flags: t.Flags})
	}
	sortAggs(out)
	return out
}

// Avoid reports whether the active reports warrant de-prioritising the charger
// (a flagging type corroborated by at least FlagThreshold submissions).
func Avoid(aggs []Agg) bool {
	for _, a := range aggs {
		if a.Flags && a.Count >= FlagThreshold {
			return true
		}
	}
	return false
}

func sortAggs(a []Agg) {
	// stable-ish: group asc, then count desc, then type — no external deps.
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && less(a[j], a[j-1]); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func less(x, y Agg) bool {
	if x.Group != y.Group {
		return x.Group < y.Group
	}
	if x.Count != y.Count {
		return x.Count > y.Count
	}
	return x.Type < y.Type
}
