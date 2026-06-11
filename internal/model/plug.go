package model

import "strings"

// NormalizePlug maps the various connector-standard spellings across feeds
// (OCPI "IEC_62196_T2", DATEX "iec62196T2", "chademo", …) to a single canonical
// OCPI 2.2 value, so stored data, filtering and the export are consistent.
// Unknown values are returned unchanged.
func NormalizePlug(raw string) string {
	key := strings.ToUpper(raw)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, " ", "")
	key = strings.ReplaceAll(key, "-", "")
	switch key {
	case "IEC62196T2":
		return "IEC_62196_T2"
	case "IEC62196T2COMBO":
		return "IEC_62196_T2_COMBO"
	case "IEC62196T1":
		return "IEC_62196_T1"
	case "IEC62196T1COMBO":
		return "IEC_62196_T1_COMBO"
	case "IEC62196T3C":
		return "IEC_62196_T3C"
	case "IEC62196T3A":
		return "IEC_62196_T3A"
	case "CHADEMO":
		return "CHADEMO"
	case "DOMESTICF", "DOMESTICSCHUKO":
		return "DOMESTIC_F"
	case "DOMESTICE":
		return "DOMESTIC_E"
	case "TESLAS":
		return "TESLA_S"
	case "TESLAR":
		return "TESLA_R"
	case "":
		return ""
	}
	return raw
}
