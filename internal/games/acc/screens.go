package acc

// ACC's own setup screens (D-144, rig test R-9): the setup page shows the same six tabs as the
// game (Tyres, Electronics, Fuel & strategy, Mechanical grip, Dampers, Aero), in the game's order
// and with the game's names, checked against screenshots of ACC 1.10 on the rig. Settings the game
// doesn't show are hidden (still compared and scored as before, so nothing in the library changes).

import (
	"fmt"
	"sort"
	"strings"
)

// screen is where the game shows one setting and what it calls it.
type screen struct {
	section string   // the game's tab
	group   string   // box within the tab ("" = none)
	row     string   // the game's name for the setting
	cols    []string // per array index: wheel or axle column ("" = shown as its own row)
	short   []string // per array index: suffix for the full label, e.g. "FL"
	rows    []string // per array index: own row name instead of a column (pit strategy pressures)
	hideIdx []int    // array indexes the game doesn't show
	hidden  bool     // the game doesn't show this setting at all
	order   []int    // per array index: position in the row, when the game's column order differs
}

var (
	wheelCols  = []string{"Left front", "Right front", "Left rear", "Right rear"}
	wheelShort = []string{"FL", "FR", "RL", "RR"}
	axleCols   = []string{"Front", "Rear"}
	axleShort  = []string{"front", "rear"}
)

func wheels(section, row string) screen {
	return screen{section: section, row: row, cols: wheelCols, short: wheelShort}
}

const (
	tabTyres  = "Tyres"
	tabElec   = "Electronics"
	tabFuel   = "Fuel & strategy"
	tabMech   = "Mechanical grip"
	tabDamp   = "Dampers"
	tabAero   = "Aero"
	tabOther  = "Other"
	pitPrefix = "pit:" // settings inside one pit strategy entry
	boxFuel   = "Fuel and tyre"
	boxPit    = "Pitstop strategy"
)

