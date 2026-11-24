package naming

import "testing"

// Temperature labels per author (D-146): defaults, an author who gives only the air temperature
// and one who writes ranges; names keep their author's way of writing when renamed.
func TestTempRules(t *testing.T) {
	rules := TempRules{
		"RNG": {Four: FourRange},
		"TRK": {Single: SingleTrack},
		"EIT": {Single: SingleEither},
	}
	cases := []struct {
		name                         string
		air, track, tempMin, tempMax int
		suggest                      string
	}{
		{"ABC R 3040 1.10.3", 30, 40, 0, 0, "ABC R 3040 1.10.3"}, // default: air, track
		{"ABC R 16C 1.10.3", 16, 0, 0, 0, "ABC R 16C 1.10.3"},    // default: one number is the air
		{"hungo 22c", 22, 0, 0, 0, "R 22C hungo"},                // no initials: the default
		{"RNG R 2327", 0, 0, 23, 27, "RNG R 2327"},               // this author writes ranges
		{"RNG R 21 25", 0, 0, 21, 25, "RNG R 2125"},
		{"TRK R 38C", 0, 38, 0, 0, "TRK R 38C"},              // this author gives the track temperature
		{"EIT R 20C", 0, 0, 20, 20, "EIT R 20C"},             // not said
		{"ABC R 23-27C", 0, 0, 23, 27, "ABC R 2327"},         // a dash is always a range
		{"-rng_3040_test", 0, 0, 30, 40, "-rng R 3040 test"}, // "-name" initials, any case
	}
	for _, c := range cases {
		m := ParseWith(c.name, rules)
		if m.Air != c.air || m.Track != c.track || m.TempMin != c.tempMin || m.TempMax != c.tempMax {
			t.Errorf("%q: air %d track %d range %d–%d", c.name, m.Air, m.Track, m.TempMin, m.TempMax)
		}
		if got := Suggest(m, SessionR, false, c.name); got != c.suggest {
			t.Errorf("Suggest(%q) = %q, want %q", c.name, got, c.suggest)
		}
	}
	// A default rule for everyone ("*") is overruled by an author's own rule.
	all := TempRules{DefaultKey: {Single: SingleTrack}, "ABC": {Single: SingleAir}}
	if m := ParseWith("XYZ 30C", all); m.Track != 30 {
		t.Errorf("default rule not used: %+v", m)
	}
	if m := ParseWith("ABC 30C", all); m.Air != 30 {
		t.Errorf("author rule not used: %+v", m)
	}
}

// A rule can be set for any word in a name, for authors who don't put initials in front.
func TestTempRuleForWord(t *testing.T) {
	rules := TempRules{"CDA": {Single: SingleTrack}}
	if m := ParseWith("R 34C 1.10 CDA M LFM DRY 01", rules); m.Track != 34 {
		t.Errorf("word rule not used: %+v", m)
	}
	if m := ParseWith("R 34C 1.10 other", rules); m.Air != 34 {
		t.Errorf("default not used: %+v", m)
	}
	if m := ParseWith("ABC R 34C CDA", rules); m.Air != 34 { // initials come first
		t.Errorf("initials should decide: %+v", m)
	}
	words := AuthorWords([]string{"R 2327 CDA M LFM", "Q 2026 cda lfm", "R 14C CDA", "hungo 2026", "R 2026 test"}, 2, map[string]bool{"hungo": true})
	if len(words) != 2 || words[0] != (WordCount{"CDA", 3}) || words[1] != (WordCount{"LFM", 2}) {
		t.Errorf("words = %+v", words)
	}
}
