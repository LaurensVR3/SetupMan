package acc

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setupman/internal/game"
)

func TestNames(t *testing.T) {
	name, brand, class, year := CarInfo("amr_v12_vantage_gt3")
	if name != "Aston Martin V12 Vantage GT3" || brand != "Aston Martin" || class != ClassGT3 || year != 2013 {
		t.Errorf("got %q %q %q %d", name, brand, class, year)
	}
	if name, _, class, _ := CarInfo("Some_New_Car_GT3"); name != "Some New Car GT3" || class != ClassOther {
		t.Errorf("fallback: %q %q", name, class)
	}
	if got := TrackName("Spa"); got != "Spa-Francorchamps" {
		t.Errorf("TrackName(Spa) = %q", got)
	}
	if got := TrackName("new_track"); got != "New Track" {
		t.Errorf("fallback track = %q", got)
	}
	if got := Tidy("mclaren_720s_gt3"); got != "Mclaren 720S GT3" {
		t.Errorf("Tidy = %q", got)
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	steam := filepath.Join(dir, "Steam")
	lib2 := filepath.Join(dir, "Games", "SteamLibrary")
	os.MkdirAll(filepath.Join(steam, "steamapps"), 0o755)
	os.MkdirAll(filepath.Join(lib2, "steamapps"), 0o755)
	vdf := `"libraryfolders"
{
	"0" { "path" "` + escapeVDF(steam) + `" }
	"1" { "path" "` + escapeVDF(lib2) + `" }
}`
	os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)

	libs := librariesFromSteam(steam)
	if len(libs) != 2 || libs[1] != lib2 {
		t.Fatalf("libraries = %v", libs)
	}

	docs := filepath.Join(dir, "Documents")
	if d := detect(libs, docs); d.Installed || d.Folder != "" {
		t.Errorf("nothing installed yet: %+v", d)
	}
	os.WriteFile(filepath.Join(lib2, "steamapps", "appmanifest_805550.acf"), []byte("x"), 0o644)
	if d := detect(libs, docs); !d.Installed || d.Folder != "" {
		t.Errorf("installed via second library, no setups folder yet: %+v", d)
	}
	setups := filepath.Join(docs, "Assetto Corsa Competizione", "Setups")
	os.MkdirAll(setups, 0o755)
	if d := detect(nil, docs); !d.Installed || d.Folder != setups {
		t.Errorf("setups folder alone counts: %+v", d)
	}
}

func escapeVDF(p string) string {
	out := ""
	for _, r := range p {
		if r == '\\' {
			out += `\\`
		} else {
			out += string(r)
		}
	}
	return out
}

func putF(b []byte, off int, v float32) { binary.LittleEndian.PutUint32(b[off:], math.Float32bits(v)) }
func putI(b []byte, off int, v int32)   { binary.LittleEndian.PutUint32(b[off:], uint32(v)) }
func putW(b []byte, off int, s string) {
	for i, r := range s {
		binary.LittleEndian.PutUint16(b[off+2*i:], uint16(r))
	}
}

