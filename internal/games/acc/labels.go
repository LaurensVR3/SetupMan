package acc

import (
	"strconv"
	"strings"
	"unicode"
)

// excludedPaths are shown in the comparison table but do not count for the
// similarity score (spec D-20). Paths are written without array indexes.
var excludedPaths = map[string]bool{
	"carName":                              true, // same within a car folder anyway
	"trackBopType":                         true, // tied to the track, not to car behaviour
	"basicSetup.electronics.telemetryLaps": true,
	// Just the tyre set selected when saving; not a setup choice (spec D-22):
	"basicSetup.strategy.tyreSet":             true,
	"basicSetup.strategy.pitStrategy.tyreSet": true,
	// ACC's own fuel estimate, often wrong; later shown next to measured fuel use (spec D-25):
	"basicSetup.strategy.fuelPerLap": true,
	// Floats the game appears to derive from the click values (would count a change twice):
	"basicSetup.alignment.staticCamber":   true,
	"basicSetup.alignment.toeOutLinear":   true,
	"advancedSetup.aeroBalance.rodLength": true,
}

func isExcluded(path string) bool { return excludedPaths[stripIndexes(path)] }

// leafLabels gives readable names for ACC's JSON keys.
// Unknown keys fall back to a split camelCase version of the key.
var leafLabels = map[string]string{
	"carName":               "Car",
	"trackBopType":          "Track BoP type",
	"tyreCompound":          "Tyre compound",
	"tyrePressure":          "Tyre pressure",
	"camber":                "Camber",
	"toe":                   "Toe",
	"staticCamber":          "Static camber",
	"toeOutLinear":          "Toe out (linear)",
	"casterLF":              "Caster FL",
	"casterRF":              "Caster FR",
	"steerRatio":            "Steering ratio",
	"tC1":                   "TC",
	"tC2":                   "TC cut",
	"abs":                   "ABS",
	"eCUMap":                "Engine map",
	"fuelMix":               "Fuel mix",
	"telemetryLaps":         "Telemetry laps",
	"fuel":                  "Fuel",
	"nPitStops":             "Pit stops",
	"tyreSet":               "Tyre set",
	"frontBrakePadCompound": "Brake pads front",
	"rearBrakePadCompound":  "Brake pads rear",
	"fuelToAdd":             "Fuel to add",
	"fuelPerLap":            "Fuel per lap",
	"aRBFront":              "Anti-roll bar front",
	"aRBRear":               "Anti-roll bar rear",
	"wheelRate":             "Wheel rate",
	"bumpStopRateUp":        "Bump stop rate",
	"bumpStopRateDn":        "Bump stop rate (down)",
	"bumpStopWindow":        "Bump stop range",
	"brakeTorque":           "Brake power",
	"brakeBias":             "Brake bias",
	"bumpSlow":              "Bump slow",
	"bumpFast":              "Bump fast",
	"reboundSlow":           "Rebound slow",
	"reboundFast":           "Rebound fast",
	"rideHeight":            "Ride height",
	"rodLength":             "Rod length",
	"splitter":              "Splitter",
	"rearWing":              "Rear wing",
	"brakeDuct":             "Brake duct",
	"preload":               "Diff preload",
}

// Wheel order for 4-element arrays. Assumed FL, FR, RL, RR – to be verified in-game.
var wheelNames = []string{"FL", "FR", "RL", "RR"}

func label(keys []string, idx, arrLen int) string {
	if len(keys) == 0 {
		return "?"
	}
	leaf := keys[len(keys)-1]
	l, ok := leafLabels[leaf]
	if !ok {
		l = splitCamel(leaf)
	}
	switch {
	case idx < 0:
		return l
	case arrLen == 4:
		return l + " " + wheelNames[idx]
	case arrLen == 2:
		return l + " " + []string{"front", "rear"}[idx]
	default:
		return l + " " + strconv.Itoa(idx+1)
	}
}

// section is the name of the object that contains the field, e.g. "Tyres".
func section(keys []string) string {
	if len(keys) < 2 {
		return "General"
	}
	return splitCamel(keys[len(keys)-2])
}

// splitCamel turns "mechanicalBalance" into "Mechanical balance".
func splitCamel(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
			r = unicode.ToLower(r)
		}
		if i == 0 {
			r = unicode.ToUpper(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// choices names click values that mean the same on every car (D-120): ACC stores the tyre
// compound as 0 = dry, 1 = wet, and brake pads 1–4 as 0–3 (the game shows them as 1–4).
var choices = map[string][]string{
	"tyreCompound":          {"Dry", "Wet"},
	"frontBrakePadCompound": {"1", "2", "3", "4"},
	"rearBrakePadCompound":  {"1", "2", "3", "4"},
}

// realUnits converts what the file stores to what ACC shows for settings that work the same on
// every car (spec D-101, D-144; checked on the rig against ACC 1.10). Tyre pressure starts at
// 17.0 psi instead on GT4-type tyres, and the other settings are converted per car (carunits.go,
// D-141). The file stores fuel 2 L below what the game shows (the game's minimum is 2 L), and
// ECU map, tyre set and the pit stop number from 0 while the game counts from 1.
var realUnits = map[string]struct {
	unit         string
	offset, step float64
}{
	"tyrePressure": {"psi", 20.3, 0.1},
	"fuel":         {"L", 2, 1},
	"fuelToAdd":    {"L", 0, 1},
	"eCUMap":       {"", 1, 1},
	"tyreSet":      {"", 1, 1},
	"nPitStops":    {"", 1, 1},
	"fuelPerLap":   {"L/lap", 0, 0},
}
