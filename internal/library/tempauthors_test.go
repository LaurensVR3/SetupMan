package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/game"
	"setupman/internal/naming"
	"setupman/internal/store"
)

// Temperature labels per author (D-146): changing an author's rule re-reads the details of their
// existing setups, and the overview shows authors, readings and what recorded laps fit.
func TestTempAuthors(t *testing.T) {
	folder := setupFolder(t)
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	for i, n := range []string{"ABC R 3040", "ABC R 16C", "RNG R 2327", "hungo 2026"} {
		os.WriteFile(filepath.Join(spa, n+".json"), []byte(strings.Replace(string(base), `"rearWing": 6`, `"rearWing": `+string(rune('1'+i)), 1)), 0o644)
	}
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, st := newLib(t, g)
	l.Apply()
	meta := func(name string) naming.Meta {
		all, _ := l.db.SetupsForGame("acc")
		for _, s := range all {
			if s.Name == name {
				return s.Meta
			}
		}
		t.Fatalf("%s not found", name)
		return naming.Meta{}
	}
	if m := meta("RNG R 2327"); m.Air != 23 || m.Track != 27 {
		t.Errorf("default reading: %+v", m)
	}
	if m := meta("ABC R 16C"); m.Air != 16 {
		t.Errorf("one number = air by default: %+v", m)
	}

	cfg := st.Load()
	cfg.TempRules = naming.TempRules{"RNG": {Four: naming.FourRange}, "ABC": {Single: naming.SingleTrack}}
	st.Save(cfg)
	l.Apply() // what saving Settings does
	if m := meta("RNG R 2327"); m.TempMin != 23 || m.TempMax != 27 || m.Air != 0 {
		t.Errorf("range rule not applied: %+v", m)
	}
	if m := meta("ABC R 16C"); m.Track != 16 || m.Air != 0 {
		t.Errorf("track rule not applied: %+v", m)
	}
	if m := meta("ABC R 3040"); m.Air != 30 || m.Track != 40 {
		t.Errorf("two numbers of ABC changed: %+v", m)
	}

	ov, err := l.TempAuthors()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]TempAuthor{}
	for _, a := range ov.Authors {
		got[a.Key] = a
	}
	if a := got["ABC"]; a.Count != 2 || !a.Own || a.Rule.Single != naming.SingleTrack || len(a.Examples) != 2 {
		t.Errorf("ABC: %+v", a)
	}
	if a := got["RNG"]; a.Examples[0].Reading != "23–27 °C" {
		t.Errorf("RNG: %+v", a)
	}
	if a := got[naming.DefaultKey]; a.Count != 1 || a.Examples[0].Reading != "air 20 °C, track 26 °C" {
		t.Errorf("names without initials: %+v", a)
	}
}

// The lap hint: the name's numbers compared with the measured air and track temperature.
func TestTempFit(t *testing.T) {
	laps := []store.Lap{{Air: 30.4, Road: 39.6, HasTemps: true}, {Air: 30.8, Road: 40.2, HasTemps: true}}
	var f TempFit
	addFit(&f, naming.Parse("ABC R 3040"), laps) // air 30, track 40
	addFit(&f, naming.Parse("ABC R 31C"), laps)  // the air temperature
	addFit(&f, naming.Parse("ABC R 41C"), laps)  // the track temperature
	addFit(&f, naming.Parse("ABC R 4550"), laps) // neither air/track nor a range around them
	addFit(&f, naming.Parse("ABC R 2635"), laps) // a range around the air temperature
	addFit(&f, naming.Parse("ABC R 3040"), nil)  // no laps: no evidence
	if f != (TempFit{AirTrack: 1, Range: 1, Air: 1, Track: 1}) {
		t.Errorf("fit = %+v", f)
	}
}
