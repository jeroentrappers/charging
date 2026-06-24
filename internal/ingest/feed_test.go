package ingest

import "testing"

func TestDatexURL(t *testing.T) {
	const base = "https://api.eco-movement.com/api/nap/datexii/locations"
	cases := []struct {
		name, url, token, want string
	}{
		{"open feed, no token", base, "", base},
		{"token appended", base, "abc123", base + "?token=abc123"},
		{"token merged into existing query", base + "?perPage=500", "abc123", base + "?perPage=500&token=abc123"},
		{"token is escaped", base, "a b/c", base + "?token=a+b%2Fc"},
	}
	for _, c := range cases {
		if got := datexURL(c.url, c.token); got != c.want {
			t.Errorf("%s: datexURL(%q,%q) = %q, want %q", c.name, c.url, c.token, got, c.want)
		}
	}
}