func TestParseSharedMemory(t *testing.T) {
	g := make([]byte, graphicsReadSize)
	p := make([]byte, physicsReadSize)
	st := make([]byte, staticReadSize)
	putI(g, offStatus, statusLive)
	putI(g, offSession, 2)
	putI(g, offCompletedLaps, 7)
	putI(g, offLastLapMs, 105123)
	putI(g, offIsInPit, 1)
	putI(g, offIsInPitLane, 1)
	putI(g, offTC, 3)
	putI(g, offEngineMap, 1)
	putI(g, offABS, 2)
	putI(g, offIsValidLap, 1)
	putF(p, offFuel, 43.5)
	putF(p, offSpeedKmh, 123.5)
	putF(p, offAirTemp, 22)
	putF(p, offRoadTemp, 31)
	putW(st, offCarModel, "ferrari_296_gt3")
	putW(st, offTrack, "spa")

	s := parse(p, g, st)
	tl := s.t
	if !s.ok || s.status != statusLive || !s.inPit || s.speed != 123.5 {
		t.Fatalf("sample: %+v", s)
	}
	if !tl.Live || tl.Session != "race" || tl.CompletedLaps != 7 || tl.LastLapMs != 105123 || tl.Fuel != 43.5 {
		t.Errorf("telemetry: %+v", tl)
	}
	if !tl.PitLaneKnown || !tl.InPitLane || !tl.ValidKnown || !tl.ValidLap || !tl.TempsKnown || tl.RoadTemp != 31 {
		t.Errorf("flags/temps: %+v", tl)
	}
	if tl.TC != 3 || tl.ABS != 2 || tl.Map != 1 || tl.Car != "ferrari_296_gt3" || tl.Track != "spa" {
		t.Errorf("electronics/names: %+v", tl)
	}

	// Implausible values are marked unknown instead of trusted.
	putI(g, offIsValidLap, 12345)
	putF(p, offRoadTemp, 900)
	putI(g, offTC, -77)
	tl = parse(p, g, st).t
	if tl.ValidKnown || tl.TempsKnown || tl.TC != -1 {
		t.Errorf("implausible values trusted: %+v", tl)
	}
	if parse(p[:10], g, st).ok {
		t.Error("short buffer accepted")
	}
}

// Assetto Corsa's graphics page is shorter, with the pit-lane flag at its own place and no
// lap-valid flag, electronics, rain tyres, stint, stops or pit menu (D-137).
func TestParseACSharedMemory(t *testing.T) {
	g := make([]byte, acLayout.graphicsReadSize)
	p := make([]byte, physicsReadSize)
	st := make([]byte, staticReadSize)
	putI(g, offStatus, statusLive)
	putI(g, offSession, 5) // drift: AC only
	putI(g, offCompletedLaps, 3)
	putI(g, offLastLapMs, 98765)
	putI(g, 276, 1) // isInPitLane
	putF(g, offTimeLeft, 600_000)
	putF(p, offFuel, 20)
	putF(p, offAirTemp, 18)
	putF(p, offRoadTemp, 25)
	for i := 0; i < 4; i++ {
		putF(p, offPressures+4*i, 27.5)
	}
	putW(st, offCarModel, "ks_ferrari_488_gt3")
	putW(st, offTrack, "ks_nurburgring")

	s := acLayout.parse(p, g, st)
	tl := s.t
	if !s.ok || !tl.Live || tl.Session != "drift" || tl.CompletedLaps != 3 || tl.LastLapMs != 98765 || tl.Fuel != 20 {
		t.Fatalf("telemetry: %+v", tl)
	}
	if !tl.PitLaneKnown || !tl.InPitLane || !tl.PressuresKnown || !tl.TempsKnown || !tl.TimeLeftKnown {
		t.Errorf("known values: %+v", tl)
	}
	if tl.ValidKnown || tl.WetKnown || tl.StintKnown || tl.StopsKnown || tl.PitMenuKnown || tl.TC != -1 || tl.ABS != -1 || tl.Map != -1 {
		t.Errorf("AC has no such values, but they were read: %+v", tl)
	}
	if tl.Car != "ks_ferrari_488_gt3" || tl.Track != "ks_nurburgring" {
		t.Errorf("names: %+v", tl)
	}
	if acLayout.parse(p, g[:200], st).ok {
		t.Error("short graphics page accepted")
	}
}

func TestMatchSetup(t *testing.T) {
	base, _ := os.ReadFile("testdata/base.json") // fuel 30, tC1 4, abs 3, eCUMap 1
	other := []byte(strings.Replace(strings.Replace(string(base), `"fuel": 30`, `"fuel": 60`, 1), `"tC1": 4`, `"tC1": 6`, 1))
	g := New()
	tl := game.Telemetry{Fuel: 30, TC: 4, ABS: 3, Map: 2}
	if got := g.MatchSetup(tl, [][]byte{other, base}); fmt.Sprint(got) != "[1]" {
		t.Errorf("match = %v, want [1]", got)
	}
	if got := g.MatchSetup(game.Telemetry{Fuel: 99, TC: -1, ABS: -1, Map: -1}, [][]byte{other, base}); len(got) != 0 {
		t.Errorf("no clue should give nothing, got %v", got)
	}
	if got := g.MatchSetup(game.Telemetry{}, [][]byte{base}); fmt.Sprint(got) != "[0]" {
		t.Errorf("single candidate should match, got %v", got)
	}
	if got := g.MatchSetup(tl, [][]byte{base, base}); fmt.Sprint(got) != "[0 1]" {
		t.Errorf("equal setups should both be offered, got %v", got)
	}
}

