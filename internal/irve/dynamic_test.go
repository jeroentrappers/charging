package irve

import (
	"strings"
	"testing"
	"time"
)

// The consolidated dynamic file repeats points (several publishers report the
// same one) and keeps rows long after they went stale, so the reader has to pick
// the freshest row per point and ignore anything too old to mean anything.
func TestParseDynamic(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	csv := strings.Join([]string{
		`id_pdc_itinerance,etat_pdc,occupation_pdc,horodatage,etat_prise_type_2,etat_prise_type_combo_ccs`,
		// fresh + free
		`FRA1,en_service,libre,2026-08-18 09:00:00+00:00,fonctionnel,`,
		// same point, older row: must lose to the fresher one below
		`FRA2,en_service,libre,2026-08-18 01:00:00+00:00,"",`,
		`FRA2,en_service,occupe,2026-08-18 11:30:00+00:00,"",`,
		// out of service wins over whatever the occupancy column says
		`FRA3,hors_service,libre,2026-08-18T10:00:00Z,,`,
		// too old to be a statement about availability
		`FRA4,en_service,libre,2026-06-15 08:00:00+00:00,,`,
		// unusable timestamp
		`FRA5,en_service,libre,,,`,
		// unknown occupancy
		`FRA6,en_service,inconnu,2026-08-18 11:00:00+00:00,,`,
		// reserved counts as taken
		`FRA7,en_service,reserve,2026-08-18 11:00:00+00:00,,`,
		// no id
		`,en_service,libre,2026-08-18 11:00:00+00:00,,`,
	}, "\n")

	got, err := ParseDynamic(strings.NewReader(csv), now)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FRA1": "AVAILABLE",
		"FRA2": "CHARGING", // the fresher row
		"FRA3": "OUTOFORDER",
		"FRA6": "UNKNOWN",
		"FRA7": "CHARGING",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d statuses %v, want %d", len(got), got, len(want))
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %q, want %q", id, got[id], w)
		}
	}
	for _, id := range []string{"FRA4", "FRA5"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s should be absent (stale or undated), got %q", id, got[id])
		}
	}
}

// A schema change upstream must fail loudly rather than silently yield no
// availability at all.
func TestParseDynamic_RejectsUnexpectedColumns(t *testing.T) {
	_, err := ParseDynamic(strings.NewReader("a,b,c\n1,2,3\n"), time.Now())
	if err == nil {
		t.Fatal("want an error for a header without the expected columns")
	}
}
