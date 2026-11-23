package telemetry

import "strings"

// ChannelInfo describes a channel for the Telemetry pages: a readable name, a group for the channel
// picker, the unit shown, and the factor from the stored unit (e.g. m/s → km/h).
type ChannelInfo struct {
	Name   string  `json:"name"`  // as stored (e.g. "SPEED", "TYRE_PRESS_LF", "x:SLIP_ANGLE_FL")
	Label  string  `json:"label"` // e.g. "Speed", "Tyre pressure FL"
	Group  string  `json:"group"` // e.g. "Driver", "Tyres"
	Unit   string  `json:"unit"`  // shown unit
	Hz     int     `json:"hz"`
	Source string  `json:"source"`          // "motec" or "extra" (SetupMan's extra log, D-162)
	Wheel  string  `json:"wheel,omitempty"` // FL, FR, RL, RR for per-wheel channels
	Base   string  `json:"base,omitempty"`  // the channel without the wheel, e.g. "TYRE_PRESS" (car detail view)
	Scale  float64 `json:"-"`
	Step   bool    `json:"step,omitempty"` // a value that jumps (gear, levels): drawn as steps
}

// Groups in the order the channel picker shows them.
var Groups = []string{"Driver", "Timing", "Tyres", "Brakes", "Suspension", "Chassis", "Electronics", "Engine", "Track", "Other"}

type known struct {
	label, group, unit string
	scale              float64
	step               bool
}

// motecChannels: ACC's MoTeC channels (checked on the user's files, 2026-11-22). Per-wheel
// channels end in _LF, _RF, _LR, _RR and are listed once without the wheel.
var motecChannels = map[string]known{
	"SPEED":           {"Speed", "Driver", "km/h", 3.6, false},
	"THROTTLE":        {"Throttle", "Driver", "%", 1, false},
	"BRAKE":           {"Brake", "Driver", "%", 1, false},
	"CLUTCH":          {"Clutch", "Driver", "%", 1, false},
	"STEERANGLE":      {"Steering angle", "Driver", "°", 1, false},
	"GEAR":            {"Gear", "Driver", "", 1, true},
	"RPMS":            {"Engine speed", "Engine", "rpm", 1, false},
	"TC":              {"TC level", "Electronics", "", 1, true},
	"ABS":             {"ABS level", "Electronics", "", 1, true},
	"G_LAT":           {"Lateral G", "Chassis", "G", 1, false}, // ACC stores G already (checked: yaw rate × speed)
	"G_LON":           {"Longitudinal G", "Chassis", "G", 1, false},
	"ROTY":            {"Yaw rate", "Chassis", "°/s", 57.29578, false},
	"TIME":            {"Lap clock", "Timing", "s", 1, false},
	"LAP_BEACON":      {"Lap beacon", "Timing", "", 1, true},
	"SUS_TRAVEL":      {"Suspension travel", "Suspension", "mm", 1, false},
	"BUMPSTOPUP_RIDE": {"Bump stop ride (upper)", "Suspension", "", 1, false},
	"BUMPSTOPDN_RIDE": {"Bump stop ride (lower)", "Suspension", "", 1, false},
	"BUMPSTOP_FORCE":  {"Bump stop force", "Suspension", "", 1, false},
	"BRAKE_TEMP":      {"Brake temperature", "Brakes", "°C", 1, false},
	"TYRE_PRESS":      {"Tyre pressure", "Tyres", "psi", 1, false},
	"TYRE_TAIR":       {"Tyre temperature", "Tyres", "°C", 1, false},
	"WHEEL_SPEED":     {"Wheel speed", "Tyres", "km/h", 3.6, false},
}

var wheelSuffix = map[string]string{"LF": "FL", "RF": "FR", "LR": "RL", "RR": "RR", "FL": "FL", "FR": "FR", "RL": "RL"}

// describe returns the readable description of a channel stored under name with unit and hz.
func describe(name, unit string, hz int, source string) ChannelInfo {
	ci := ChannelInfo{Name: name, Label: name, Group: "Other", Unit: unit, Hz: hz, Source: source, Scale: 1}
	key := strings.TrimPrefix(name, extraPrefix)
	table := motecChannels
	if source == "extra" {
		table = extraKnown
	}
	if k, ok := table[key]; ok {
		ci.Label, ci.Group, ci.Unit, ci.Scale, ci.Step = k.label, k.group, k.unit, k.scale, k.step
		return ci
	}
	if i := strings.LastIndexByte(key, '_'); i > 0 {
		if w, ok := wheelSuffix[strings.ToUpper(key[i+1:])]; ok {
			if k, ok := table[key[:i]]; ok {
				ci.Label, ci.Group, ci.Unit, ci.Scale, ci.Step = k.label+" "+w, k.group, k.unit, k.scale, k.step
				ci.Wheel, ci.Base = w, key[:i]
				return ci
			}
		}
	}
	return ci
}