// Rig log 2026-11-21 (D-149): two setups with the same fuel, TC, ABS and map ("1616" and "2027");
// ACC's pit menu showed the loaded setup's first pit-strategy pressures, which tell them apart.
func TestMatchSetupPitStrategy(t *testing.T) {
	dir := "../../../SampleSetups/Assetto Corsa Competizione/ferrari_296_gt3/Imola/"
	s1616, err1 := os.ReadFile(dir + "BGK R 16C 1.10.3.json")
	s2027, err2 := os.ReadFile(dir + "BGK R 2027 1.10.3.json")
	if err1 != nil || err2 != nil {
		t.Skip("SampleSetups not present")
	}
	g := New()
	tl := game.Telemetry{Fuel: 13.9, TC: 6, ABS: 2, Map: 0}
	if got := g.MatchSetup(tl, [][]byte{s1616, s2027}); fmt.Sprint(got) != "[0 1]" {
		t.Fatalf("without the pit menu both fit: got %v", got)
	}
	tl.PitMenuKnown, tl.PitPressures = true, [4]float64{25.8, 25.4, 25.0, 24.6}
	if got := g.MatchSetup(tl, [][]byte{s1616, s2027}); fmt.Sprint(got) != "[1]" {
		t.Errorf("with the pit menu: got %v, want [1] (2027)", got)
	}
}

func TestDriveState(t *testing.T) {
	var d driveState
	t0 := time.Now()
	live := func(speed float32, inPit bool) sample {
		return sample{ok: true, status: statusLive, speed: speed, inPit: inPit}
	}
	steps := []struct {
		s    sample
		at   time.Duration
		want game.LiveState
	}{
		{sample{}, 0, game.LiveUnknown},
		{live(0, true), 1 * time.Second, game.LiveNotDriving},                          // in pit box / pit menu
		{live(60, false), 2 * time.Second, game.LiveDriving},                           // left the pits
		{live(0, false), 5 * time.Second, game.LiveDriving},                            // spin: stopped briefly
		{live(0, false), 13 * time.Second, game.LiveNotDriving},                        // stopped > 10 s
		{live(80, false), 14 * time.Second, game.LiveDriving},                          // driving again
		{sample{ok: true, status: 3, speed: 0}, 15 * time.Second, game.LiveNotDriving}, // paused
		{live(80, false), 16 * time.Second, game.LiveDriving},
		{live(40, true), 17 * time.Second, game.LiveNotDriving}, // in the box
	}
	for i, st := range steps {
		if got := d.update(st.s, t0.Add(st.at)); got != st.want {
			t.Errorf("step %d: got %s, want %s", i, got, st.want)
		}
	}
}

