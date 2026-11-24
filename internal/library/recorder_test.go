package library

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"setupman/internal/game"
	"setupman/internal/lastopened"
	"setupman/internal/settings"
	histstore "setupman/internal/store"
)

// A simulated session: out-lap from the pits, three flying laps (one invalid), an in-lap with
// a stop, an out-lap, one more flying lap. Fuel drops 3 L per lap; refuelled at the stop.
func TestRecorder(t *testing.T) {
	folder := setupFolder(t) // ferrari_296_gt3/Spa/{a,b}.json (fuel 30 in both), bmw_m4_gt4/Monza
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idA := td.SetupIDs[0]

	snap := func(laps, lastMs int, fuel float64, inPit, valid bool, road float64) game.Telemetry {
		return game.Telemetry{Live: true, Session: "race", Car: "ferrari_296_gt3", Track: "Spa", CompletedLaps: laps,
			LastLapMs: lastMs, InPit: inPit, PitLaneKnown: true, InPitLane: inPit, ValidKnown: true, ValidLap: valid,
			Fuel: fuel, TempsKnown: true, AirTemp: 20, RoadTemp: road, TC: 4, ABS: 3, Map: 1}
	}
	feed := func(tl game.Telemetry) { l.feedTelemetry(g, tl, true) }

	feed(snap(0, 0, 30, true, true, 30))       // in the pit box, session starts
	feed(snap(0, 0, 30, false, true, 30))      // leaves the pits (setup matched: both a and b fit → unsure)
	feed(snap(1, 140000, 27, false, true, 30)) // out-lap done (pit lap)
	feed(snap(2, 105000, 24, false, true, 31)) // flying lap
	feed(snap(2, 105000, 23, false, false, 31))
	feed(snap(3, 104000, 21, false, true, 31)) // invalid lap (cut seen during the lap)
	feed(snap(4, 106000, 18, false, true, 32)) // flying lap
	feed(snap(4, 0, 18, true, true, 32))       // into the pits
	feed(snap(4, 0, 50, true, true, 32))       // refuelled
	feed(snap(5, 130000, 50, true, true, 32))  // in-lap/stop lap completes in the pit lane
	feed(snap(5, 0, 50, false, true, 32))      // leaves the pits
	feed(snap(6, 120000, 47, false, true, 32)) // out-lap
	feed(snap(7, 104500, 44, false, true, 33)) // flying lap

	if rec := l.Recording(); !rec.Active || rec.Laps != 7 {
		t.Fatalf("recording: %+v", rec)
	}
	// Both setups fit the telemetry equally (identical values) → no automatic pick; the user picks.
	sessions, _ := l.db.SessionsForSetup(idA)
	if len(sessions) != 0 {
		t.Fatalf("expected no automatic match, got %+v", sessions)
	}
	rec := l.Recording()
	s, _ := l.db.Session(1)
	if s == nil || s.Match != "none" {
		t.Fatalf("session: %+v (recording %+v)", s, rec)
	}
	if err := l.SetSessionSetup(s.ID, idA); err != nil {
		t.Fatal(err)
	}

	st, err := l.SetupLapTimes(idA)
	if err != nil {
		t.Fatal(err)
	}
	o := st.Overall
	// Laps: out(pit) 140.0, 105.0, 104.0 invalid, 106.0, stop(pit) 130.0, out(pit) 120.0, 104.5
	if o.Laps != 7 || o.Clean != 3 {
		t.Errorf("laps %d clean %d, want 7 / 3: %+v", o.Laps, o.Clean, o)
	}
	if o.BestMs != 104500 { // the invalid 104.0 doesn't count
		t.Errorf("best = %d, want 104500", o.BestMs)
	}
	if want := 105167; o.AvgMs != want { // (105000 + 106000 + 104500) / 3 = 105166.67, rounded
		t.Errorf("avg = %d, want %d", o.AvgMs, want)
	}
	if o.FuelPerLap < 2.99 || o.FuelPerLap > 3.01 {
		t.Errorf("fuel per lap = %.3f, want 3 (refuel lap ignored)", o.FuelPerLap)
	}
	if len(st.Points) != 3 || len(st.Sessions) != 1 || st.Sessions[0].Match != "manual" {
		t.Errorf("points %d sessions %+v", len(st.Points), st.Sessions)
	}

	// Game closes: recording stops.
	l.feedTelemetry(g, game.Telemetry{}, false)
	if l.Recording().Active {
		t.Error("still recording after the game closed")
	}

	ts, _ := l.TrackLapTimes("acc", "ferrari_296_gt3", "spa")
	if ts.Laps != 7 || ts.BestMs != 104500 {
		t.Errorf("track stats: %+v", ts)
	}
}

