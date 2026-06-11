package report

import (
	"encoding/json"
	"testing"
	"time"
)

func raw(typ string, ageHours float64, now time.Time) Raw {
	return Raw{Type: typ, CreatedAt: now.Add(-time.Duration(ageHours * float64(time.Hour)))}
}

func find(aggs []Agg, typ string) (Agg, bool) {
	for _, a := range aggs {
		if a.Type == typ {
			return a, true
		}
	}
	return Agg{}, false
}

func TestAggregate_TTLDropsStaleTransient(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	// out_of_service TTL is 6h: a 7h-old one is gone, a 2h-old one stays.
	aggs := Aggregate(now, []Raw{raw("out_of_service", 7, now)})
	if _, ok := find(aggs, "out_of_service"); ok {
		t.Fatal("7h-old out_of_service should have expired (6h TTL)")
	}
	aggs = Aggregate(now, []Raw{raw("out_of_service", 2, now)})
	if _, ok := find(aggs, "out_of_service"); !ok {
		t.Fatal("2h-old out_of_service should be active")
	}
}

func TestAggregate_OppositeSupersedes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	// A newer back_in_service hides the older out_of_service.
	aggs := Aggregate(now, []Raw{
		raw("out_of_service", 3, now),
		raw("back_in_service", 1, now),
	})
	if _, ok := find(aggs, "out_of_service"); ok {
		t.Fatal("out_of_service should be suppressed by a newer back_in_service")
	}
	if _, ok := find(aggs, "back_in_service"); !ok {
		t.Fatal("back_in_service should be active")
	}
}

func TestAvoid_NeedsCorroboration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	one := Aggregate(now, []Raw{raw("not_public", 1, now)})
	if Avoid(one) {
		t.Fatal("a single not_public report must not de-prioritise (only note)")
	}
	// Two distinct clients (distinct rows) -> corroborated -> avoid.
	two := []Agg{{Type: "not_public", Count: 2, Flags: true}}
	if !Avoid(two) {
		t.Fatalf("two corroborating not_public reports should trigger avoid (threshold %d)", FlagThreshold)
	}
}

func TestValidateValue(t *testing.T) {
	// valueless type ignores any value
	if v, err := ValidateValue("not_public", json.RawMessage(`{"x":1}`)); err != nil || v != nil {
		t.Fatalf("valueless type should return (nil,nil), got %s %v", v, err)
	}
	// site_hours: valid close time
	if _, err := ValidateValue("site_hours", json.RawMessage(`{"close":"22:00"}`)); err != nil {
		t.Fatalf("valid site_hours rejected: %v", err)
	}
	// site_hours: garbage time
	if _, err := ValidateValue("site_hours", json.RawMessage(`{"close":"99:99"}`)); err == nil {
		t.Fatal("invalid time should be rejected")
	}
	// kw out of range
	if _, err := ValidateValue("slower_than_rated", json.RawMessage(`{"kw":5000}`)); err == nil {
		t.Fatal("absurd kw should be rejected")
	}
	if _, err := ValidateValue("slower_than_rated", json.RawMessage(`{"kw":50}`)); err != nil {
		t.Fatalf("valid kw rejected: %v", err)
	}
	// price in range
	if _, err := ValidateValue("price_incorrect", json.RawMessage(`{"price":0.55}`)); err != nil {
		t.Fatalf("valid price rejected: %v", err)
	}
	// unknown type
	if _, err := ValidateValue("nope", nil); err != ErrUnknownType {
		t.Fatalf("want ErrUnknownType, got %v", err)
	}
}
