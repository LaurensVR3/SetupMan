package library

// Pit stop advice (spec D-124): during a session, propose the pit-menu values (fuel to add, tyre
// pressures) from the laps driven so far and the live telemetry. Nothing here writes anything;
// SetupMan can't change the game's pit menu.

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"setupman/internal/game"
	"setupman/internal/naming"
	"setupman/internal/store"
)

const (
	pressureLaps     = 3    // laps averaged for tyre pressures
	fuelLaps         = 5    // laps averaged for fuel use and lap time
	firmFuelLaps     = 3    // fewer laps than this: the fuel advice is marked as provisional
	defaultDryTarget = 27.0 // psi, hot (user, D-124)
	defaultWetTarget = 30.0
	defaultMargin    = 2.0 // litres, as the fuel calculator (D-55)
)

// PitAdvice is what the Pit stop page shows.
type PitAdvice struct {
	Active    bool        `json:"active"` // a session is being recorded
	Game      string      `json:"game,omitempty"`
	Short     string      `json:"short,omitempty"`
	Car       string      `json:"car,omitempty"`
	Track     string      `json:"track,omitempty"`
	CarName   string      `json:"carName,omitempty"`
	TrackName string      `json:"trackName,omitempty"`
	Session   string      `json:"session,omitempty"`
	Fuel      *FuelAdvice `json:"fuel,omitempty"`
	Tyres     *TyreAdvice `json:"tyres,omitempty"`
}

// FuelAdvice is the fuel part of the pit stop advice.
type FuelAdvice struct {
	Laps       int     `json:"laps"`       // laps the averages are based on
	Firm       bool    `json:"firm"`       // based on enough laps
	FuelPerLap float64 `json:"fuelPerLap"` // litres
	LapMs      int     `json:"lapMs"`      // average lap time
	Timed      bool    `json:"timed"`      // session ends on time (else on laps)
	RaceEnd    string  `json:"raceEnd"`    // timed race: "leader" (from where the leader is), "you" (you lead), "unknown" (leader not known: counted as if you lead), D-198
	TimeLeftMs float64 `json:"timeLeftMs"`
	LapsToGo   float64 `json:"lapsToGo"`
	FuelNow    float64 `json:"fuelNow"`
	Margin     float64 `json:"margin"`
	FuelNeeded float64 `json:"fuelNeeded"` // to the finish, including the margin
	MinNeeded  float64 `json:"minNeeded"`  // to the finish, without a margin (D-178)
	Spare      float64 `json:"spare"`      // fuel now minus MinNeeded: left over at the finish (negative = short), D-178
	LastStop   bool    `json:"lastStop"`   // the next stop is the last: FuelToAdd is the minimum, no margin (D-178)
	MinToAdd   float64 `json:"minToAdd"`   // the exact minimum the last stop must add (FuelToAdd rounds it up), D-178
	StopsLeft  int     `json:"stopsLeft"`
	StopsKnown bool    `json:"stopsKnown"`
	FuelToAdd  int     `json:"fuelToAdd"` // advice for the next stop, whole litres
	// Stint plan (D-125): from the session of the setup being driven, without choices during the race.
	Endurance    bool    `json:"endurance"`    // long stints, the last two equal; else equal stints
	SetupName    string  `json:"setupName"`    // the setup the plan is based on ("" = not recognised)
	StopsPlanned int     `json:"stopsPlanned"` // stops still needed: the mandatory ones, or more when the tank or stint limit requires it
	TankSize     float64 `json:"tankSize"`     // 0 = unknown
	MaxStintMs   int     `json:"maxStintMs"`   // the server's stint limit, 0 = none or unknown
	StintFuel    float64 `json:"stintFuel"`    // the most one stop can add (tank and stint limit), 0 = no limit
	PitMenuFuel  float64 `json:"pitMenuFuel"`
	PitMenuKnown bool    `json:"pitMenuKnown"`
	Reason       string  `json:"reason,omitempty"` // why there is no advice yet
}

// TyreAdvice is the tyre pressure part of the pit stop advice.
type TyreAdvice struct {
	Wet          bool       `json:"wet"`
	Target       float64    `json:"target"`
	Laps         int        `json:"laps"`
	Average      [4]float64 `json:"average"` // hot, psi, FL FR RL RR
	Change       [4]float64 `json:"change"`  // target − average
	PitMenu      [4]float64 `json:"pitMenu"` // as now set in the pit menu
	PitMenuKnown bool       `json:"pitMenuKnown"`
	Base         [4]float64 `json:"base"` // cold pressures of the tyres on the car (D-182)
	BaseKnown    bool       `json:"baseKnown"`
	Advice       [4]float64 `json:"advice"` // base + change: the pressures to set, not moved by the pit menu (D-182)
	Live         [4]float64 `json:"live"`   // hot pressures right now
	LiveKnown    bool       `json:"liveKnown"`
	Reason       string     `json:"reason,omitempty"`
}

