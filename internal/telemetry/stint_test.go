package telemetry

import (
	"math"
	"testing"

	"setupman/internal/store"
)

// Brakes over a stint come from the extra log, lap by lap in order: two laps with the same time
// are not mixed up, and a lap's wear is its pad life at the start minus at the end.
func TestStint(t *testing.T) {
	names := []string{ExtraLapCount, ExtraLapTime, ExtraLastLap, "PIT_LANE"}
	for _, p := range []string{"PAD_LIFE", "DISC_LIFE", "BRAKE_TEMP"} {
		for _, w := range Wheels {
			names = append(names, p+"_"+w)
		}
	}
	x := &ExtraLog{Data: make([][]float32, len(names))}
	for _, n := range names {
		x.Header.Channels = append(x.Header.Channels, ExtraChannel{Name: n})
	}
	// 4 laps of 10 rows: lap times 100.000, 100.000 (the same), 99.000 and a pit lap of 130.000
	times := []float32{100000, 100000, 99000, 130000}
	pad := float32(29)
	for lap := 0; lap < 4; lap++ {
		for r := 0; r < 10; r++ {
			x.T = append(x.T, float32(len(x.T))/10)
			last := float32(0)
			if lap > 0 {
				last = times[lap-1]
			}
			row := []float32{float32(lap), float32(r) * 10000, last, 0}
			if lap == 3 && r > 6 {
				row[3] = 1 // in the pit lane
			}
			pad -= 0.01 // 0.01 per row on every wheel
			for range Wheels {
				row = append(row, pad)
			}
			for range Wheels {
				row = append(row, pad+3)
			}
			for w := range Wheels {
				row = append(row, float32(400+100*w))
			}
			for c, v := range row {
				x.Data[c] = append(x.Data[c], v)
			}
		}
	}
	// the lap counter goes up once more: the end of lap 4
	x.T = append(x.T, 4)
	end := []float32{4, 0, times[3], 0}
	for c := range x.Data {
		v := x.Data[c][len(x.Data[c])-1]
		if c < len(end) {
			v = end[c]
		}
		x.Data[c] = append(x.Data[c], v)
	}
	laps := []store.TeleLap{{N: 0, Lap: 0, LapMs: 60000}}
	for i, ms := range times {
		laps = append(laps, store.TeleLap{N: i + 1, Lap: i + 1, LapMs: int(ms), Timed: true, DistM: 4000})
	}
	st := Stint(laps, x)
	if len(st) != 4 {
		t.Fatalf("%d laps: %+v", len(st), st)
	}
	for i, s := range st {
		if s.Lap != i+1 || s.Pad == nil || s.Temp == nil {
			t.Fatalf("lap %d: %+v", i+1, s)
		}
		if math.Abs(float64(s.PadUsed[0])-0.09) > 1e-4 { // 10 rows, the first is the start
			t.Errorf("lap %d used %.4f", s.Lap, s.PadUsed[0])
		}
		if s.Temp[3] != 700 {
			t.Errorf("lap %d RR temperature %v", s.Lap, s.Temp[3])
		}
	}
	if st[1].Pad[0] >= st[0].Pad[0] {
		t.Error("the second 100.000 lap must be the later one (less pad left)")
	}
	if !st[3].Pit || st[2].Pit {
		t.Error("only the last lap went through the pit lane")
	}
	// without the extra log: lap times only
	if st := Stint(laps, nil); len(st) != 4 || st[0].Pad != nil {
		t.Fatalf("no extra log: %+v", st)
	}
}
