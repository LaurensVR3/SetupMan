package naming

import "testing"

func TestSuggest(t *testing.T) {
	cases := []struct {
		name    string
		session string
		wet     bool
		want    string
	}{
		{"ABC R 1.10.4 2327 TC25", SessionR, false, "ABC R 2327 1.10.4 TC25"},
		{"SCG F296 HUN  1.42.9   Q 21 25", SessionQ, false, "SCG Q 2125 1.42.9 F296 HUN"},
		{"hungo25m 2530", SessionR, false, "R 25m 2530 hungo"},
		{"LA 296 HUN Q", SessionQ, false, "LA Q 296 HUN"},
		{"-abesports_HUN_23_27", "", false, "abesports 2327 HUN"},
		{"45m 1 stp 2026", SessionR, false, "R 45m 1stop 2026"},
		{"hungo", "", true, "WET hungo"},
		{"tst", "", false, "tst"},
		{"19c 21c CDA M LFM Q DRY 01", SessionQ, false, "Q 1921 CDA M LFM DRY 01"},
		{"2123R", SessionR, false, "R 2123"},
		{"HUN W2429", "", false, "HUN 2429 W"},
		{"GO2 296 GT3 SIL E R01 23C", SessionR, false, "R 23C GO2 GT3 296 SIL E"},
		{"ABC 1313 R 1.10.4 tc36", SessionR, false, "ABC R 1313 1.10.4 tc36"}, // air 13, track 13
		{"19c 21c CDA4 QF DRY 02", SessionQ, false, "Q 1921 CDA4 QF DRY 02"},
	}
	for _, c := range cases {
		if got := Suggest(Parse(c.name), c.session, c.wet, c.name); got != c.want {
			t.Errorf("Suggest(%q) = %q, want %q", c.name, got, c.want)
		}
	}
	// Reading a suggested name back gives the same details (names stay consistent).
	m := Parse("ABC R 2327 1.10.4 TC25")
	if m.Initials != "ABC" || m.Session != SessionR || m.Air != 23 || m.Track != 27 || m.GameVersion != "1.10.4" || len(m.Codes) != 1 {
		t.Errorf("round trip: %+v", m)
	}
}

// A name that is only initials keeps them in front (seen in the user's library: "BGK").
func TestSuggestInitialsOnly(t *testing.T) {
	m := Parse("BGK")
	if m.Initials != "BGK" {
		t.Fatalf("initials %q", m.Initials)
	}
	m.GameVersion = "1.9.2"
	if got := Suggest(m, "", false, "BGK"); got != "BGK 1.9.2" {
		t.Errorf("got %q", got)
	}
	if Parse("WET").Initials != "" || Parse("QUALI").Initials != "" || Parse("ENDU").Initials != "" {
		t.Error("a keyword was taken as initials")
	}
}