func TestRecorderMatchesSetup(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	// Make b different: 60 L of fuel. Then telemetry with 60 L picks b.
	idB := td.SetupIDs[1]
	if _, err := l.EditSetup(idB, map[string]string{"basicSetup.strategy.fuel": "60"}, ""); err != nil {
		t.Fatal(err)
	}
	// The edit is library-only (unmanaged), so the file still says 30: stage the match on the
	// version in the folder. Use a fresh setup made "in the folder" instead: re-scan b as edited.
	s, _ := l.db.Setup(idB)
	latest, _ := l.db.LatestVersion(idB)
	s.DiskVersionID = latest.ID
	l.db.UpdateSetup(s)

	// The file's 60 is 62 L in the car (the file stores 2 L less, D-144).
	tl := game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "spa", Fuel: 62, TC: 4, ABS: 3, Map: 1}
	l.feedTelemetry(g, tl, true)
	if rec := l.Recording(); rec.SetupID != idB {
		t.Errorf("matched %d (%s), want b (%d)", rec.SetupID, rec.SetupName, idB)
	}
}

// Rig test 2026-11-20 (R-2): when a session loads, ACC first reports placeholders (0 L fuel,
// ABS 5, map 2). SetupMan must wait for real values instead of giving up on the setup.
func TestRecorderWaitsForRealValues(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idB := td.SetupIDs[1]
	if _, err := l.EditSetup(idB, map[string]string{"basicSetup.strategy.fuel": "60"}, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := l.db.Setup(idB)
	latest, _ := l.db.LatestVersion(idB)
	s.DiskVersionID = latest.ID
	l.db.UpdateSetup(s)

	base := game.Telemetry{Live: true, Session: "race", Car: "ferrari_296_gt3", Track: "spa"}
	placeholder := base
	placeholder.TC, placeholder.ABS, placeholder.Map = 5, 5, 2
	l.feedTelemetry(g, placeholder, true)
	if rec := l.Recording(); !rec.Active || rec.SetupID != 0 {
		t.Fatalf("placeholders: %+v, want recording without a setup yet", rec)
	}
	// In the pit box with real values, before the first lap: recognised there already (D-152).
	pitBox := base
	pitBox.Fuel, pitBox.TC, pitBox.ABS, pitBox.Map, pitBox.InPit = 62, 4, 3, 1, true
	l.feedTelemetry(g, pitBox, true)
	if rec := l.Recording(); rec.SetupID != idB {
		t.Fatalf("not recognised in the pit box: %+v", rec)
	}
	// Setup a loaded in the pit box instead (its file says 30 L, so 32 L in the car): looked at again.
	other := pitBox
	other.Fuel = 32
	l.feedTelemetry(g, other, true)
	if rec := l.Recording(); rec.SetupID != td.SetupIDs[0] {
		t.Fatalf("pit box change: %+v, want a (%d)", rec, td.SetupIDs[0])
	}
	// Out of the pits: recognised.
	out := pitBox
	out.InPit = false
	l.feedTelemetry(g, out, true)
	if rec := l.Recording(); rec.SetupID != idB {
		t.Errorf("matched %d (%s), want b (%d)", rec.SetupID, rec.SetupName, idB)
	}
}

// A race that starts on the grid never leaves the pits: recognise the setup as soon as the car
// reports real values (the rig case of 2026-11-20).
func TestRecorderGridStart(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idB := td.SetupIDs[1]
	if _, err := l.EditSetup(idB, map[string]string{"basicSetup.strategy.fuel": "60"}, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := l.db.Setup(idB)
	latest, _ := l.db.LatestVersion(idB)
	s.DiskVersionID = latest.ID
	l.db.UpdateSetup(s)

	tl := game.Telemetry{Live: true, Session: "race", Car: "ferrari_296_gt3", Track: "spa", TC: 5, ABS: 5, Map: 2}
	l.feedTelemetry(g, tl, true) // placeholders
	tl.Fuel, tl.TC, tl.ABS, tl.Map = 62, 4, 3, 1
	l.feedTelemetry(g, tl, true) // real values, still on the grid
	if rec := l.Recording(); rec.SetupID != idB {
		t.Errorf("matched %d (%s), want b (%d)", rec.SetupID, rec.SetupName, idB)
	}
}

// startedGame is a fake game that reports when its program started (D-147).
type startedGame struct {
	*fakeGame
	at time.Time
}

func (s startedGame) StartedAt() (time.Time, bool) { return s.at, true }

// D-147: the setup file the game opened last since it started is the one being driven, even when
// the values can't tell the setups apart. Files opened before the game started don't count.
func TestRecorderRecognisesOpenedFile(t *testing.T) {
	if st := lastopened.Check(); !st.Known || !st.Enabled {
		t.Skip("last-opened times are not recorded on this PC")
	}
	folder := setupFolder(t)
	fg := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, fg)
	l.Apply() // SetupMan's own scan reads the files without leaving a trace
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	track := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	old := time.Now().Add(-3 * time.Hour)
	for _, n := range []string{"a.json", "b.json"} {
		os.Chtimes(filepath.Join(track, n), old, old)
	}
	tl := game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "spa", Fuel: 32, TC: 4, ABS: 3, Map: 1}

	// Nothing opened since the game started, and a and b have the same values: unknown.
	g := startedGame{fg, time.Now().Add(-time.Hour)}
	l.feedTelemetry(g, tl, true)
	if rec := l.Recording(); rec.SetupID != 0 {
		t.Fatalf("recognised %q without a clue", rec.SetupName)
	}

	// The game loads b: it reads the file.
	if _, err := os.ReadFile(filepath.Join(track, "b.json")); err != nil {
		t.Fatal(err)
	}
	tl.Session = "qualify" // a new session looks again
	l.feedTelemetry(g, tl, true)
	var idB int64
	for _, id := range td.SetupIDs {
		if s, _ := l.db.Setup(id); s != nil && s.Name == "b" {
			idB = id
		}
	}
	if rec := l.Recording(); rec.SetupID != idB {
		t.Fatalf("recognised %d (%s), want b (%d)", rec.SetupID, rec.SetupName, idB)
	}
}