// PitAdvice returns the advice for the session being recorded now.
func (l *Library) PitAdvice() PitAdvice {
	if l.db == nil {
		return PitAdvice{}
	}
	l.recMu.Lock()
	var id string
	var sessionID int64
	var car, track, kind string
	var setupID int64
	var maxStint int
	var cold coldTyres
	for g, r := range l.recs {
		if r != nil && r.sessionID != 0 {
			id, sessionID, car, track, kind, setupID, maxStint = g, r.sessionID, r.car, r.track, r.kind, r.setupID, r.maxStintMs
			cold = coldTyres{base: r.coldBase, known: r.coldKnown, since: r.coldSince}
			break
		}
	}
	l.recMu.Unlock()
	if sessionID == 0 {
		return PitAdvice{}
	}
	g := l.game(id)
	src, ok := g.(game.TelemetrySource)
	if !ok {
		return PitAdvice{}
	}
	t, running := src.Telemetry()
	if !running {
		return PitAdvice{}
	}
	laps, err := l.db.LapsForSession(sessionID)
	if err != nil {
		return PitAdvice{}
	}
	a := PitAdvice{Active: true, Game: id, Short: game.Short(g), Car: car, Track: track, Session: kind}
	a.CarName, a.TrackName = l.readableNames(id, car, track)
	cfg := l.Settings().GameOrDefault(id)
	dry, wet := cfg.TyreTargetDry, cfg.TyreTargetWet
	if dry == 0 {
		dry = defaultDryTarget
	}
	if wet == 0 {
		wet = defaultWetTarget
	}
	plan := stintPlan{maxStintMs: maxStint}
	if setupID != 0 {
		if s, err := l.db.Setup(setupID); err == nil && s != nil {
			plan.setupName, plan.endurance = s.Name, s.Session == naming.SessionEndu
		}
	}
	a.Fuel = fuelAdvice(laps, t, l.fuelMargin(id, car, track), plan)
	a.Tyres = tyreAdvice(laps, t, dry, wet, cold)
	return a
}

// fuelMargin is the safety margin the user set in the fuel calculator for this car and track.
func (l *Library) fuelMargin(gameID, car, track string) float64 {
	raw, _ := l.db.GetKV("fuel/" + gameID + "/" + strings.ToLower(car) + "/" + strings.ToLower(track))
	var saved struct {
		Race struct {
			Safety string `json:"safety"`
		} `json:"race"`
	}
	if raw != "" && json.Unmarshal([]byte(raw), &saved) == nil {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(saved.Race.Safety, ",", "."), 64); err == nil && v >= 0 && v < 50 {
			return v
		}
	}
	return defaultMargin
}

// stintPlan is how the fuel is spread over the stops (D-125). It follows the session of the setup
// being driven, so nothing has to be chosen during the race: a race setup gets equal stints, an
// Endurance setup the longest stints the tank and the server allow, with the last two equal.
type stintPlan struct {
	endurance  bool
	setupName  string
	maxStintMs int // 0 = no limit or unknown
}

func fuelAdvice(laps []store.Lap, t game.Telemetry, margin float64, plan stintPlan) *FuelAdvice {
	f := &FuelAdvice{FuelNow: t.Fuel, Margin: margin, StopsLeft: t.StopsLeft, StopsKnown: t.StopsKnown,
		PitMenuFuel: t.PitFuel, PitMenuKnown: t.PitMenuKnown, TimeLeftMs: t.TimeLeftMs,
		Endurance: plan.endurance, SetupName: plan.setupName, MaxStintMs: plan.maxStintMs}
	if t.TankKnown {
		f.TankSize = t.TankSize
	}
	var used []store.Lap
	for i := len(laps) - 1; i >= 0 && len(used) < fuelLaps; i-- {
		if l := laps[i]; l.Valid && !l.Pit && l.FuelUsed > 0 && l.LapMs > 0 {
			used = append(used, l)
		}
	}
	if len(used) == 0 {
		f.Reason = "Fuel advice appears after the first valid lap that is not an out-lap or in-lap."
		return f
	}
	var fuel, ms float64
	for _, l := range used {
		fuel += l.FuelUsed
		ms += float64(l.LapMs)
	}
	f.Laps, f.Firm = len(used), len(used) >= firmFuelLaps
	f.FuelPerLap, f.LapMs = fuel/float64(len(used)), int(ms/float64(len(used))+0.5)

	done := 0.0 // part of the current lap already driven
	if t.ProgressKnown {
		done = t.LapProgress
	}
	switch {
	case t.SessionLaps > 0: // the session ends after a number of laps
		f.LapsToGo = math.Max(0, float64(t.SessionLaps-t.CompletedLaps)-done)
	case t.TimeLeftKnown: // timed: every lap started before the clock reaches zero is finished
		f.Timed = true
		f.LapsToGo, f.RaceEnd = timedLapsToGo(t, done, float64(f.LapMs))
	default:
		f.Reason = "The session length is not known yet."
		return f
	}
	f.MinNeeded = f.LapsToGo * f.FuelPerLap
	f.FuelNeeded = f.MinNeeded + margin
	f.Spare = t.Fuel - f.MinNeeded
	extra := math.Max(0, f.FuelNeeded-t.Fuel)   // still to be added at the stops, with the margin
	minExtra := math.Max(0, f.MinNeeded-t.Fuel) // the same without it: the last stint needs no margin (D-178)

	// The most one stop can add: the tank (arriving with the margin left) and the stint limit
	// (whole laps that fit in one stint).
	limit := math.Inf(1)
	if f.TankSize > 0 {
		limit = math.Max(1, f.TankSize-margin)
	}
	if plan.maxStintMs > 0 {
		limit = math.Min(limit, math.Max(1, math.Floor(float64(plan.maxStintMs)/float64(f.LapMs)))*f.FuelPerLap)
	}
	if !math.IsInf(limit, 1) {
		f.StintFuel = limit
	}

	// Stops still needed: the mandatory ones, or more when one stop can't add enough.
	stops := 0
	if t.StopsKnown {
		stops = t.StopsLeft
	}
	if minExtra > 0 {
		stops = max(stops, 1, int(math.Ceil(minExtra/limit-1e-9)))
	}
	f.StopsPlanned = stops
	switch {
	case stops == 0:
		f.FuelToAdd = 0
	case stops == 1:
		// The last stop: just enough to reach the flag (owner, race 2026-11-23: "I really don't
		// need 2l of safety in the last stint"). The pit menu takes whole litres: rounded up.
		f.LastStop, f.MinToAdd = true, minExtra
		f.FuelToAdd = int(math.Ceil(minExtra - 1e-9))
	case plan.endurance && stops > 2:
		// Endurance: a full stint now; the last two stints share what is left equally.
		f.FuelToAdd = int(math.Floor(math.Min(limit, extra) + 1e-9))
	default:
		f.FuelToAdd = int(math.Ceil(extra/float64(stops) - 1e-9))
	}
	return f
}

