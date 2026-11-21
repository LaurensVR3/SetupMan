package ace

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"setupman/internal/setup"
)

// What the numbered fields mean, worked out by comparing setups of five cars (D-111). Values are
// real units (N/m, psi, degrees, mm, litres), not clicks. Names marked "unverified" in the spec
// still have to be checked against the game's setup screen.
type known struct {
	label, section, unit string
	excluded             bool // worked out by the game, or not a setting
}

// Paths with "{w}" exist once per wheel: [0] FL, [1] FR, [2] RL, [3] RR.
var schema = map[string]known{
	"1.1[0]":   {"Anti-roll bar front", "Mechanical balance", "N/m", false},
	"1.1[1]":   {"Anti-roll bar rear", "Mechanical balance", "N/m", false},
	"1.2":      {"Steering ratio", "Mechanical balance", "", false},
	"1.3.1":    {"Brake bias", "Mechanical balance", "%", false},
	"1.3.2":    {"Brake power", "Mechanical balance", "%", false},
	"1.4.1":    {"Differential coast", "Drivetrain", "", false},
	"1.4.2":    {"Differential power", "Drivetrain", "", false},
	"1.4.3":    {"Differential preload", "Drivetrain", "Nm", false},
	"2{w}.1":   {"Spring rate", "Mechanical balance", "N/m", false},
	"2{w}.2.1": {"Bump stop gap", "Mechanical balance", "m", false},
	"2{w}.2.2": {"Bump stop rate", "Mechanical balance", "N/m", false},
	"2{w}.3.1": {"Rebound stop gap", "Mechanical balance", "m", false},
	"2{w}.3.2": {"Rebound stop rate", "Mechanical balance", "N/m", false},
	"2{w}.4":   {"Spring value 2.4", "Mechanical balance", "", false}, // only some cars (GT4, GT2)
	"2{w}.5":   {"Spring value 2.5", "Mechanical balance", "", false},
	"3{w}.1":   {"Bump", "Dampers", "", false},
	"3{w}.2":   {"Fast bump", "Dampers", "", false},
	"3{w}.3":   {"Rebound", "Dampers", "", false},
	"3{w}.4":   {"Fast rebound", "Dampers", "", false},
	"4{w}.1":   {"Tyre pressure", "Tyres", "psi", false},
	"4{w}.2":   {"Camber", "Alignment", "°", false},
	"4{w}.3":   {"Toe", "Alignment", "°", false},
	"4{w}.4":   {"Alignment value 4.4", "Alignment", "", false},
	"4{w}.5":   {"Calculated value 4.5", "Alignment", "", true},
	"4{w}.6":   {"Calculated value 4.6", "Alignment", "", true},
	"5.1":      {"TC", "Electronics", "", false},
	"5.2":      {"ABS", "Electronics", "", false},
	"5.3":      {"Engine map", "Electronics", "", false},
	"5.4":      {"Electronics value 5.4", "Electronics", "", false},
	"5.5":      {"Electronics value 5.5", "Electronics", "", false},
	"6.2":      {"Ride height front", "Aero balance", "mm", false},
	"6.3":      {"Ride height rear", "Aero balance", "mm", false},
	"6.4":      {"Aero value 6.4", "Aero balance", "", false},
	"6.5":      {"Rear wing", "Aero balance", "", false},
	"7.1":      {"Fuel", "Strategy", "L", false},
	"9":        {"Car preset", "Car", "", true},
	"10":       {"File value 10", "Car", "", true},
	"6.1[0]":   {"Calculated value 6.1 (1)", "Aero balance", "", true},
	"6.1[1]":   {"Calculated value 6.1 (2)", "Aero balance", "", true},
	"6.1[2]":   {"Calculated value 6.1 (3)", "Aero balance", "", true},
	"6.1[3]":   {"Calculated value 6.1 (4)", "Aero balance", "", true},
}

var wheels = []string{"FL", "FR", "RL", "RR"}

var reWheel = regexp.MustCompile(`^(\d+)\[([0-3])\]\.(.+)$`)

// lookup finds the meaning of a path, with the wheel for per-wheel fields.
func lookup(path string) (known, string, bool) {
	if k, ok := schema[path]; ok {
		return k, "", true
	}
	if m := reWheel.FindStringSubmatch(path); m != nil {
		if k, ok := schema[m[1]+"{w}."+m[3]]; ok {
			i, _ := strconv.Atoi(m[2])
			return k, wheels[i], true
		}
	}
	return known{}, "", false
}

// Parse reads an AC EVO .carsetup file.
func Parse(raw []byte) ([]setup.Field, error) {
	var vals []pbValue
	if err := decode(raw, "", 0, &vals); err != nil || len(vals) < 10 {
		return nil, errFormat
	}
	got := map[string]pbValue{}
	for _, v := range vals {
		got[v.path] = v
	}
	// The format leaves out values that are 0; add them, so setups always have the same settings.
	for p := range schema {
		if strings.Contains(p, "{w}") {
			for i := range wheels {
				wp := strings.Replace(p, "{w}", "["+strconv.Itoa(i)+"]", 1)
				if _, ok := got[wp]; !ok {
					got[wp] = pbValue{path: wp, isNum: true}
				}
			}
		} else if _, ok := got[p]; !ok && p != "9" && p != "10" && !strings.HasPrefix(p, "6.1[") {
			got[p] = pbValue{path: p, isNum: true}
		}
	}
	fields := make([]setup.Field, 0, len(got))
	for _, v := range got {
		fields = append(fields, makeField(v))
	}
	sort.Slice(fields, func(i, j int) bool { return pathLess(fields[i].Path, fields[j].Path) })
	return fields, nil
}

func makeField(v pbValue) setup.Field {
	k, wheel, ok := lookup(v.path)
	if !ok {
		k = known{label: "Value " + v.path, section: "Other"}
	}
	f := setup.Field{Path: v.path, Label: k.label, Section: k.section, Excluded: k.excluded, Hidden: k.excluded}
	if wheel != "" {
		f.Label += " " + wheel
	}
	if !v.isNum {
		f.Kind, f.Value = "string", v.text
		return f
	}
	f.Num = round(v.num)
	f.Value = strconv.FormatFloat(f.Num, 'f', -1, 64)
	if f.Num == float64(int64(f.Num)) {
		f.Kind = "int"
	} else {
		f.Kind = "float"
	}
	if k.unit != "" {
		f.Unit, f.Step = k.unit, 1
	}
	return f
}

// round removes float noise (61.2 is stored as 61.200001).
func round(x float64) float64 {
	v, _ := strconv.ParseFloat(fmt.Sprintf("%.5g", x), 64)
	return v
}

// pathLess sorts "2[1].3.2" naturally (numbers as numbers).
func pathLess(a, b string) bool {
	na, nb := wheelLast(a), wheelLast(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] < nb[i]
		}
	}
	return len(na) < len(nb)
}

// wheelLast turns "4[2].1" into 4, 1, 2: per-wheel settings sort by setting, then wheel.
func wheelLast(p string) []int {
	if m := reWheel.FindStringSubmatch(p); m != nil {
		w, _ := strconv.Atoi(m[2])
		return append(append(splitNums(m[1]), splitNums(m[3])...), w)
	}
	return splitNums(p)
}

func splitNums(p string) []int {
	var out []int
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r < '0' || r > '9' }) {
		n, _ := strconv.Atoi(part)
		out = append(out, n)
	}
	return out
}