func TestNewestOpened(t *testing.T) {
	now := time.Now()
	var zero time.Time
	cases := []struct {
		times []time.Time
		want  []int
	}{
		{[]time.Time{zero, zero}, nil},
		{[]time.Time{now.Add(-time.Minute), now, zero}, []int{1}},
		{[]time.Time{now.Add(-time.Second), now, now.Add(-time.Hour)}, []int{0, 1}},
	}
	for _, c := range cases {
		if got := newestOpened(c.times); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("newestOpened = %v, want %v", got, c.want)
		}
	}
}

// D-149: when setups fit equally well, they are offered for manual setup select; one pick sets the
// session's setup and the offer goes away.
func TestRecorderManualSetupSelect(t *testing.T) {
	folder := setupFolder(t) // a and b are the same setup
	g := &fakeGame{installed: true, folder: folder}
	l, _ := newLib(t, g)
	l.Apply()
	tl := game.Telemetry{Live: true, Session: "race", Car: "ferrari_296_gt3", Track: "spa", Fuel: 32, TC: 4, ABS: 3, Map: 1}
	l.feedTelemetry(g, tl, true)
	rec := l.Recording()
	if rec.SetupID != 0 || len(rec.Likely) != 2 || len(rec.Choices) != 2 || rec.SessionID == 0 {
		t.Fatalf("want two likely setups offered, got %+v", rec)
	}
	if err := l.SetSessionSetup(rec.SessionID, rec.Likely[1].ID); err != nil {
		t.Fatal(err)
	}
	rec = l.Recording()
	if rec.SetupID != rec.Choices[1].ID || len(rec.Likely) != 0 || rec.SetupName == "" {
		t.Fatalf("after the pick: %+v", rec)
	}
	// Leaving the pits again does not undo the pick.
	tl.InPit = true
	l.feedTelemetry(g, tl, true)
	tl.InPit = false
	l.feedTelemetry(g, tl, true)
	if got := l.Recording(); got.SetupID != rec.SetupID {
		t.Fatalf("pick undone: %+v", got)
	}
}

