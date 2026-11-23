package telemetry

import (
	"setupman/internal/store"
)

// Brakes over a stint (D-170, owner request 2026-11-23: "monitor brake wear over time for longer
// stints"). ACC's MoTeC files have brake temperatures but keep data for the last laps only and have
// no wear; SetupMan's extra log (D-162) has pad and disc life (and, from D-170, brake temperatures)
// for the whole session. A lap's wear is its life at the start minus at the end.

// Wheels is the order of the per-wheel values in a StintLap.
var Wheels = [4]string{"FL", "FR", "RL", "RR"}

// StintLap is one lap of a session with what its brakes did. Missing values are nil.
type StintLap struct {
	Lap      int       `json:"lap"`
	LapMs    int       `json:"lapMs"`
	Pit      bool      `json:"pit,omitempty"`
	Pad      []float32 `json:"pad,omitempty"`      // pad life at the end of the lap (as the game reports it)
	Disc     []float32 `json:"disc,omitempty"`     // disc life at the end of the lap
	PadUsed  []float32 `json:"padUsed,omitempty"`  // pad life used during the lap
	DiscUsed []float32 `json:"discUsed,omitempty"` // disc life used during the lap
	Temp     []float32 `json:"temp,omitempty"`     // brake temperature, lap average, °C
	TempMax  []float32 `json:"tempMax,omitempty"`  // brake temperature, highest in the lap, °C
	FileID   int64     `json:"fileId"`             // where the lap is (to open it in the analysis)
	N        int       `json:"n"`
	Data     bool      `json:"data"` // the MoTeC file has the lap's data
}

// Stint lists the timed laps of a session (laps: TelemetrySessionLaps, in order) with the brake
// values the extra log has for them (nil extra: lap times only). Extra-log laps are matched to the
// session's laps in order by their lap times (within 3 ms), so a time driven twice can't be taken
// for the wrong lap.
func Stint(laps []store.TeleLap, extra *ExtraLog) []StintLap {
	var xl []ExtraLap
	if extra != nil {
		xl = extra.Laps()
	}
	idx := func(prefix string) []int {
		var out []int
		if extra == nil {
			return nil
		}
		for _, w := range Wheels {
			i := extra.Index(prefix + "_" + w)
			if i < 0 {
				return nil
			}
			out = append(out, i)
		}
		return out
	}
	padI, discI, tempI := idx("PAD_LIFE"), idx("DISC_LIFE"), idx("BRAKE_TEMP")
	pitI := -1
	if extra != nil {
		pitI = extra.Index("PIT_LANE")
	}
	out := []StintLap{}
	next := 0
	for _, l := range laps {
		if !l.Timed || l.LapMs <= 0 {
			continue // the out-lap and the unfinished last lap
		}
		s := StintLap{Lap: l.Lap, LapMs: l.LapMs, Pit: l.Pit, FileID: l.FileID, N: l.N, Data: l.DistM >= lapDataM}
		for k := next; k < len(xl); k++ {
			if d := xl[k].LapMs - l.LapMs; d < -3 || d > 3 {
				continue
			}
			next = k + 1
			from, to := xl[k].From, xl[k].To
			if to <= from {
				break
			}
			if pitI >= 0 {
				for r := from; r < to; r++ {
					if extra.Data[pitI][r] > 0 {
						s.Pit = true
						break
					}
				}
			}
			s.Pad, s.PadUsed = wear(extra, padI, from, to)
			s.Disc, s.DiscUsed = wear(extra, discI, from, to)
			if tempI != nil {
				s.Temp, s.TempMax = make([]float32, 4), make([]float32, 4)
				for w, ci := range tempI {
					sum, hi := 0.0, float32(0)
					for r := from; r < to; r++ {
						v := extra.Data[ci][r]
						sum += float64(v)
						hi = max(hi, v)
					}
					s.Temp[w], s.TempMax[w] = float32(sum/float64(to-from)), hi
				}
				if s.TempMax[0] == 0 && s.TempMax[1] == 0 && s.TempMax[2] == 0 && s.TempMax[3] == 0 {
					s.Temp, s.TempMax = nil, nil // the game reported nothing
				}
			}
			break
		}
		out = append(out, s)
	}
	return out
}

// lapDataM: a lap shorter than this (speed integrated) has no MoTeC data (as the store's).
const lapDataM = 100

// wear returns the life at the end of rows [from, to) and how much was used in them, per wheel
// (nil when the log has no such values: life 0 means the game reported nothing).
func wear(x *ExtraLog, idx []int, from, to int) (end, used []float32) {
	if idx == nil {
		return nil, nil
	}
	end, used = make([]float32, 4), make([]float32, 4)
	for w, ci := range idx {
		a, b := x.Data[ci][from], x.Data[ci][to-1]
		if a <= 0 || b <= 0 {
			return nil, nil
		}
		end[w], used[w] = b, max(0, a-b)
	}
	return end, used
}
