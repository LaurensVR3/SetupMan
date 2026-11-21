// Package acc supports Assetto Corsa Competizione: reading setup files,
// readable car/track names, finding the installation and reading live data.
package acc

import (
	"bytes"
	"fmt"
	"math"
	"time"

	"setupman/internal/game"
	"setupman/internal/games/gamekit"
	"setupman/internal/setup"
)

// SteamAppID is ACC's Steam application id.
const SteamAppID = "805550"

// Game is the ACC implementation of game.Game.
type Game struct {
	live *liveReader
}

// New creates the ACC game.
func New() *Game { return &Game{live: newLiveReader()} }

func (g *Game) ID() string   { return "acc" }
func (g *Game) Name() string { return "Assetto Corsa Competizione" }

// Scan reads an ACC setups folder (read-only).
func (g *Game) Scan(folder string) (*game.Scan, error) { return ScanFolder(folder) }

// ParseSetup turns stored file content back into comparable fields.
func (g *Game) ParseSetup(content []byte) ([]setup.Field, error) {
	s, err := Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	return s.Fields, nil
}

// Detect looks for ACC via Steam and for its setups folder in Documents.
// IsWet reports whether a setup has rain tyres fitted (tyre compound 1 = Wet, D-120).
func (g *Game) IsWet(fields []setup.Field) bool {
	for _, f := range fields {
		if f.Path == "basicSetup.tyres.tyreCompound" && f.Num == 1 {
			return true
		}
	}
	return false
}

func (g *Game) Detect() game.Detection {
	return detect(steamLibraries(), documentsDir())
}

// Live reports whether the player is driving (spec D-38).
func (g *Game) Live() game.LiveState { return g.live.State() }

// Telemetry returns the latest live-data snapshot (false when ACC isn't running).
func (g *Game) Telemetry() (game.Telemetry, bool) { return g.live.Telemetry() }

// CachesSetups: ACC reads every setup of the car and track when it lists them, and none when one is
// loaded (rig log 2026-11-21, D-149).
func (g *Game) CachesSetups() bool { return true }

// StartedAt returns when ACC's program started (D-147).
func (g *Game) StartedAt() (time.Time, bool) { return gamekit.StartedAt(g.live.exes...) }

// MatchSetup returns the candidate setups that fit best what the car reports when it leaves the
// pits: one index = sure, several = they fit equally well (the user picks, D-149), none = nothing
// fits. Clues: start fuel (the file stores 2 L less than the car has, D-144), the pit menu's tyre
// pressures and fuel to add, which ACC takes from the loaded setup's first pit-stop strategy
// (rig log 2026-11-21, D-149), and the TC / ABS / engine map positions. The car reports the engine
// map from 0, as the file stores it (rig test 2026-11-20: file 0, car 0, game screen 1).
func (g *Game) MatchSetup(t game.Telemetry, candidates [][]byte) []int {
	if len(candidates) == 1 {
		return []int{0} // only one setup for this car and track: it must be that one
	}
	scores := make([]int, len(candidates))
	cold := make([][4]float64, len(candidates))
	hasCold := make([]bool, len(candidates))
	best := 0
	for i, c := range candidates {
		scores[i], cold[i], hasCold[i] = matchScore(t, c)
		if scores[i] > best {
			best = scores[i]
		}
	}
	if best < 2 {
		return nil
	}
	var out []int
	for i, sc := range scores {
		if sc == best {
			out = append(out, i)
		}
	}
	if len(out) > 1 && t.PressuresKnown {
		if i, ok := closestPressures(t.Pressures, out, cold, hasCold); ok {
			return []int{i}
		}
	}
	return out
}

// closestPressures breaks a tie with the tyre pressures (D-152): in the pits and just out of
// them the tyres are still near the setup's cold pressures (rig log 2026-11-21: car 26.0 25.5 25.8
// 25.5, the loaded setup 26.1 25.6 25.8 25.5 psi). The closest setup wins only when it is within
// 0.2 psi per tyre on average and clearly closer (0.1 psi per tyre) than the next one.
func closestPressures(now [4]float64, among []int, cold [][4]float64, hasCold []bool) (int, bool) {
	best, bestD, nextD := -1, math.Inf(1), math.Inf(1)
	for _, i := range among {
		if !hasCold[i] {
			return 0, false
		}
		d := 0.0
		for w := 0; w < 4; w++ {
			d += math.Abs(now[w] - cold[i][w])
		}
		d /= 4
		switch {
		case d < bestD:
			best, bestD, nextD = i, d, bestD
		case d < nextD:
			nextD = d
		}
	}
	if best >= 0 && bestD <= 0.2 && nextD-bestD >= 0.1 {
		return best, true
	}
	return 0, false
}

// pitPressurePath is the tyre pressure of the first pit-stop strategy, per wheel.
const pitPressurePath = "basicSetup.strategy.pitStrategy#1.tyres.tyrePressure[%d]"

// matchScore scores one setup and returns its cold tyre pressures (psi) for a tie-break.
func matchScore(t game.Telemetry, content []byte) (score int, cold [4]float64, hasCold bool) {
	s, err := Parse(bytes.NewReader(content))
	if err != nil {
		return 0, cold, false
	}
	num, real := map[string]float64{}, map[string]float64{}
	for _, f := range s.Fields {
		num[f.Path], real[f.Path] = f.Num, f.RealValue()
	}
	hasCold = true
	for w := 0; w < 4; w++ {
		p, ok := real[fmt.Sprintf("basicSetup.tyres.tyrePressure[%d]", w)]
		cold[w], hasCold = p, hasCold && ok
	}
	if fuel, ok := real["basicSetup.strategy.fuel"]; ok && t.Fuel > 0 && math.Abs(fuel-t.Fuel) <= 1.5 {
		score += 3
	}
	if t.PitMenuKnown {
		same := true
		for w := 0; w < 4; w++ {
			p, ok := real[fmt.Sprintf(pitPressurePath, w)]
			if !ok || math.Abs(p-t.PitPressures[w]) > 0.05 {
				same = false
			}
		}
		if same {
			score += 3 // four pressures to 0.1 psi: a strong fingerprint
		}
		if add, ok := real["basicSetup.strategy.pitStrategy#1.fuelToAdd"]; ok && add > 0 && math.Abs(add-t.PitFuel) <= 0.5 {
			score++
		}
	}
	if tc, ok := num["basicSetup.electronics.tC1"]; ok && t.TC >= 0 && int(tc) == t.TC {
		score++
	}
	if abs, ok := num["basicSetup.electronics.abs"]; ok && t.ABS >= 0 && int(abs) == t.ABS {
		score++
	}
	if m, ok := num["basicSetup.electronics.eCUMap"]; ok && t.Map >= 0 && (int(m) == t.Map || int(m)+1 == t.Map) {
		score++
	}
	return score, cold, hasCold
}
