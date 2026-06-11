package model

import "strings"

// IsPrivateName reports whether a charger's operator-given name marks it as a
// private (home / peer-to-peer-shared) point rather than a genuinely public
// station. There is no structured public/private flag in the AFIR/DATEX feeds,
// so the operator-provided name is the only signal: networks like Stroohm and
// CenEnergy explicitly label "Private" vs "Public" and tag home chargers with a
// "· Home" site category. Anything explicitly marked "public" is kept.
func IsPrivateName(name string) bool {
	n := strings.ToLower(name)
	if strings.Contains(n, "public") {
		return false
	}
	return strings.Contains(n, "private") || strings.Contains(n, "· home")
}
