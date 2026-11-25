package telemetry

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cutRoundTrip cuts each lap out of x, writes and reads it back, and checks the lap is found again
// with the same time and the same rows.
func cutRoundTrip(t *testing.T, name string, x *ExtraLog) int {
	t.Helper()
	n := 0
	for _, l := range x.Laps() {
		c := x.Cut(l.From, l.To)
		var buf bytes.Buffer
		if err := WriteExtra(&buf, c); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "cut.smx.gz")
		os.WriteFile(p, buf.Bytes(), 0o644)
		y, err := ReadExtra(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, ok := y.FindLap(l.LapMs)
		if !ok || got.To-got.From != l.To-l.From {
			t.Errorf("%s: lap %d ms not found again in the cut log (%+v, %v)", name, l.LapMs, got, ok)
			continue
		}
		for i := range x.Data {
			if y.Data[i][got.From] != x.Data[i][l.From] || y.Data[i][got.To-1] != x.Data[i][l.To-1] {
				t.Errorf("%s: channel %s differs", name, x.Header.Channels[i].Name)
				break
			}
		}
		want := x.Header.Start.Add(time.Duration(float64(x.T[l.From]) * float64(time.Second)))
		have := y.Header.Start.Add(time.Duration(float64(y.T[got.From]) * float64(time.Second)))
		if d := want.Sub(have); d > time.Millisecond || d < -time.Millisecond {
			t.Errorf("%s: lap starts %v later in the cut log", name, -d)
		}
		if y.Header.Session != 0 {
			t.Errorf("%s: the session number of this PC stays behind", name)
		}
		n++
	}
	return n
}

func TestExtraLogCutSmall(t *testing.T) {
	h := ExtraHeader{Game: "acc", Session: 7, Start: time.Date(2026, 11, 24, 20, 0, 0, 0, time.UTC), Hz: 10,
		Channels: []ExtraChannel{{Name: ExtraLapCount}, {Name: ExtraLapTime}, {Name: ExtraLastLap}, {Name: "FUEL"}}}
	x := &ExtraLog{Header: h}
	x.Data = make([][]float32, 4)
	// an out-lap of 5 s, then laps of 10, 11 and 12 s: the lap clock restarts at the line and
	// the lap counter goes up one reading later, with the last lap's time
	add := func(lap, clock, last float32) {
		ts := float32(len(x.T)) / 10
		x.T = append(x.T, ts)
		for c, v := range []float32{lap, clock, last, 60 - ts/10} {
			x.Data[c] = append(x.Data[c], v)
		}
	}
	lap, last := float32(0), float32(0)
	for i, secs := range []int{5, 10, 11, 12, 3} {
		for k := 0; k < secs*10; k++ {
			if k == 1 && i > 0 { // one reading after the line: the counter and last-lap time follow
				lap++
				last = float32([]int{5, 10, 11, 12, 3}[i-1] * 1000)
			}
			add(lap, float32(k)*100, last)
		}
	}
	if n := cutRoundTrip(t, "small", x); n < 3 {
		t.Fatalf("%d laps cut", n)
	}
}

// TestExtraLogCutReal cuts every lap of the rig's extra logs when present (git-ignored copy).
func TestExtraLogCutReal(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "Telemetry", "Assetto Corsa Competizione", "SetupMan", "telemetry", "*.smx.gz"))
	if len(files) == 0 {
		t.Skip("no extra logs")
	}
	n := 0
	for _, p := range files {
		x, err := ReadExtra(p)
		if err != nil {
			t.Fatal(err)
		}
		n += cutRoundTrip(t, filepath.Base(p), x)
	}
	t.Logf("%d laps cut and read back", n)
}
