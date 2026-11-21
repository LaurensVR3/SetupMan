package acc

// Real values per car (spec D-141): ACC stores most settings as clicks, and what a click means
// differs per car (camber −4.0° + 0.1° per click on one car, −3.5° on another; caster and wheel
// rates are lookup tables). carunits.json holds those numbers for every ACC car, taken from the
// open-source Race Element project; only the numbers are used, not its code. Corrected after
// checking against the game (D-151): porsche_718_cayman_gt4_mr ride height front base 111 mm and
// rear 99 mm (Race Element: 96 and 84; ACC 1.10 showed 111 / 105 for front 0 / rear 6 clicks).

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
)

//go:embed carunits.json
var carUnitsJSON []byte

// conversion turns clicks into the value the game shows: base + clicks × step, or table[clicks].
type conversion struct {
	Base  float64   `json:"base"`
	Step  float64   `json:"step"`
	Table []float64 `json:"table"`
}

// carConversions: per setting (e.g. "Camber"), the conversion for the front, the rear, or all wheels.
type carConversions struct {
	Compound string // "GT3" or "GT4": which tyre pressure scale the car uses
	Settings map[string]map[string]conversion
}

var carUnits = loadCarUnits()

func loadCarUnits() map[string]carConversions {
	var doc struct {
		Cars map[string]map[string]json.RawMessage `json:"cars"`
	}
	if err := json.Unmarshal(carUnitsJSON, &doc); err != nil {
		panic("carunits.json: " + err.Error()) // only possible if the embedded file is broken (a test checks it)
	}
	out := map[string]carConversions{}
	for car, fields := range doc.Cars {
		c := carConversions{Settings: map[string]map[string]conversion{}}
		for k, raw := range fields {
			if k == "compound" {
				_ = json.Unmarshal(raw, &c.Compound)
				continue
			}
			var per map[string]conversion
			if json.Unmarshal(raw, &per) == nil {
				c.Settings[k] = per
			}
		}
		out[car] = c
	}
	return out
}

// settingOf maps ACC's JSON keys to a setting in carunits.json and the unit ACC shows.
var settingOf = map[string]struct{ setting, unit string }{
	"camber":         {"Camber", "°"},
	"toe":            {"Toe", "°"},
	"casterLF":       {"Caster", "°"},
	"casterRF":       {"Caster", "°"},
	"steerRatio":     {"SteeringRatio", ""},
	"aRBFront":       {"AntiRollBarFront", ""},
	"aRBRear":        {"AntiRollBarRear", ""},
	"wheelRate":      {"WheelRate", "N/m"},
	"bumpStopRateUp": {"BumpstopRate", "N"},
	"bumpStopWindow": {"BumpstopRange", ""},
	"brakeTorque":    {"BrakePower", "%"},
	"brakeBias":      {"BrakeBias", "%"},
	"rideHeight":     {"RideHeight", "mm"},
	"brakeDuct":      {"BrakeDucts", ""},
	"rearWing":       {"RearWing", ""},
	"splitter":       {"Splitter", ""},
	"preload":        {"PreloadDifferential", "Nm"},
	"bumpSlow":       {"BumpSlow", ""},
	"bumpFast":       {"BumpFast", ""},
	"reboundSlow":    {"ReboundSlow", ""},
	"reboundFast":    {"ReboundFast", ""},
}

// gt4Pressure is the tyre pressure scale of GT4-type tyres (17.0 psi + 0.1 per click);
// GT3-type tyres start at 20.3 psi (realUnits).
const gt4Pressure = 17.0

// rideHeightNote: ACC shifts ride heights per track (Balance of Performance; the setup file's
// trackBopType) and no public source has those numbers, so the mm can be a few off (D-158, Q-RIG7).
const rideHeightNote = "Ride heights can differ a few mm from the game: ACC adjusts them per track (Balance of Performance)."

// applyCarUnits gives the fields of a setup for a known car their real values.
func applyCarUnits(s *Setup) {
	car, ok := carUnits[strings.ToLower(s.CarName)]
	if !ok {
		return
	}
	for i := range s.Fields {
		f := &s.Fields[i]
		leaf, idx := leafOf(f.Path)
		if leaf == "tyrePressure" && car.Compound == "GT4" {
			f.Offset = gt4Pressure
			continue
		}
		so, ok := settingOf[leaf]
		if !ok || f.Kind != "int" {
			continue
		}
		if leaf == "rideHeight" && (idx == 1 || idx == 3) {
			continue // the game uses entry 0 (front) and 2 (rear); the meaning of the others is not known
		}
		per := car.Settings[so.setting]
		c, ok := per[position(leaf, idx)]
		if !ok {
			c, ok = per["all"]
		}
		if !ok || (len(c.Table) == 0 && c.Base == 0 && c.Step == 1 && so.unit == "") {
			continue // the game shows the clicks themselves
		}
		f.Real, f.Unit = true, so.unit
		if leaf == "rideHeight" {
			f.Note = rideHeightNote
		}
		if len(c.Table) > 0 {
			f.Table, f.Offset, f.Step = c.Table, 0, 0
		} else {
			f.Offset, f.Step = c.Base, c.Step
		}
	}
}

// position: front or rear for settings stored per wheel (FL FR RL RR) or per axle (front, rear).
func position(leaf string, idx int) string {
	switch {
	case idx < 0:
		return "all"
	case leaf == "brakeDuct":
		return []string{"front", "rear"}[min(idx, 1)]
	case idx <= 1:
		return "front"
	default:
		return "rear"
	}
}

// leafOf returns the last key of a field path and its array index (-1 when none):
// "basicSetup.alignment.camber[2]" → ("camber", 2).
func leafOf(path string) (string, int) {
	leaf := path[strings.LastIndexAny(path, ".#")+1:]
	idx := -1
	if i := strings.IndexByte(leaf, '['); i >= 0 {
		if n, err := strconv.Atoi(strings.TrimSuffix(leaf[i+1:], "]")); err == nil {
			idx = n
		}
		leaf = leaf[:i]
	}
	return leaf, idx
}