// Rig log 2026-11-21: ACC's setup screen reports no live session, so a save made there got the
// parent setup's temperatures. During a recorded session, the session's latest ones count.
func TestLiveSessionInMenus(t *testing.T) {
	folder := setupFolder(t)
	g := &teleGame{fakeGame: &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}}
	store := settings.NewStore(filepath.Join(t.TempDir(), "settings.json"))
	db, err := histstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l := New([]game.Game{g}, store, db)
	l.Debounce, l.LiveEvery = 50*time.Millisecond, time.Hour
	t.Cleanup(l.Stop)
	l.Apply()
	live := game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "spa", Fuel: 30,
		TempsKnown: true, AirTemp: 20.4, RoadTemp: 21.9}
	g.set(live)
	l.feedTelemetry(g, live, true)
	menus := live
	menus.Live, menus.TempsKnown = false, false // the setup screen
	g.set(menus)
	got, ok := l.liveSession("acc")
	if !ok || !got.TempsKnown || got.AirTemp != 20.4 || got.RoadTemp != 21.9 || got.Session != "practice" {
		t.Fatalf("liveSession in the menus = %+v, %v", got, ok)
	}
}

// The game restarts its lap clock without counting a lap when the car crosses the line on its way
// out of the pit box: the first counted lap starts there, so it is a flying lap with its own fuel
// (rig 2026-11-24, Imola: lap 1 had 5.3 L and a pit mark because it held the out-lap).
func TestRecorderUncountedOutLap(t *testing.T) {
	old := lapRestartWait
	lapRestartWait = 0
	defer func() { lapRestartWait = old }()
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Apply()
	snap := func(laps, lastMs, clock int, fuel float64, inPit bool) game.Telemetry {
		return game.Telemetry{Live: true, Session: "practice", Car: "ferrari_296_gt3", Track: "Spa", CompletedLaps: laps,
			LastLapMs: lastMs, CurrentLapMs: clock, CurrentKnown: true, InPit: inPit, PitLaneKnown: true, InPitLane: inPit,
			Fuel: fuel, TC: 4, ABS: 3, Map: 1}
	}
	feed := func(tl game.Telemetry) { l.feedTelemetry(g, tl, true) }
	feed(snap(0, 0, 0, 30, true))           // pit box
	feed(snap(0, 0, 60000, 29.5, false))    // out-lap
	feed(snap(0, 0, 128000, 27.5, false))   // just before the line
	feed(snap(0, 0, 500, 27.4, false))      // line crossed: clock restarts, no lap counted
	feed(snap(0, 0, 60000, 26, false))      // flying lap
	feed(snap(1, 101000, 400, 24.7, false)) // counted together with the clock restart
	feed(snap(1, 101000, 50000, 23.4, false))
	feed(snap(2, 102000, 1500, 22, false)) // the next lap
	laps, err := l.db.LapsForSession(1)
	if err != nil || len(laps) != 2 {
		t.Fatalf("laps: %+v %v", laps, err)
	}
	if laps[0].Pit || laps[0].FuelUsed < 2.69 || laps[0].FuelUsed > 2.71 {
		t.Errorf("first lap: pit %v fuel %.2f, want a flying lap with 2.7 L", laps[0].Pit, laps[0].FuelUsed)
	}
	if laps[1].Pit || laps[1].FuelUsed < 2.69 || laps[1].FuelUsed > 2.71 {
		t.Errorf("second lap: pit %v fuel %.2f, want 2.7 L", laps[1].Pit, laps[1].FuelUsed)
	}
}
