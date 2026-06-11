package model

import "testing"

func TestIsPrivateName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Stroohm · Private - Home - Luc Charlier", true},
		{"Stroohm · Private - Office - Roche SA", true},
		{"CenEnergy · Home HS", true},
		{"Stroohm · Public - Office - Boutique Hotel", false}, // explicitly public
		{"EQUANS Carbon Shift · Koen Weckx Home Public", false}, // public wins over home
		{"Enersol · AD Delhaize Hannut", false},
		{"Censo Charging Solutions B.V. · GXO Tongeren", false},
		{"Road station Antwerpen", false},
	}
	for _, c := range cases {
		if got := IsPrivateName(c.name); got != c.want {
			t.Errorf("IsPrivateName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
