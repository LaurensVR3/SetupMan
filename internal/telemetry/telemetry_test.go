package telemetry

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"setupman/internal/motec"
	"setupman/internal/store"
)

func TestParseName(t *testing.T) {
	track, car, start, ok := ParseName("Barcelona-ferrari_296_gt3-10-2026.08.24-18.49.10.ld")
	if !ok || track != "barcelona" || car != "ferrari_296_gt3" {
		t.Fatalf("got %q %q %v", track, car, ok)
	}
	if want := time.Date(2026, 8, 24, 18, 49, 10, 0, time.Local); !start.Equal(want) {
		t.Fatalf("start %v", start)
	}
	for _, bad := range []string{"notes.ld", "spa-car-2026.08.24-18.49.10.ld", "a-b-1-2026.08.24-18.49.10.ldx"} {
		if _, _, _, ok := ParseName(bad); ok {
			t.Errorf("%s should not parse", bad)
		}
	}
}

func TestMatch(t *testing.T) {
	start := time.Date(2026, 11, 17, 22, 6, 38, 0, time.Local)
	f := store.TeleFile{StartedAt: start}
	laps := []store.TeleLap{
		{N: 0, StartMs: 0, LapMs: 142780},
		{N: 1, StartMs: 142780, LapMs: 91002, Timed: true}, // a pit lap or one without data: timed, not full
		{N: 2, StartMs: 233782, LapMs: 91087, Complete: true, Timed: true},
	}
	at := func(ms int64) time.Time { return start.Add(time.Duration(ms)*time.Millisecond + 800*time.Millisecond) }
	sessions := []store.Session{{ID: 1, StartedAt: start.Add(-time.Hour)}, {ID: 2, StartedAt: start.Add(-20 * time.Second)}}
	recorded := map[int64][]store.Lap{
		1: {{ID: 10, LapMs: 91002, At: start.Add(-50 * time.Minute)}}, // same time, but far too early
		2: {{ID: 20, LapMs: 91003, At: at(233782)}, {ID: 21, LapMs: 91087, At: at(324869)}},
	}
	sid, links := Match(f, laps, sessions, recorded)
	if sid != 2 || links[1] != 20 || links[2] != 21 || len(links) != 2 {
		t.Fatalf("got session %d links %v", sid, links)
	}
	// a file without complete laps links to a session that started within two minutes
	sid, _ = Match(f, laps[:1], sessions, recorded)
	if sid != 2 {
		t.Fatalf("no laps: got %d", sid)
	}
	// complete laps but none recorded: not linked
	if sid, _ := Match(f, laps, sessions, map[int64][]store.Lap{}); sid != 0 {
		t.Fatalf("expected no link, got %d", sid)
	}
}

// One ACC file for a server weekend (D-179): practice and race laps each link to their own
// recorded lap; the file goes to the session with the most laps.
func TestMatchWeekendFile(t *testing.T) {
	start := time.Date(2026, 11, 23, 20, 15, 0, 0, time.Local)
	f := store.TeleFile{StartedAt: start}
	laps := []store.TeleLap{
		{N: 1, StartMs: 0, LapMs: 101000, Timed: true},       // practice
		{N: 2, StartMs: 1800000, LapMs: 102000, Timed: true}, // race
		{N: 3, StartMs: 1902000, LapMs: 102500, Timed: true}, // race
	}
	at := func(ms int64) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
	sessions := []store.Session{{ID: 1, StartedAt: start}, {ID: 2, StartedAt: start.Add(29 * time.Minute)}}
	recorded := map[int64][]store.Lap{
		1: {{ID: 10, LapMs: 101000, At: at(101000)}},
		2: {{ID: 20, LapMs: 102000, At: at(1902000)}, {ID: 21, LapMs: 102500, At: at(2004500)}},
	}
	sid, links := Match(f, laps, sessions, recorded)
	if sid != 2 || links[1] != 10 || links[2] != 20 || links[3] != 21 {
		t.Fatalf("got session %d links %v", sid, links)
	}
}

func TestExtraLogRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "acc-7.smx.gz")
	h := ExtraHeader{Game: "acc", Session: 7, Start: time.Now(), Hz: 10, Channels: []ExtraChannel{
		{Name: ExtraLapCount}, {Name: ExtraLapTime, Unit: "ms"}, {Name: ExtraLastLap, Unit: "ms"}, {Name: "FUEL", Unit: "l"}}}
	w, err := CreateExtra(p, h)
	if err != nil {
		t.Fatal(err)
	}
	// lap 0 → 1 at row 5 with a lap time of 90123 ms
	for i := 0; i < 10; i++ {
		lap, last := float32(0), float32(0)
		if i >= 5 {
			lap, last = 1, 90123
		}
		if err := w.Write(float64(i)/10, []float32{lap, float32(i%5) * 100, last, 50 - float32(i)*0.1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	x, err := ReadExtra(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.T) != 10 || x.Header.Session != 7 || x.Data[x.Index("FUEL")][3] != 49.7 {
		t.Fatalf("read back: %d rows, %+v", len(x.T), x.Header)
	}
	l, ok := x.FindLap(90124)
	if !ok || l.From != 0 || l.To != 5 {
		t.Fatalf("lap: %+v %v", l, ok)
	}
	if ci := describe(extraPrefix+"SLIP_ANGLE_FL", "", 10, "extra"); ci.Label != "Slip angle FL" || ci.Wheel != "FL" || ci.Group != "Tyres" {
		t.Fatalf("describe: %+v", ci)
	}
	if ci := describe("TYRE_PRESS_LR", "", 20, "motec"); ci.Label != "Tyre pressure RL" || ci.Base != "TYRE_PRESS" {
		t.Fatalf("describe: %+v", ci)
	}
}

func TestExtraLogCutShort(t *testing.T) {
	p := filepath.Join(t.TempDir(), "acc-8.smx.gz")
	w, err := CreateExtra(p, ExtraHeader{Hz: 10, Channels: []ExtraChannel{{Name: "FUEL"}}})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(0, []float32{1})
	w.flush()
	// SetupMan stops here without closing the file
	x, err := ReadExtra(p)
	if err != nil || len(x.T) != 1 {
		t.Fatalf("cut-short file: %v rows, %v", len(x.T), err)
	}
	w.Close()
}

func TestFloatsJSON(t *testing.T) {
	b, _ := json.Marshal(Floats{1, 2.5, 123.456789, float32(math.NaN())})
	if string(b) != "[1,2.5,123.46,0]" {
		t.Fatalf("got %s", b)
	}
}

// TestRealFiles indexes the user's MoTeC files when present (git-ignored) and checks laps, the
// track map and lap data on a long session.
func TestRealFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "Telemetry", "Assetto Corsa Competizione", "MoTeC")
	name := "donington-ferrari_296_gt3-59-2026.11.17-22.06.38.ld"
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Skip("no MoTeC files")
	}
	f, laps, err := IndexFile(dir, name, "acc")
	if err != nil {
		t.Fatal(err)
	}
	if f.Track != "donington" || f.Channels != 55 || len(laps) < 40 {
		t.Fatalf("file %+v, %d laps", f, len(laps))
	}
	if laps[2].LapMs != 91003 && laps[2].LapMs != 91002 { // 90.988 + 0.020 − 0.005
		t.Fatalf("lap 2: %+v", laps[2])
	}
	lf, err := motec.Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	m, err := MapFromMotion(lf, laps[2])
	if err != nil || m == nil {
		t.Fatal(err)
	}
	if !m.Clockwise() {
		t.Error("Donington runs clockwise")
	}
	if d := m.Dist[len(m.Dist)-1]; d < 3800 || d > 4100 {
		t.Errorf("lap length %.0f m", d)
	}
	d, err := ReadLap(lf, laps[2], []string{"SPEED", "GEAR", "SUS_TRAVEL_LF"}, nil, nil)
	if err != nil || len(d.Channels) != 3 {
		t.Fatalf("lap data: %v", err)
	}
	if sp := d.Channels[0]; sp.Unit != "km/h" || sp.Data[100] < 50 || sp.Data[100] > 300 {
		t.Errorf("speed: %+v", sp.Data[:5])
	}
	if d.Dist[0] < -1 || d.Dist[0] > 5 {
		t.Errorf("distance should start at the line: %v", d.Dist[0])
	}
	chans := Channels(lf, nil)
	if chans[0].Group != "Driver" {
		t.Errorf("first group: %+v", chans[0])
	}
}

// In this file ACC kept speed, pedals and the rest only for laps 7 … 11 (TIME runs 1323 s, SPEED
// 504 s), D-167: earlier laps have no data, later ones start at the line.
func TestRealFileLastLapsOnly(t *testing.T) {
	dir := filepath.Join("..", "..", "Telemetry", "Assetto Corsa Competizione", "MoTeC")
	name := "donington-ferrari_296_gt3-13-2026.04.28-19.09.24.ld"
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Skip("no MoTeC files")
	}
	_, laps, err := IndexFile(dir, name, "acc")
	if err != nil || len(laps) != 13 {
		t.Fatalf("%d laps, %v", len(laps), err)
	}
	if laps[4].DistM > 100 {
		t.Errorf("lap 4 should have no data: %.0f m", laps[4].DistM)
	}
	if d := laps[7].DistM; d < 3900 || d > 4000 {
		t.Errorf("lap 7: %.0f m", d)
	}
	lf, err := motec.Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	d, err := ReadLap(lf, laps[7], []string{"SPEED"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v := d.Channels[0].Data[0]; v < 150 || v > 200 { // the main straight, before Redgate
		t.Errorf("speed at the line %.0f km/h", v)
	}
	m, err := MapFromMotion(lf, laps[7])
	if err != nil || m == nil {
		t.Fatal(err)
	}
	if m.Dist[0] < -2 || m.Dist[0] > 5 {
		t.Errorf("map distance should start at the line: %v", m.Dist[0])
	}
}

// Turns come from several laps: a slow point on most laps counts, one on a single lap does not,
// and two slow points 40 m apart (a chicane) are one turn.
func TestFindTurns(t *testing.T) {
	lap := func(extra bool) LapSpeed {
		var l LapSpeed
		for x := 0; x <= 4000; x += 5 {
			v := 200.0
			for _, c := range []float64{600, 1500, 1540, 3000} { // a hairpin, a chicane (two minima), a corner
				v = math.Min(v, 80+math.Abs(float64(x)-c)*0.6)
			}
			if extra {
				v = math.Min(v, 150+math.Abs(float64(x)-2300)*0.6) // a lift on one lap only
			}
			l.Speed = append(l.Speed, float32(v))
			l.Dist = append(l.Dist, float32(x))
		}
		return l
	}
	turns := FindTurns([]LapSpeed{lap(false), lap(true), lap(false), lap(false)})
	if len(turns) != 3 {
		t.Fatalf("turns %v", turns)
	}
	for i, want := range []float64{600, 1520, 3000} {
		if math.Abs(turns[i]*4000-want) > 30 {
			t.Errorf("T%d at %.0f m, want about %.0f", i+1, turns[i]*4000, want)
		}
	}
	if FindTurns([]LapSpeed{lap(false), lap(false)}) != nil {
		t.Error("two laps are too few")
	}
}
