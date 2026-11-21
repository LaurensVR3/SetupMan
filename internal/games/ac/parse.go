package ac

import (
	"bufio"
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"

	"setupman/internal/setup"
)

// An AC setup is an INI file: one [SECTION] per setting with VALUE=<clicks>, e.g.
//
//	[PRESSURE_LF]
//	VALUE=17
//
// plus [CAR] MODEL=<car folder>, and optionally [ABOUT] AUTHOR=… and [__EXT_PATCH] VERSION=…
// (written by Custom Shaders Patch). Values are clicks, except tyre pressure (psi) and fuel (litres).

// Parse reads an AC setup file.
func Parse(raw []byte) ([]setup.Field, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	type kv struct{ sec, key, val string }
	var items []kv
	sec := ""
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if !strings.HasSuffix(line, "]") {
				return nil, errors.New("not an AC setup file")
			}
			sec = strings.ToUpper(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || sec == "" {
			return nil, errors.New("not an AC setup file")
		}
		items = append(items, kv{sec, strings.ToUpper(strings.TrimSpace(k)), strings.TrimSpace(v)})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	hasCar := false
	var fields []setup.Field
	for _, it := range items {
		if it.sec == "CAR" && it.key == "MODEL" {
			hasCar = true
		}
		fields = append(fields, makeField(it.sec, it.key, it.val))
	}
	if !hasCar && len(fields) < 3 {
		return nil, errors.New("not an AC setup file")
	}
	sort.SliceStable(fields, func(i, j int) bool { return sortKey(fields[i].Path) < sortKey(fields[j].Path) })
	return fields, nil
}

// carModel returns the car folder named inside the file ("" if none).
func carModel(fields []setup.Field) string {
	for _, f := range fields {
		if f.Path == "CAR.MODEL" {
			return f.Value
		}
	}
	return ""
}

func makeField(sec, key, val string) setup.Field {
	path := sec
	if key != "VALUE" {
		path = sec + "." + key
	}
	label, section := describe(sec, key)
	f := setup.Field{Path: path, Label: label, Section: section, Value: val}
	if n, err := strconv.ParseFloat(val, 64); err == nil {
		f.Num = n
		if _, err := strconv.ParseInt(val, 10, 64); err == nil {
			f.Kind = "int"
		} else {
			f.Kind = "float"
		}
	} else {
		f.Kind = "string"
	}
	switch {
	case sec == "CAR" || sec == "ABOUT" || strings.HasPrefix(sec, "__"):
		f.Excluded, f.Hidden = true, true // car name, author, Custom Shaders Patch details: not part of the setup
	case strings.HasPrefix(sec, "PRESSURE_"):
		f.Unit, f.Step = "psi", 1
	case sec == "FUEL":
		f.Unit, f.Step = "L", 1
	}
	f.Signed = f.Kind == "int" && !f.Excluded // AC stores some settings below zero, e.g. camber -39
	return f
}

var wheelSuffix = map[string]string{"LF": "FL", "RF": "FR", "LR": "RL", "RR": "RR"}

var wheelRank = map[string]string{"LF": "0", "RF": "1", "LR": "2", "RR": "3"}

// sortKey puts the four wheels of a setting in the order FL, FR, RL, RR.
func sortKey(path string) string {
	if i := strings.LastIndex(path, "_"); i > 0 {
		if r, ok := wheelRank[path[i+1:]]; ok {
			return path[:i] + "_" + r
		}
	}
	return path
}

// Readable names for AC's settings; anything else gets a name made from its section.
var names = []struct {
	prefix, label, section string
}{
	{"PRESSURE_", "Tyre pressure", "Tyres"},
	{"TYRES", "Tyre compound", "Tyres"},
	{"CAMBER_", "Camber", "Alignment"},
	{"TOE_OUT_", "Toe", "Alignment"},
	{"ROD_LENGTH_", "Ride height", "Alignment"},
	{"ABS", "ABS", "Electronics"},
	{"TRACTION_CONTROL", "TC", "Electronics"},
	{"ENGINE_LIMITER", "Engine limiter", "Electronics"},
	{"TURBO", "Turbo boost", "Electronics"},
	{"STEER_ASSIST", "Steering assist", "Electronics"},
	{"FUEL", "Fuel", "Strategy"},
	{"ARB_FRONT", "Anti-roll bar front", "Mechanical balance"},
	{"ARB_REAR", "Anti-roll bar rear", "Mechanical balance"},
	{"SPRING_RATE_", "Spring rate", "Mechanical balance"},
	{"PACKER_RANGE_", "Bump stop range", "Mechanical balance"},
	{"BUMP_STOP_RATE_", "Bump stop rate", "Mechanical balance"},
	{"FRONT_BIAS", "Brake bias", "Mechanical balance"},
	{"BRAKE_POWER_MULT", "Brake power", "Mechanical balance"},
	{"DAMP_BUMP_", "Bump", "Dampers"},
	{"DAMP_FAST_BUMP_", "Fast bump", "Dampers"},
	{"DAMP_REBOUND_", "Rebound", "Dampers"},
	{"DAMP_FAST_REBOUND_", "Fast rebound", "Dampers"},
	{"WING_", "Wing", "Aero balance"},
	{"DIFF_POWER", "Differential power", "Drivetrain"},
	{"DIFF_COAST", "Differential coast", "Drivetrain"},
	{"DIFF_PRELOAD", "Differential preload", "Drivetrain"},
	{"FINAL_RATIO", "Final drive", "Drivetrain"},
	{"FINAL_GEAR_RATIO", "Final drive", "Drivetrain"},
	{"INTERNAL_GEAR_", "Gear", "Drivetrain"},
}

func describe(sec, key string) (label, section string) {
	switch sec {
	case "CAR":
		return "Car", "Car"
	case "ABOUT":
		return capital(strings.ToLower(key)), "Car"
	case "__EXT_PATCH":
		return "Custom Shaders Patch " + strings.ToLower(key), "Car"
	}
	for _, n := range names {
		if sec == n.prefix || (strings.HasSuffix(n.prefix, "_") && strings.HasPrefix(sec, n.prefix)) {
			rest := strings.TrimPrefix(sec, n.prefix)
			switch {
			case rest == "":
				return n.label, n.section
			case wheelSuffix[rest] != "":
				return n.label + " " + wheelSuffix[rest], n.section
			default:
				return n.label + " " + rest, n.section // WING_1 → "Wing 1", INTERNAL_GEAR_3 → "Gear 3"
			}
		}
	}
	// Unknown setting: "BRAKE_DUCT_FRONT" → "Brake duct front".
	l := strings.ToLower(strings.ReplaceAll(sec, "_", " "))
	for s, w := range wheelSuffix {
		if strings.HasSuffix(sec, "_"+s) {
			l = strings.TrimSuffix(l, " "+strings.ToLower(s)) + " " + w
		}
	}
	return capital(l), "Other"
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
