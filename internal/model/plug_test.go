package model

import "testing"

func TestNormalizePlug(t *testing.T) {
	cases := map[string]string{
		"IEC_62196_T2":       "IEC_62196_T2",
		"iec62196T2":         "IEC_62196_T2",
		"IEC_62196_T2_COMBO": "IEC_62196_T2_COMBO",
		"iec62196T2Combo":    "IEC_62196_T2_COMBO",
		"chademo":            "CHADEMO",
		"CHADEMO":            "CHADEMO",
		"iec62196T1":         "IEC_62196_T1",
		"":                   "",
		"SOMETHING_NEW":      "SOMETHING_NEW", // unknown passes through
	}
	for in, want := range cases {
		if got := NormalizePlug(in); got != want {
			t.Errorf("NormalizePlug(%q) = %q, want %q", in, got, want)
		}
	}
}
