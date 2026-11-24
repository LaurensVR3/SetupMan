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

// Equally likely setups: the setup of the previous session on this car and track wins (D-180).
func TestRecorderPreviousSessionBreaksTie(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idB := td.SetupIDs[1]
	// a and b have the same values: a tie
	tie := game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "spa", TC: 4, ABS: 3, Map: 1, Fuel: 32}
	tie.InPit = true
	l.feedTelemetry(g, tie, true)
	tie.InPit = false
	l.feedTelemetry(g, tie, true)
	if rec := l.Recording(); rec.SetupID != 0 || len(rec.Likely) != 2 {
		t.Fatalf("first session: %+v, want a tie", rec)
	}
	if err := l.SetRecordingSetup(idB); err != nil {
		t.Fatal(err)
	}
	// next session (qualifying): the same tie, b was driven before
	q := tie
	q.Session, q.InPit = "qualifying", true
	l.feedTelemetry(g, q, true)
	q.InPit = false
	l.feedTelemetry(g, q, true)
	if rec := l.Recording(); rec.SetupID != idB {
		t.Errorf("qualifying: %+v, want b from the previous session", rec)
	}
}

// SetupMan restarted during a session: the first session it sees continues the recorded one (same
// car, track and kind, begun after the game started); a session from an earlier run of the game
// does not (A-5).
func TestRecorderResumesAfterRestart(t *testing.T) {
	folder := setupFolder(t)
	fg := &fakeGame{installed: true, folder: folder}
	g := startedGame{fakeGame: fg, at: time.Now().Add(-time.Hour)}
	l, _ := newLib(t, fg)
	l.Apply()
	base := game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "spa", TC: 4, ABS: 3, Map: 1, Fuel: 32}
	l.feedTelemetry(g, base, true)
	first := l.Recording().SessionID
	lap := base
	lap.CompletedLaps, lap.LastLapMs = 1, 100000
	l.feedTelemetry(g, lap, true)

	// "restart": the recorder is gone, the session is in the library
	l.recMu.Lock()
	l.recs, l.resumeTried = map[string]*recorder{}, nil
	l.recMu.Unlock()
	again := base // ACC reported 0 laps again after the restart (rig log 2026-11-23 19:29)
	l.feedTelemetry(g, again, true)
	if rec := l.Recording(); rec.SessionID != first || rec.Laps != 1 {
		t.Fatalf("after a restart: %+v, want session %d with 1 lap", rec, first)
	}
	lap2 := base
	lap2.CompletedLaps, lap2.LastLapMs = 1, 101000
	l.feedTelemetry(g, lap2, true)
	if laps, _ := l.db.LapsForSession(first); len(laps) != 2 {
		t.Errorf("laps in the continued session: %d, want 2", len(laps))
	}

	// a game started after the session: a new session
	l.recMu.Lock()
	l.recs, l.resumeTried = map[string]*recorder{}, nil
	l.recMu.Unlock()
	g.at = time.Now().Add(time.Minute)
	l.feedTelemetry(g, base, true)
	if rec := l.Recording(); rec.SessionID == first {
		t.Errorf("continued a session from an earlier run of the game: %+v", rec)
	}
}

// A setup picked by hand still gives way to another loaded setup after SetupMan was restarted
// during the session (D-177, A-5): the pick stays while the car's values stay, then SetupMan follows.
func TestRecorderManualPickAfterRestart(t *testing.T) {
	folder := setupFolder(t)
	fg := &fakeGame{installed: true, folder: folder}
	g := startedGame{fakeGame: fg, at: time.Now().Add(-time.Hour)}
	l, _ := newLib(t, fg)
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
	if err := l.SetRecordingSetup(idB); err != nil {
		t.Fatal(err)
	}
	// "restart": the recorder is gone, the session (with its hand-picked setup) is in the library
	l.recMu.Lock()
	l.recs, l.resumeTried = map[string]*recorder{}, nil
	l.recMu.Unlock()
	feed(base, 0, false)
	if rec := l.Recording(); rec.SetupID != idB {
		t.Fatalf("after the restart: %+v, want b (picked by hand)", rec)
	}
	feed(base, 1, false)
	// another setup loaded in the pits: SetupMan follows it
	other := base
	other.PitPressures = [4]float64{25.4, 24.9, 25.3, 24.9}
	feed(other, 1, true)
	feed(other, 1, false)
	if rec := l.Recording(); rec.SetupID != idA {
		t.Errorf("after loading another setup: %+v, want a", rec)
	}
}