func TestVersionAt(t *testing.T) {
	list := New().Patches()
	cases := map[string]string{"2021-06-01": "", "2022-05-01": "1.8.11", "2023-04-20": "1.9", "2023-05-04": "1.9.2", "2023-08-01": "1.9.5", "2024-06-01": "1.10", "2024-09-01": "1.10.3", "2026-01-01": "1.10.4"}
	for d, want := range cases {
		tm, _ := time.Parse("2006-01-02", d)
		if got := game.VersionAt(list, tm); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
}

// The values for pit stop advice (D-124) are read from their places and checked for plausibility.
func TestParsePitAdvice(t *testing.T) {
	g := make([]byte, graphicsReadSize)
	p := make([]byte, physicsReadSize)
	putI(g, offStatus, statusLive)
	for i, v := range []float32{27.1, 27.3, 26.4, 26.6} {
		putF(p, offPressures+4*i, v)
	}
	putI(g, offRainTyres, 1)
	putF(g, offTimeLeft, 1_234_000)
	putI(g, offSessionLaps, 0)
	putF(g, offLapProgress, 0.25)
	putI(g, offStopsLeft, 1)
	putF(g, offPitFuel, 42)
	for i, v := range []float32{26.5, 26.5, 25.9, 25.9} {
		putF(g, offPitPressures+4*i, v)
	}
	tl := parse(p, g, nil).t
	near := func(a, b float64) bool { return a-b < 0.01 && b-a < 0.01 }
	if !tl.PressuresKnown || !near(tl.Pressures[0], 27.1) || !near(tl.Pressures[3], 26.6) {
		t.Errorf("pressures: %+v", tl.Pressures)
	}
	if !tl.WetKnown || !tl.WetTyres || !tl.TimeLeftKnown || tl.TimeLeftMs != 1_234_000 || tl.SessionLaps != 0 {
		t.Errorf("wet/time: %+v", tl)
	}
	if !tl.ProgressKnown || tl.LapProgress != 0.25 || !tl.StopsKnown || tl.StopsLeft != 1 {
		t.Errorf("progress/stops: %+v", tl)
	}
	st := make([]byte, staticReadSize)
	putF(st, offMaxFuel, 120)
	putI(g, offStintLeft, 3_900_000)
	if x := parse(p, g, st).t; !x.TankKnown || x.TankSize != 120 || !x.StintKnown || x.StintLeftMs != 3_900_000 {
		t.Errorf("tank/stint: %+v", x)
	}
	putI(g, offStintLeft, -1) // no stint limit
	if parse(p, g, st).t.StintKnown {
		t.Error("stint limit of -1 taken as a limit")
	}
	if !tl.PitMenuKnown || tl.PitFuel != 42 || !near(tl.PitPressures[2], 25.9) {
		t.Errorf("pit menu: %+v %+v", tl.PitFuel, tl.PitPressures)
	}
	// Nonsense is not trusted.
	putF(p, offPressures, 300)
	putF(g, offPitPressures, 0)
	putI(g, offStopsLeft, -1)
	tl = parse(p, g, nil).t
	if tl.PressuresKnown || tl.PitMenuKnown || tl.StopsKnown {
		t.Errorf("implausible values trusted: %+v", tl)
	}
}

// D-152: when setups tie on every clue, the tyre pressures in the pits (still near the cold
// pressures) pick the closest one, but only when it is clearly closest.
func TestMatchSetupTyrePressureTieBreak(t *testing.T) {
	dir := "../../../SampleSetups/Assetto Corsa Competizione/ferrari_296_gt3/Imola/"
	s1616, err1 := os.ReadFile(dir + "BGK R 16C 1.10.3.json")  // cold 26.4 25.9 25.6 25.2
	s2027, err2 := os.ReadFile(dir + "BGK R 2027 1.10.3.json") // cold 26.1 25.6 25.8 25.5
	if err1 != nil || err2 != nil {
		t.Skip("SampleSetups not present")
	}
	g := New()
	tl := game.Telemetry{Fuel: 13.9, TC: 6, ABS: 2, Map: 0}
	tl.PressuresKnown, tl.Pressures = true, [4]float64{26.0, 25.5, 25.8, 25.5} // rig log 2026-11-21
	if got := g.MatchSetup(tl, [][]byte{s1616, s2027}); fmt.Sprint(got) != "[1]" {
		t.Errorf("pressures near 2027's: got %v, want [1]", got)
	}
	tl.Pressures = [4]float64{27.5, 27.5, 27.5, 27.5} // hot after a stint: no clue
	if got := g.MatchSetup(tl, [][]byte{s1616, s2027}); fmt.Sprint(got) != "[0 1]" {
		t.Errorf("hot tyres: got %v, want both", got)
	}
}
