package library

import (
	"testing"
	"time"

	"setupman/internal/game"
)

// The rig case of 2026-11-23 (D-177): a setup picked by hand, then another setup loaded in the
// pits. The pick stays while the car's setup values stay; once they change, SetupMan follows the
// new setup, so laps of both setups don't end up under the first one. Back in the main menu, the
// session ends without closing the game.
func TestRecorderSetupPerStint(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idA, idB := td.SetupIDs[0], td.SetupIDs[1]
	if _, err := l.EditSetup(idB, map[string]string{"basicSetup.strategy.fuel": "60"}, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := l.db.Setup(idB)
	latest, _ := l.db.LatestVersion(idB)
	s.DiskVersionID = latest.ID
	l.db.UpdateSetup(s)

	base := game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "spa", TC: 4, ABS: 3, Map: 1,
		PitMenuKnown: true, PitPressures: [4]float64{25.3, 24.8, 25.1, 24.7}, Fuel: 32}
	feed := func(tm game.Telemetry, laps int, inPit bool) {
		tm.CompletedLaps, tm.InPit = laps, inPit
		if laps > 0 {
			tm.LastLapMs = 100000 + laps
		}
		l.feedTelemetry(g, tm, true)
	}
	feed(base, 0, true)
	feed(base, 0, false)
	if rec := l.Recording(); rec.SetupID != idA {
		t.Fatalf("start: %+v, want a", rec)
	}
	// Picked b by hand on track: this stint and what follows.
	if err := l.SetRecordingSetup(idB); err != nil {
		t.Fatal(err)
	}
	feed(base, 1, false)
	// To the pits and out again with the same setup values: the pick stays.
	feed(base, 1, true)
	feed(base, 1, false)
	if rec := l.Recording(); rec.SetupID != idB {
		t.Fatalf("same values after the pits: %+v, want b (picked by hand)", rec)
	}
	feed(base, 2, false)
	// Another setup loaded in the pits (pit menu values change): SetupMan follows it.
	other := base
	other.PitPressures = [4]float64{25.4, 24.9, 25.3, 24.9}
	feed(other, 2, true)
	feed(other, 2, false)
	if rec := l.Recording(); rec.SetupID != idA {
		t.Fatalf("after loading another setup: %+v, want a", rec)
	}
	feed(other, 3, false)

	rec := l.Recording()
	laps, _ := l.db.LapsForSession(rec.SessionID)
	if len(laps) != 3 || laps[0].SetupID != idB || laps[1].SetupID != idB || laps[2].SetupID != idA {
		t.Fatalf("laps: %+v", laps)
	}
	// Fixed afterwards on the session page: the first stint (lap 1) was a.
	if err := l.SetStintSetup(rec.SessionID, 1, idA); err != nil {
		t.Fatal(err)
	}
	laps, _ = l.db.LapsForSession(rec.SessionID)
	if laps[0].SetupID != idA || laps[2].SetupID != idA {
		t.Errorf("after the stint fix: %+v", laps)
	}

	// Back in the main menu (left the server): the session ends without closing the game.
	offGrace = 0
	defer func() { offGrace = 5 * time.Second }()
	l.feedTelemetry(g, game.Telemetry{Off: true}, true)
	l.feedTelemetry(g, game.Telemetry{Off: true}, true)
	if rec := l.Recording(); rec.Active {
		t.Errorf("still recording in the main menu: %+v", rec)
	}
}

// In a race the setup stays after a pit stop: the fuel added and pit menu changes fit no setup.
func TestRecorderRaceKeepsSetup(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idA, idB := td.SetupIDs[0], td.SetupIDs[1]
	if _, err := l.EditSetup(idB, map[string]string{"basicSetup.strategy.fuel": "60"}, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := l.db.Setup(idB)
	latest, _ := l.db.LatestVersion(idB)
	s.DiskVersionID = latest.ID
	l.db.UpdateSetup(s)
	base := game.Telemetry{Live: true, Session: "race", Car: "ferrari_296_gt3", Track: "spa", TC: 4, ABS: 3, Map: 1,
		PitMenuKnown: true, PitPressures: [4]float64{25.3, 24.8, 25.1, 24.7}, Fuel: 32}
	base.InPit = true
	l.feedTelemetry(g, base, true)
	base.InPit = false
	l.feedTelemetry(g, base, true)
	if rec := l.Recording(); rec.SetupID != idA {
		t.Fatalf("start: %+v", rec)
	}
	stop := base
	stop.CompletedLaps, stop.LastLapMs, stop.InPit = 5, 100000, true
	stop.Fuel, stop.PitPressures = 90, [4]float64{26, 26, 26, 26}
	l.feedTelemetry(g, stop, true)
	stop.InPit = false
	l.feedTelemetry(g, stop, true)
	if rec := l.Recording(); rec.SetupID != idA {
		t.Errorf("after a race stop: %+v, want the same setup", rec)
	}
}