// screens lists every known setting in the order the game shows it. Keys are field paths
// without array indexes; settings inside a pit strategy entry start with "pit:".
var screens = []struct {
	key string
	s   screen
}{
	// Tyres: per wheel psi, toe, camber, caster.
	{"basicSetup.tyres.tyrePressure", wheels(tabTyres, "psi")},
	{"basicSetup.alignment.toe", wheels(tabTyres, "Toe")},
	{"basicSetup.alignment.camber", wheels(tabTyres, "Camber")},
	{"basicSetup.alignment.casterLF", screen{section: tabTyres, row: "Caster", cols: []string{"Left front"}, short: []string{"FL"}}},
	{"basicSetup.alignment.casterRF", screen{section: tabTyres, row: "Caster", cols: []string{"Right front"}, short: []string{"FR"}}},
	{"basicSetup.alignment.staticCamber", hide(wheels(tabTyres, "Static camber"))},
	{"basicSetup.alignment.toeOutLinear", hide(wheels(tabTyres, "Toe out (linear)"))},

	// Electronics: TC, ABS, ECUMap, telemetry laps, TC2.
	{"basicSetup.electronics.tC1", screen{section: tabElec, row: "TC"}},
	{"basicSetup.electronics.abs", screen{section: tabElec, row: "ABS"}},
	{"basicSetup.electronics.eCUMap", screen{section: tabElec, row: "ECU map"}},
	{"basicSetup.electronics.telemetryLaps", screen{section: tabElec, row: "Telemetry laps"}},
	{"basicSetup.electronics.tC2", screen{section: tabElec, row: "TC2"}},
	{"basicSetup.electronics.fuelMix", screen{section: tabElec, row: "Fuel mix", hidden: true}},

	// Fuel & strategy: box "Fuel and tyre", then box "Pitstop strategy".
	{"basicSetup.strategy.fuel", screen{section: tabFuel, group: boxFuel, row: "Fuel"}},
	{"basicSetup.tyres.tyreCompound", screen{section: tabFuel, group: boxFuel, row: "Tyre"}},
	{"basicSetup.strategy.tyreSet", screen{section: tabFuel, group: boxFuel, row: "Tyre set"}},
	{"basicSetup.strategy.frontBrakePadCompound", screen{section: tabFuel, group: boxFuel, row: "Front brakes"}},
	{"basicSetup.strategy.rearBrakePadCompound", screen{section: tabFuel, group: boxFuel, row: "Rear brakes"}},
	{"basicSetup.strategy.fuelPerLap", screen{section: tabFuel, group: boxFuel, row: "Fuel per lap"}},
	{"basicSetup.strategy.nPitStops", screen{section: tabFuel, group: boxPit, row: "N° pitstop"}},
	{pitPrefix + "fuelToAdd", screen{section: tabFuel, group: boxPit, row: "Fuel to add"}},
	{pitPrefix + "tyres.tyreCompound", screen{section: tabFuel, group: boxPit, row: "Tyre"}},
	{pitPrefix + "tyres.tyrePressure", screen{section: tabFuel, group: boxPit, rows: wheelCols}},
	{pitPrefix + "frontBrakePadCompound", screen{section: tabFuel, group: boxPit, row: "Front brakes"}},
	{pitPrefix + "rearBrakePadCompound", screen{section: tabFuel, group: boxPit, row: "Rear brakes"}},
	{pitPrefix + "tyreSet", screen{section: tabFuel, group: boxPit, row: "Tyre set", hidden: true}},

	// Mechanical grip: box "Front", the four wheels, box "Rear".
	{"advancedSetup.mechanicalBalance.aRBFront", screen{section: tabMech, group: "Front", row: "Antiroll bar", short: []string{"front"}}},
	{"advancedSetup.mechanicalBalance.brakeTorque", screen{section: tabMech, group: "Front", row: "Brake power"}},
	{"advancedSetup.mechanicalBalance.brakeBias", screen{section: tabMech, group: "Front", row: "Brake bias"}},
	{"basicSetup.alignment.steerRatio", screen{section: tabMech, group: "Front", row: "Steer ratio"}},
	{"advancedSetup.mechanicalBalance.wheelRate", wheels(tabMech, "Wheel rate")},
	{"advancedSetup.mechanicalBalance.bumpStopRateUp", wheels(tabMech, "Bumpstop rate")},
	{"advancedSetup.mechanicalBalance.bumpStopWindow", wheels(tabMech, "Bumpstop range")},
	{"advancedSetup.mechanicalBalance.bumpStopRateDn", hide(wheels(tabMech, "Bumpstop rate (down)"))},
	{"advancedSetup.mechanicalBalance.aRBRear", screen{section: tabMech, group: "Rear", row: "Antiroll bar", short: []string{"rear"}}},
	{"advancedSetup.drivetrain.preload", screen{section: tabMech, group: "Rear", row: "Preload differential"}},

	// Dampers: per wheel bump, fast bump, rebound, fast rebound.
	{"advancedSetup.dampers.bumpSlow", wheels(tabDamp, "Bump")},
	{"advancedSetup.dampers.bumpFast", wheels(tabDamp, "Fast bump")},
	{"advancedSetup.dampers.reboundSlow", wheels(tabDamp, "Rebound")},
	{"advancedSetup.dampers.reboundFast", wheels(tabDamp, "Fast rebound")},

	// Aero: front and rear ride height, splitter, rear wing, brake ducts. ACC stores four ride
	// heights; the game shows entry 1 (front) and 3 (rear).
	// ACC's Aero tab has the rear box on the left and the front box on the right (rig 2026-11-21).
	{"advancedSetup.aeroBalance.rideHeight", screen{section: tabAero, row: "Ride height",
		cols: []string{"Front", "", "Rear", ""}, short: []string{"front", "(2)", "rear", "(4)"}, hideIdx: []int{1, 3}, order: []int{1, 2, 0, 3}}},
	{"advancedSetup.aeroBalance.splitter", screen{section: tabAero, row: "Splitter", cols: []string{"Front"}}},
	{"advancedSetup.aeroBalance.rearWing", screen{section: tabAero, row: "Rear wing", cols: []string{"Rear"}}},
	{"advancedSetup.aeroBalance.brakeDuct", screen{section: tabAero, row: "Brake ducts", cols: axleCols, short: axleShort, order: []int{1, 0}}},
	{"advancedSetup.aeroBalance.rodLength", hide(wheels(tabAero, "Rod length"))},

	{"carName", screen{section: tabOther, row: "Car", hidden: true}},
	{"trackBopType", screen{section: tabOther, row: "Track BoP type", hidden: true}},
}

