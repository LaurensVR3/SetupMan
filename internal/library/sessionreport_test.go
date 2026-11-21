package library

import (
	"testing"

	"setupman/internal/store"
)

// A race: out-lap from the grid, three laps (one invalid), in-lap, out-lap, two laps, a stop with
// the timing line in the pit lane, one last lap.
func TestSessionStints(t *testing.T) {
	lap := func(ms int, valid, pit bool, fuel float64, p float64) store.Lap {
		l := store.Lap{LapMs: ms, Valid: valid, Pit: pit, FuelUsed: fuel}
		if p > 0 {
			l.Pressures, l.HasPressures = [4]float64{p, p, p + 1, p + 1}, true
		}
		return l
	}
	laps := []store.Lap{
		lap(120000, true, true, 3.2, 26.0),   // 1 out-lap
		lap(105000, true, false, 3.0, 26.8),  // 2
		lap(104000, false, false, 3.0, 27.0), // 3 invalid
		lap(106000, true, false, 3.0, 27.2),  // 4
		lap(130000, true, true, 2.0, 27.0),   // 5 in-lap
		lap(125000, true, true, 0, 26.0),     // 6 out-lap (refuelled: fuel unknown)
		lap(104500, true, false, 3.0, 26.9),  // 7
		lap(104800, true, false, 3.0, 27.1),  // 8
		lap(150000, true, true, 0, 26.5),     // 9 stop in one lap
		lap(105500, true, false, 3.0, 27.0),  // 10
	}
	rows := lapRows(laps, map[int64]int{})
	kinds := ""
	for _, r := range rows {
		k := r.Kind
		if k == "" {
			k = "-"
		}
		kinds += k + " "
	}
	if want := "out - - - in out - - stop - "; kinds != want {
		t.Errorf("kinds %q, want %q", kinds, want)
	}
	if !rows[6].Best || rows[2].Best { // 104.5 (lap 7); the invalid 104.0 doesn't count
		t.Errorf("best lap marked wrong")
	}
	st := stints(rows)
	if len(st) != 3 {
		t.Fatalf("stints: %+v", st)
	}
	if st[0].FromLap != 1 || st[0].ToLap != 5 || st[1].FromLap != 6 || st[1].ToLap != 9 || st[2].FromLap != 10 || st[2].ToLap != 10 {
		t.Errorf("stint laps: %d-%d %d-%d %d-%d", st[0].FromLap, st[0].ToLap, st[1].FromLap, st[1].ToLap, st[2].FromLap, st[2].ToLap)
	}
	if st[0].Clean != 2 || st[0].BestMs != 105000 || st[0].AvgMs != 105500 {
		t.Errorf("stint 1: %+v", st[0].LapStats)
	}
	if !st[0].HasPressures || st[0].Pressures[0] != 27 { // laps 2-4: (26.8 + 27.0 + 27.2) / 3
		t.Errorf("stint 1 pressures: %v", st[0].Pressures)
	}
	sum := summarize(laps)
	if sum.Laps != 10 || sum.Clean != 5 || sum.BestMs != 104500 || sum.ConsistencyMs == 0 || sum.FuelUsed < 23.19 || sum.FuelUsed > 23.21 {
		t.Errorf("summary: %+v", sum)
	}
}
