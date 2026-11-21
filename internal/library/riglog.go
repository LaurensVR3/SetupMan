package library

// Rig-test log (D-140): what SetupMan sees of the games goes into setupman.log in plain lines, so
// findings from the sim rig can be fixed from facts afterwards (shared-memory offsets, driving
// detection, lap recording, setup recognition). A few lines per lap at most: nothing here runs
// more often than the live check, and only when something changes.

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/lastopened"
	"setupman/internal/store"
)

// logLiveChange writes a line when a game starts, stops, or the driver starts or stops driving.
func logLiveChange(g game.Game, from, to game.LiveState) {
	say := map[game.LiveState]string{game.LiveUnknown: "not running", game.LiveNotDriving: "running, not driving", game.LiveDriving: "driving"}
	log.Printf("game %s: %s (was %s)", game.Short(g), say[to], say[from])
}

// logScan writes what a folder scan changed in the library (nothing when nothing changed).
func logScan(g game.Game, r history.Result, gameRunning bool) {
	if r.Imported > 0 {
		log.Printf("scan %s: first import, %d setups", game.Short(g), r.Imported)
		return
	}
	if r.NewSetups+r.NewVersions+r.Branches+r.Renamed+r.Removed+r.Returned == 0 {
		return
	}
	log.Printf("scan %s: %d new setups, %d branches, %d new versions, %d renamed, %d removed, %d back in the folder (game running: %v)",
		game.Short(g), r.NewSetups, r.Branches, r.NewVersions, r.Renamed, r.Removed, r.Returned, gameRunning)
}

// logSession writes the start of a recorded session with every value the game reports.
func logSession(g game.Game, t game.Telemetry) {
	log.Printf("rec %s: new session %q, car %q, track %q, laps done %d, %s", game.Short(g), t.Session, t.Car, t.Track, t.CompletedLaps, liveSnapshot(t))
}

// logPits writes entering or leaving the pits (checks the pit box and pit lane flags).
func logPits(g game.Game, t game.Telemetry, entered bool) {
	what := "left the pits"
	if entered {
		what = "entered the pits"
	}
	lane := "?"
	if t.PitLaneKnown {
		lane = fmt.Sprint(t.InPitLane)
	}
	log.Printf("rec %s: %s (pit box %v, pit lane %s, fuel %.1f L)", game.Short(g), what, t.InPit, lane, t.Fuel)
}

// logMatch writes which setup SetupMan thinks is driven, with the values it compared. kept is the
// setup the laps stay with when nothing matches now (e.g. after refuelling at a stop).
func logMatch(g game.Game, t game.Telemetry, candidates int, name, kept, how string) {
	who := "not recognised"
	switch {
	case name != "":
		who = fmt.Sprintf("recognised as %q", name)
		if how != "" {
			who += " from " + how
		}
	case kept != "":
		who = fmt.Sprintf("not recognised again (laps stay with %q)", kept)
	}
	if name == "" && how != "" {
		who += " (" + how + ")"
	}
	menu := "pit menu ?"
	if t.PitMenuKnown {
		menu = fmt.Sprintf("pit menu %.0f L, %.1f %.1f %.1f %.1f psi", t.PitFuel, t.PitPressures[0], t.PitPressures[1], t.PitPressures[2], t.PitPressures[3])
	}
	extra := ""
	if t.BiasKnown {
		extra += fmt.Sprintf(", brake bias %.4f", t.BrakeBias)
	}
	if t.PressuresKnown {
		extra += fmt.Sprintf(", tyres now %.1f %.1f %.1f %.1f psi", t.Pressures[0], t.Pressures[1], t.Pressures[2], t.Pressures[3])
	}
	log.Printf("rec %s: setup %s among %d setups in the folder (car reports fuel %.1f L, TC %s, ABS %s, map %s, %s%s)",
		game.Short(g), who, candidates, t.Fuel, num(t.TC), num(t.ABS), num(t.Map), menu, extra)
}

