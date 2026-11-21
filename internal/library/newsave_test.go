package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setupman/internal/game"
	"setupman/internal/settings"
	histstore "setupman/internal/store"
)

// liveFake is a game that reports a running session (for naming new saves).
type liveFake struct {
	*fakeGame
	t game.Telemetry
}

func (f *liveFake) Telemetry() (game.Telemetry, bool) { return f.t, true }

// Rig test R-7 (2026-11-20): a save named "1.10.4 setupman test", made from "ABC R 1.10.3 3040" in
// a race session, gets the driver's initials, the session and the temperature.
func TestNewSaveName(t *testing.T) {
	base, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Setups")
	spa := filepath.Join(dir, "ferrari_296_gt3", "Spa")
	os.MkdirAll(spa, 0o755)
	os.WriteFile(filepath.Join(spa, "ABC R 1.10.3 3040.json"), base, 0o644)

	g := &liveFake{fakeGame: &fakeGame{installed: true, folder: dir}}
	st := settings.NewStore(filepath.Join(t.TempDir(), "settings.json"))
	db, err := histstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l := New([]game.Game{g}, st, db)
	l.Debounce, l.LiveEvery = 50*time.Millisecond, 20*time.Millisecond
	t.Cleanup(l.Stop)
	cfg := st.Load()
	cfg.Initials = "XYZ"
	st.Save(cfg)
	l.Apply()

	// The new save: one click more rear wing, saved under a free name (a branch of the ABC setup).
	os.WriteFile(filepath.Join(spa, "1.10.4 setupman test.json"), []byte(strings.Replace(string(base), `"rearWing": 6`, `"rearWing": 7`, 1)), 0o644)
	l.Rescan("acc")
	var s *histstore.Setup
	all, _ := l.db.SetupsForGame("acc")
	for i := range all {
		if all[i].Name == "1.10.4 setupman test" {
			s = &all[i]
		}
	}
	if s == nil || s.ParentVersionID == 0 {
		t.Fatalf("new save not found or not linked to the ABC setup: %+v", s)
	}

	cases := []struct {
		session   string
		air, road float64
		want      string
	}{
		// Air and track as ACC shows them, rounded (D-145): 30.7 / 39.1 → 3139.
		{"race", 30.7, 39.1, "XYZ R 3139 1.10.4 setupman test"},
		{"race", 44.6, 52.0, "XYZ R 4552 1.10.4 setupman test"},
		{"qualifying", 30.7, 39.1, "XYZ Q 3139 1.10.4 setupman test"},
		{"practice", 30.7, 39.1, "XYZ R 3139 1.10.4 setupman test"}, // practice: the session of the setup it came from
	}
	for _, c := range cases {
		g.t = game.Telemetry{Live: true, Session: c.session, AirTemp: c.air, RoadTemp: c.road, TempsKnown: true}
		if got, why := l.newSaveName(s); got != c.want {
			t.Errorf("%s at %.1f/%.1f °C: %q (%s), want %q", c.session, c.air, c.road, got, why, c.want)
		}
	}
	// Saved outside a session: air and track of the setup it was made from (3040).
	g.t = game.Telemetry{}
	if got, _ := l.newSaveName(s); got != "XYZ R 3040 1.10.4 setupman test" {
		t.Errorf("outside a session: %q", got)
	}
}
