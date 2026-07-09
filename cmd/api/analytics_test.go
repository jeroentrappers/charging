package main

import "testing"

func TestUAClass(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Macintosh)": "browser",
		"curl/8.1":                "api",
		"Go-http-client/2.0":      "api",
		"Googlebot/2.1":           "bot",
		"":                        "other",
		"SomeRandomThing/1":       "other",
	}
	for ua, want := range cases {
		if got := uaClass(ua); got != want {
			t.Errorf("uaClass(%q)=%q want %q", ua, got, want)
		}
	}
}

func TestFeedFormat(t *testing.T) {
	cases := map[string]string{
		"datex/BE-001-table.xml":     "datex-xml",
		"datex/BE-001-table.json":    "datex-json",
		"datex/status.xml":           "datex-xml",
		"ndjson/BE-001.ndjson":       "ndjson",
		"geojson/BE-001.geojson":     "geojson",
		"ocpi/BE-001-locations.json": "ocpi",
		"availability.json":          "availability",
		"index.json":                 "manifest",
	}
	for name, want := range cases {
		if got := feedFormat(name); got != want {
			t.Errorf("feedFormat(%q)=%q want %q", name, got, want)
		}
	}
}

func TestSkipAnalyticsPath(t *testing.T) {
	for _, p := range []string{"/healthz", "/admin/runs", "/export/x", "/metrics", "/events"} {
		if !skipAnalyticsPath(p) {
			t.Errorf("%q should be skipped", p)
		}
	}
	for _, p := range []string{"/chargers/cheapest", "/chargers/{id}", "/stats"} {
		if skipAnalyticsPath(p) {
			t.Errorf("%q should NOT be skipped", p)
		}
	}
}