// logOpened writes when the game started and which setup files it opened since (D-147), newest
// first, so the rig test shows whether the game reads only the setup it loads.
func logOpened(g game.Game, start time.Time, names []string, opened []time.Time) {
	st := lastopened.Check()
	if !st.Enabled {
		known := "off"
		if !st.Known {
			known = "unknown"
		}
		log.Printf("rec %s: Windows last-opened times %s; recognising from values only", game.Short(g), known)
		return
	}
	type f struct {
		name string
		at   time.Time
	}
	var list []f
	for i, at := range opened {
		if !at.IsZero() {
			list = append(list, f{names[i], at})
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].at.After(list[b].at) })
	var b strings.Builder
	started := "unknown"
	if !start.IsZero() {
		started = start.Format("15:04:05")
	}
	fmt.Fprintf(&b, "rec %s: game started %s; %d of %d setups opened since", game.Short(g), started, len(list), len(names))
	for k, x := range list {
		if k == 8 {
			fmt.Fprintf(&b, ", …")
			break
		}
		fmt.Fprintf(&b, "%s %q at %s", map[bool]string{true: ":", false: ","}[k == 0], x.name, x.at.Format("15:04:05.000"))
	}
	log.Print(b.String())
}

// logLap writes one recorded lap and what the game reports at the line.
func logLap(g game.Game, lapNo int, l *store.Lap, t game.Telemetry) {
	var b strings.Builder
	fmt.Fprintf(&b, "rec %s: lap %d %s valid=%v pit=%v", game.Short(g), lapNo, msText(l.LapMs), l.Valid, l.Pit)
	if l.FuelUsed > 0 {
		fmt.Fprintf(&b, " fuel used %.2f L", l.FuelUsed)
	} else {
		b.WriteString(" fuel used ?")
	}
	if l.HasTemps {
		fmt.Fprintf(&b, " air %.1f road %.1f °C", l.Air, l.Road)
	}
	if l.HasPressures {
		fmt.Fprintf(&b, " hot psi %.1f %.1f %.1f %.1f", l.Pressures[0], l.Pressures[1], l.Pressures[2], l.Pressures[3])
	}
	if l.Wet {
		b.WriteString(" rain tyres")
	}
	if l.SetupID == 0 {
		b.WriteString(" (setup unknown)")
	}
	fmt.Fprintf(&b, "; now %s", liveSnapshot(t))
	log.Print(b.String())
}

// liveSnapshot lists the values used for pit stop advice; "?" = the game doesn't report it (yet).
func liveSnapshot(t game.Telemetry) string {
	parts := []string{fmt.Sprintf("fuel %.1f L", t.Fuel)}
	if t.TankKnown {
		parts = append(parts, fmt.Sprintf("tank %.0f L", t.TankSize))
	} else {
		parts = append(parts, "tank ?")
	}
	if t.TimeLeftKnown {
		parts = append(parts, "time left "+msText(int(t.TimeLeftMs)))
	} else {
		parts = append(parts, "time left ?")
	}
	if t.SessionLaps > 0 {
		parts = append(parts, fmt.Sprintf("session laps %d", t.SessionLaps))
	}
	if t.StintKnown {
		parts = append(parts, "stint left "+msText(t.StintLeftMs))
	}
	if t.StopsKnown {
		parts = append(parts, fmt.Sprintf("stops left %d", t.StopsLeft))
	}
	if t.PitMenuKnown {
		parts = append(parts, fmt.Sprintf("pit menu %.0f L, %.1f %.1f %.1f %.1f psi", t.PitFuel, t.PitPressures[0], t.PitPressures[1], t.PitPressures[2], t.PitPressures[3]))
	}
	if t.WetKnown {
		parts = append(parts, fmt.Sprintf("rain tyres %v", t.WetTyres))
	}
	if t.TempsKnown {
		parts = append(parts, fmt.Sprintf("air %.1f road %.1f °C", t.AirTemp, t.RoadTemp))
	}
	if t.PressuresKnown {
		parts = append(parts, fmt.Sprintf("hot psi %.1f %.1f %.1f %.1f", t.Pressures[0], t.Pressures[1], t.Pressures[2], t.Pressures[3]))
	}
	parts = append(parts, fmt.Sprintf("TC %s ABS %s map %s", num(t.TC), num(t.ABS), num(t.Map)))
	return strings.Join(parts, ", ")
}

func num(v int) string {
	if v < 0 {
		return "?"
	}
	return fmt.Sprint(v)
}