func hide(s screen) screen { s.hidden = true; return s }

var screenRank = func() map[string]int {
	m := map[string]int{}
	for i, e := range screens {
		m[e.key] = i
	}
	return m
}()

// applyScreens gives every field the game's tab, box, name and wheel column and puts the fields
// in the game's order. Settings SetupMan doesn't know (a future game update) keep their
// generated name and come last, in file order.
func applyScreens(s *Setup) {
	pitEntries := 0
	for _, f := range s.Fields {
		if n := pitEntry(f.Path); n > pitEntries {
			pitEntries = n
		}
	}
	type key struct{ a, b, c, d int }
	keys := make(map[string]key, len(s.Fields))
	pitBase := screenRank[pitPrefix+"fuelToAdd"]
	for i := range s.Fields {
		f := &s.Fields[i]
		entry := pitEntry(f.Path)
		k := stripIndexes(f.Path)
		if entry > 0 {
			k = pitPrefix + strings.TrimPrefix(k, pitStrategyPath+".")
		}
		_, idx := leafOf(f.Path)
		r, ok := screenRank[k]
		if !ok {
			keys[f.Path] = key{len(screens), 0, 0, i}
			continue
		}
		sc := screens[r].s
		pos := max(idx, 0)
		if idx >= 0 && idx < len(sc.order) {
			pos = sc.order[idx]
		}
		if entry > 0 {
			keys[f.Path] = key{pitBase, entry, r, pos}
		} else {
			keys[f.Path] = key{r, 0, 0, pos}
		}
		f.Section, f.Group, f.Row, f.Col = sc.section, sc.group, sc.row, ""
		f.Hidden = sc.hidden
		suffix := ""
		if idx >= 0 && idx < len(sc.rows) {
			f.Row = sc.rows[idx]
		}
		if col := at(sc.cols, idx); col != "" {
			f.Col = col
		}
		if sh := at(sc.short, idx); sh != "" {
			suffix = " " + sh
		}
		for _, h := range sc.hideIdx {
			if h == idx {
				f.Hidden, f.Col = true, ""
			}
		}
		f.Label = f.Row + suffix
		if entry > 0 {
			name := boxPit
			if pitEntries > 1 {
				name = fmt.Sprintf("%s %d", boxPit, entry)
			}
			f.Group = name
			f.Label = fmt.Sprintf("%s %d: %s", boxPit, entry, f.Row)
		}
	}
	sort.SliceStable(s.Fields, func(i, j int) bool {
		a, b := keys[s.Fields[i].Path], keys[s.Fields[j].Path]
		if a.a != b.a {
			return a.a < b.a
		}
		if a.b != b.b {
			return a.b < b.b
		}
		if a.c != b.c {
			return a.c < b.c
		}
		return a.d < b.d
	})
}

// at returns list[idx], or for a setting without an array index (idx -1) the only entry.
func at(list []string, idx int) string {
	if idx < 0 {
		if len(list) == 1 {
			return list[0]
		}
		return ""
	}
	if idx < len(list) {
		return list[idx]
	}
	return ""
}

// pitEntry returns n for a field of the n-th pit strategy entry ("…pitStrategy#n.…"), else 0.
func pitEntry(path string) int {
	rest, ok := strings.CutPrefix(path, pitStrategyPath+"#")
	if !ok {
		return 0
	}
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
