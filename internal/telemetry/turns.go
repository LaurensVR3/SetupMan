package telemetry

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Turn is one turn of a track: its number as shown ("5", "8a"), where it is along the lap (share
// of the lap from the line) and its name when known.
type Turn struct {
	N    string  `json:"n"`
	At   float64 `json:"at"`
	Name string  `json:"name,omitempty"`
}

// OfficialTurns returns a track's official turn numbers (D-171: from Wikimedia Commons maps fitted
// onto SetupMan's line), or nil when SetupMan has none for it.
func OfficialTurns(track string) []Turn {
	return officialTurns[strings.ToLower(track)]
}

// CountedTurns numbers SetupMan's own slow points (shares of the lap) T1, T2 … (tracks without
// official numbers).
func CountedTurns(at []float64) []Turn {
	if at == nil {
		return nil
	}
	out := make([]Turn, len(at))
	for i, a := range at {
		out[i] = Turn{N: strconv.Itoa(i + 1), At: a}
	}
	return out
}

// Turns (T1, T2 …) of a track, worked out once from several fast laps so the numbers stay the
// same whichever laps are compared (D-167). A turn is a slow point: the speed drops at least
// TurnDrop km/h before it and rises at least as much after it. Points found on most of the laps
// count; points closer together than TurnMerge metres (a chicane) are one turn. Fast kinks
// (Eau Rouge, Craner Curves) are not turns here, so the numbers can differ from a circuit's
// official ones.
const (
	TurnDrop  = 12  // km/h
	TurnMerge = 100 // metres
)

// LapSpeed is one lap's speed (km/h) along its distance (metres from the line).
type LapSpeed struct {
	Speed []float32
	Dist  []float32
}

// slowPoints returns where a lap's speed has its minima, in metres.
func slowPoints(l LapSpeed) []float64 {
	var out []float64
	if len(l.Speed) < 10 || len(l.Dist) != len(l.Speed) {
		return out
	}
	falling := false
	top, low, lowAt := float64(l.Speed[0]), 0.0, 0.0
	for i, s := range l.Speed {
		v, x := float64(s), float64(l.Dist[i])
		if !falling {
			top = math.Max(top, v)
			if v < top-TurnDrop {
				falling, low, lowAt = true, v, x
			}
			continue
		}
		if v < low {
			low, lowAt = v, x
		}
		if v > low+TurnDrop {
			out = append(out, lowAt)
			falling, top = false, v
		}
	}
	return out
}

// FindTurns returns where the turns of a track are, as a share of the lap (0 … 1), from a few
// fast laps of it (nil with fewer than 3 laps).
func FindTurns(laps []LapSpeed) []float64 {
	type point struct {
		at  float64 // share of the lap
		lap int
	}
	var pts []point
	var lengths []float64
	for i, l := range laps {
		if len(l.Dist) == 0 {
			continue
		}
		total := float64(l.Dist[len(l.Dist)-1])
		if total < 500 {
			continue
		}
		lengths = append(lengths, total)
		for _, x := range slowPoints(l) {
			pts = append(pts, point{x / total, i})
		}
	}
	if len(lengths) < 3 {
		return nil
	}
	sort.Float64s(lengths)
	merge := TurnMerge / lengths[len(lengths)/2]
	need := int(math.Ceil(0.6 * float64(len(lengths))))
	sort.Slice(pts, func(i, j int) bool { return pts[i].at < pts[j].at })
	var out []float64
	for i := 0; i < len(pts); {
		// points within TurnMerge of the group's first point: a stray slow point between two corners
		// can't chain them into one
		j := i + 1
		for j < len(pts) && pts[j].at-pts[i].at <= merge {
			j++
		}
		group := pts[i:j]
		seen := map[int]bool{}
		var at []float64
		for _, p := range group {
			seen[p.lap] = true
			at = append(at, p.at)
		}
		if len(seen) >= need {
			out = append(out, at[len(at)/2])
		}
		i = j
	}
	return out
}