// coldTyres: the cold pressures of the tyres on the car and since when (D-182).
type coldTyres struct {
	base  [4]float64
	known bool
	since time.Time
}

func tyreAdvice(laps []store.Lap, t game.Telemetry, dryTarget, wetTarget float64, cold coldTyres) *TyreAdvice {
	a := &TyreAdvice{PitMenu: t.PitPressures, PitMenuKnown: t.PitMenuKnown, Live: t.Pressures, LiveKnown: t.PressuresKnown,
		Base: cold.base, BaseKnown: cold.known}
	switch {
	case t.WetKnown:
		a.Wet = t.WetTyres
	case len(laps) > 0:
		a.Wet = laps[len(laps)-1].Wet
	}
	a.Target = dryTarget
	if a.Wet {
		a.Target = wetTarget
	}
	var used []store.Lap
	for i := len(laps) - 1; i >= 0 && len(used) < pressureLaps; i-- {
		if l := laps[i]; !l.Pit && l.HasPressures && l.Wet == a.Wet && (!cold.known || !l.At.Before(cold.since)) { // laps on these tyres
			used = append(used, l)
		}
	}
	if len(used) == 0 {
		a.Reason = "Tyre advice appears after the first lap on these tyres that is not an out-lap or in-lap."
		return a
	}
	a.Laps = len(used)
	for i := 0; i < 4; i++ {
		sum := 0.0
		for _, l := range used {
			sum += l.Pressures[i]
		}
		a.Average[i] = round1(sum / float64(len(used)))
		a.Change[i] = round1(a.Target - a.Average[i])
		if a.BaseKnown {
			a.Advice[i] = round1(a.Base[i] + a.Change[i])
		}
	}
	return a
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// timedLapsToGo is the number of laps still to drive in a session that ends on time. A lap
// started before the clock reaches zero is finished. In a race, the race ends when the overall
// leader crosses the line after the clock has run out, and every other car finishes at its next
// crossing, which can be one lap later than its own first crossing after zero (D-198). Without
// the leader's position SetupMan counts as if the driver leads.
func timedLapsToGo(t game.Telemetry, done, lapMs float64) (laps float64, raceEnd string) {
	own := 1 - done
	if t.TimeLeftMs > 0 {
		own = math.Ceil(done+t.TimeLeftMs/lapMs) - done
	}
	switch {
	case t.Session != "race":
		return own, ""
	case !t.LeaderKnown:
		return own, "unknown"
	case t.LeaderIsPlayer:
		return own, "you"
	case t.LeaderFinished:
		return 1 - done, "leader"
	}
	// The leader's finishing crossing, as time left on the clock (zero or below).
	tl := float64(t.LeaderLapMs)
	if tl <= 0 {
		tl = lapMs
	}
	tl = math.Min(1.5*lapMs, math.Max(0.5*lapMs, tl)) // leaders of another class lap faster or slower
	xl := t.TimeLeftMs - (1-t.LeaderProgress)*tl      // the leader's next crossing
	if xl > 0 {
		xl -= math.Ceil(xl/tl) * tl
	}
	// The driver finishes at the first own crossing after that.
	c1 := t.TimeLeftMs - (1-done)*lapMs // the driver's next crossing
	k := 1.0
	if c1 >= xl {
		k = math.Floor((c1-xl)/lapMs) + 2
	}
	return k - done, "leader"
}
