package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/game"
)

func TestDuplicates(t *testing.T) {
	folder := setupFolder(t) // Spa: a.json and b.json are identical
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	os.WriteFile(filepath.Join(spa, "c.json"), []byte(strings.Replace(string(base), `"rearWing": 6`, `"rearWing": 7`, 1)), 0o644)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })

	exact := l.Duplicates("acc", "", "", false)
	if len(exact) != 1 || len(exact[0].Setups) != 2 || !exact[0].Identical || exact[0].Keep == 0 {
		t.Fatalf("exact: %+v", exact)
	}
	near := l.Duplicates("acc", "ferrari_296_gt3", "spa", true)
	if len(near) != 1 || len(near[0].Setups) != 3 || near[0].Identical {
		t.Fatalf("near: %+v", near)
	}
	if other := l.Duplicates("acc", "bmw_m4_gt4", "monza", true); len(other) != 0 {
		t.Errorf("monza has no duplicates: %+v", other)
	}
}

func TestProposeKeep(t *testing.T) {
	set := func(names ...string) []DupSetup {
		out := make([]DupSetup, len(names))
		for i, n := range names {
			out[i] = DupSetup{ID: int64(i + 1), Name: n}
		}
		return out
	}
	for _, c := range []struct {
		names []string
		keep  int64
	}{
		{[]string{"quali (2)", "quali"}, 2},
		{[]string{"LA 296 BAR Q - Copy", "LA 296 BAR Q"}, 2},
		{[]string{"yt (15)", "yt 16", "yt (1)"}, 2}, // "yt 16" is a name, not a copy mark
		{[]string{"race copy 2", "race"}, 2},
		{[]string{"a (2)", "a (3)"}, 1},                 // all marked: the newest
		{[]string{"BGK WET (6) (1)", "BGK WET (6)"}, 2}, // fewest marks
		{[]string{"new", "old"}, 1},                     // none marked: the newest
		// Rig test R-10: the name that follows the naming standard, even when older.
		{[]string{"1.10.4 setupman test", "ABC R 1.10.3 3040"}, 2},
		{[]string{"ABC R 3040 (2)", "test"}, 2}, // copy marks still count first
	} {
		if got := proposeKeep(set(c.names...)); got != c.keep {
			t.Errorf("%v: keep %d, want %d", c.names, got, c.keep)
		}
	}
}

// The overview counts each track once, however many cars have setups for it (owner report
// 2026-11-23: ACC showed 70 tracks, the number of car-and-track pairs).
func TestGameSummaryTracks(t *testing.T) {
	folder := setupFolder(t) // ferrari_296_gt3/Spa, bmw_m4_gt4/monza
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	bmwSpa := filepath.Join(folder, "bmw_m4_gt4", "Spa")
	os.MkdirAll(bmwSpa, 0o755)
	os.WriteFile(filepath.Join(bmwSpa, "x.json"), base, 0o644)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	if s := l.Games()[0]; s.Cars != 2 || s.Tracks != 2 {
		t.Fatalf("cars %d, tracks %d; want 2 and 2", s.Cars, s.Tracks)
	}
}
