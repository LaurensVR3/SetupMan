package library

import (
	"testing"
	"time"

	"setupman/internal/store"
)

// The laps of a recorded practice at Imola (rig, 2026-11-23): a stint from the pits, a break
// in the garage, out of the garage, then an in-lap and an out-lap with a 30 s stop.
func imolaSession() []store.Lap {
	t0 := time.Date(2026, 11, 23, 18, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	return []store.Lap{
		{At: at(1010.526), LapMs: 101235, Valid: false, Pit: true}, // out of the pits at the start
		{At: at(1110.543), LapMs: 101102, Valid: false},
		{At: at(1212.560), LapMs: 101065, Valid: true},
		{At: at(1312.576), LapMs: 100712, Valid: true},
		{At: at(1762.644), LapMs: 101527, Valid: true, Pit: true}, // out of the garage
		{At: at(1882.663), LapMs: 120170, Valid: true, Pit: true}, // in-lap
		{At: at(2036.692), LapMs: 153542, Valid: false, Pit: true}, // out-lap with the stop
		{At: at(2136.709), LapMs: 101182, Valid: true},
	}
}

func TestPitStopsImola(t *testing.T) {
	st := pitStops(imolaSession())
	if len(st) != 1 {
		t.Fatalf("want 1 stop, got %d: %+v", len(st), st)
	}
	// 120.170 + 153.542 - 2 × 101.065 (median of the valid normal laps)
	if st[0].lossMs != 71582 {
		t.Errorf("loss = %d ms, want 71582", st[0].lossMs)
	}
}

func TestPitStopsNone(t *testing.T) {
	laps := imolaSession()[:4] // start from the pits only
	if st := pitStops(laps); len(st) != 0 {
		t.Errorf("leaving the pits at the start is no stop: %+v", st)
	}
	laps = imolaSession()[:7] // session ends in the pit lane: no normal lap after the stop
	if st := pitStops(laps); len(st) != 0 {
		t.Errorf("a stop without a lap after it is not measured: %+v", st)
	}
}
