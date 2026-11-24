package library

import (
	"testing"
	"time"

	"setupman/internal/store"
	"setupman/internal/telemetry"
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
		{At: at(1762.644), LapMs: 101527, Valid: true, Pit: true},  // out of the garage
		{At: at(1882.663), LapMs: 120170, Valid: true, Pit: true},  // in-lap
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

// Standing still in the pit lane (D-187): rows that barely move, 2 s or longer; a slow crawl or a
// short stop doesn't count.
func TestStillSpans(t *testing.T) {
	start := time.Date(2026, 11, 23, 21, 0, 0, 0, time.UTC)
	x := &telemetry.ExtraLog{Header: telemetry.ExtraHeader{Start: start, Channels: []telemetry.ExtraChannel{{Name: "PIT_LANE"}, {Name: "POS_X"}, {Name: "POS_Z"}}}}
	x.Data = make([][]float32, 3)
	pos := float32(0)
	for i := 0; i < 600; i++ { // 60 s at 10 Hz
		tm := float32(i) / 10
		pit := float32(0)
		if tm >= 10 && tm < 50 {
			pit = 1
		}
		switch {
		case tm >= 20 && tm < 52.0-20+0: // stands still 20 s … 32 s
			if tm >= 32 {
				pos += 2
			}
		case tm >= 40 && tm < 41: // a 1 s stop: too short
		default:
			pos += 2
		}
		x.T = append(x.T, tm)
		x.Data[0] = append(x.Data[0], pit)
		x.Data[1] = append(x.Data[1], pos)
		x.Data[2] = append(x.Data[2], 0)
	}
	sp := stillSpans(x)
	if len(sp) != 1 {
		t.Fatalf("spans: %v", sp)
	}
	if d := sp[0][1].Sub(sp[0][0]); d < 11*time.Second || d > 12*time.Second {
		t.Errorf("standing %v, want about 12 s", d)
	}
}
