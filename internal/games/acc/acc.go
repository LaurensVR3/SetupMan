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
	return g.MatchSetupTemps(t, candidates, nil)
}

// MatchSetupTemps is MatchSetup knowing the air temperature each setup was made for (airs[i], 0 =
// unknown, from its name). Rule of thumb (owner 2026-11-24): +1 °C air = −0.1 psi, so pressures
// set for today's air temperature, by up to 0.5 psi (5 °C), still fit the setup (D-181).
func (g *Game) MatchSetupTemps(t game.Telemetry, candidates [][]byte, airs []float64) []int {
	if len(candidates) == 1 {
		return []int{0} // only one setup for this car and track: it must be that one
	}
	scores := make([]int, len(candidates))
	cold := make([][4]float64, len(candidates))
	hasCold := make([]bool, len(candidates))
	rules := make([]float64, len(candidates))
	best := 0
	for i, c := range candidates {
		if i < len(airs) {
			rules[i] = tempShift(t, airs[i])
		}
		scores[i], cold[i], hasCold[i] = matchScore(t, c, rules[i])
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
		if i, ok := closestPressures(t.Pressures, out, cold, hasCold, rules); ok {
			return []int{i}
		}
	}
	return out
}

// closestPressures breaks a tie with the tyre pressures (D-152): in the pits and just out of
// them the tyres are still near the setup's cold pressures (rig log 2026-11-21: car 26.0 25.5 25.8
// 25.5, the loaded setup 26.1 25.6 25.8 25.5 psi). The closest setup wins only when it is within
// 0.2 psi per tyre on average and clearly closer (0.1 psi per tyre) than the next one.
func closestPressures(now [4]float64, among []int, cold [][4]float64, hasCold []bool, rules []float64) (int, bool) {
	best, bestD, nextD := -1, math.Inf(1), math.Inf(1)
	for _, i := range among {
		if !hasCold[i] {
			return 0, false
		}
		// the setup's pressures shifted for today's air temperature, as far as the rule allows (D-181)
		lo, hi := shiftRange(rules[i])
		s := math.Max(lo, math.Min(hi, meanDiff(now, cold[i])))
		d := 0.0
		for w := 0; w < 4; w++ {
			d += math.Abs(now[w] - cold[i][w] - s)
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
// tempShift is the pressure change the rule of thumb gives for today's air temperature against
// the setup's (D-181): −0.1 psi per °C warmer, at most ±0.5 psi; 0 when either is unknown.
func tempShift(t game.Telemetry, setupAir float64) float64 {
	if setupAir <= 0 || !t.TempsKnown {
		return 0
	}
	return math.Max(-0.5, math.Min(0.5, -0.1*(t.AirTemp-setupAir)))
}

// shiftRange is the uniform pressure shift (psi, the same on all four tyres) that still fits a
// setup: from none up to the rule's shift, one click (0.1 psi) either side; exact (±0.05) when the
// rule gives nothing (temperature unknown or the same).
func shiftRange(rule float64) (lo, hi float64) {
	if math.Abs(rule) < 0.05 {
		return -0.05, 0.05
	}
	return math.Max(-0.55, math.Min(0, rule)-0.1), math.Min(0.55, math.Max(0, rule)+0.1)
}

// meanDiff is the average of now − set over the four tyres.
func meanDiff(now, set [4]float64) float64 {
	d := 0.0
	for w := 0; w < 4; w++ {
		d += now[w] - set[w]
	}
	return d / 4
}

// fitsShifted: now equals set shifted by the same whole number of clicks (0.1 psi) on every tyre,
// within the range the rule allows (−0.1 −0.1 −0.2 −0.2 is no shift: rig setups 2635 and 2727).
func fitsShifted(now, set [4]float64, rule float64) bool {
	s := math.Round(meanDiff(now, set)*10) / 10
	lo, hi := shiftRange(rule)
	if s < lo-1e-9 || s > hi+1e-9 {
		return false
	}
	for w := 0; w < 4; w++ {
		if math.Abs(now[w]-set[w]-s) > 0.02 {
			return false
		}
	}
	return true
}

func matchScore(t game.Telemetry, content []byte, rule float64) (score int, cold [4]float64, hasCold bool) {
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
		var pit [4]float64
		same := true
		for w := 0; w < 4; w++ {
			p, ok := real[fmt.Sprintf(pitPressurePath, w)]
			pit[w], same = p, same && ok
		}
		if same && fitsShifted(t.PitPressures, pit, rule) {
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
