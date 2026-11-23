package library

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"setupman/internal/game"
	"setupman/internal/settings"
	histstore "setupman/internal/store"
)

// teleGame is a fake game that also reports live telemetry (for the pit stop advice).
type teleGame struct {
	*fakeGame
	tmu sync.Mutex
	t   game.Telemetry
}

func (g *teleGame) Telemetry() (game.Telemetry, bool) {
	g.tmu.Lock()
	defer g.tmu.Unlock()
	return g.t, true
}

func (g *teleGame) set(t game.Telemetry) {
	g.tmu.Lock()
	g.t = t
	g.tmu.Unlock()
}

// A simulated 60-minute race: an out-lap, then laps of 1:45 using 3.0 L each with hot pressures of
// about 26.5 psi at the front and 27.4 at the rear. The advice must follow the last laps.
func TestPitAdvice(t *testing.T) {
	folder := setupFolder(t)
	g := &teleGame{fakeGame: &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}}
	store := settings.NewStore(filepath.Join(t.TempDir(), "settings.json"))
	db, err := histstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l := New([]game.Game{g}, store, db)
	l.Debounce, l.LiveEvery = 50*time.Millisecond, time.Hour // laps are fed by hand below
	t.Cleanup(l.Stop)
	l.Apply()

	if a := l.PitAdvice(); a.Active {
		t.Fatalf("advice without a session: %+v", a)
	}

	timeLeft := 60 * 60 * 1000.0
	fuel := 60.0
	base := game.Telemetry{Live: true, Session: "race", Car: "ferrari_296_gt3", Track: "Spa", PitLaneKnown: true, ValidKnown: true,
		ValidLap: true, PressuresKnown: true, WetKnown: true, TimeLeftKnown: true, ProgressKnown: true, StopsKnown: true, StopsLeft: 1,
		PitMenuKnown: true, PitFuel: 40, PitPressures: [4]float64{26.0, 26.0, 25.5, 25.5}, TC: -1, ABS: -1, Map: -1}
	feed := func(laps int, last int, inPit bool, pressures [4]float64, progress float64) {
		tl := base
		tl.CompletedLaps, tl.LastLapMs, tl.InPit, tl.InPitLane = laps, last, inPit, inPit
		tl.Fuel, tl.TimeLeftMs, tl.LapProgress, tl.Pressures = fuel, timeLeft, progress, pressures
		g.set(tl)
		l.feedTelemetry(g, tl, true)
	}
	front, rear := [4]float64{26.4, 26.6, 27.3, 27.5}, [4]float64{26.6, 26.4, 27.5, 27.3}

	feed(0, 0, true, front, 0.9) // grid / pit box
	feed(0, 0, false, front, 0.1)
	for lap := 1; lap <= 6; lap++ {
		p := front
		if lap%2 == 0 {
			p = rear
		}
		feed(lap-1, 0, false, p, 0.5) // mid-lap sample
		fuel -= 3.0
		timeLeft -= 105000
		last := 105000
		if lap == 1 {
			last = 120000 // the first lap starts from the pits: an out-lap, not used
		}
		feed(lap, last, false, p, 0.0)
	}

	a := l.PitAdvice()
	if !a.Active || a.Fuel == nil || a.Tyres == nil {
		t.Fatalf("advice: %+v", a)
	}
	f := a.Fuel
	if f.Laps != 5 || !f.Firm || f.FuelPerLap < 2.99 || f.FuelPerLap > 3.01 || f.LapMs != 105000 {
		t.Errorf("fuel averages: %+v", f)
	}
	// 60 min − 6 × 1:45 = 49.5 min left = 28.29 laps → the lap now plus 28 more started before zero → 29 laps.
	if f.LapsToGo != 29 || !f.Timed {
		t.Errorf("laps to go = %v, want 29", f.LapsToGo)
	}
	// 29 × 3.0 = 87 L to the flag, 42 L in the tank: the last stop adds the minimum, 45 L, without
	// the 2 L margin (D-178).
	if f.FuelNeeded < 88.99 || f.FuelNeeded > 89.01 || f.FuelToAdd != 45 || !f.LastStop || f.MinToAdd < 44.99 || f.MinToAdd > 45.01 {
		t.Errorf("fuel needed %.2f, add %d (min %.2f, last %v); want 89, 45", f.FuelNeeded, f.FuelToAdd, f.MinToAdd, f.LastStop)
	}
	if f.Spare > -44.99 || f.Spare < -45.01 {
		t.Errorf("spare %.2f, want −45", f.Spare)
	}
	ty := a.Tyres
	// Last 3 laps: 6 (rear), 5 (front), 4 (rear) → FL (26.6+26.4+26.6)/3 = 26.5 → +0.5; RL 27.4 → −0.4.
	if ty.Laps != 3 || ty.Target != 27 || ty.Wet {
		t.Errorf("tyres: %+v", ty)
	}
	if ty.Average[0] != 26.5 || ty.Change[0] != 0.5 || ty.Advice[0] != 26.5 || ty.Change[2] != -0.4 || ty.Advice[2] != 25.1 {
		t.Errorf("pressures: avg %v change %v advice %v", ty.Average, ty.Change, ty.Advice)
	}

	// Two mandatory stops left: the fuel still needed is split over them.
	base.StopsLeft = 2
	feed(6, 105000, false, rear, 0.0)
	if f := l.PitAdvice().Fuel; f.FuelToAdd != 24 { // (89 − 42) / 2 = 23.5 → 24
		t.Errorf("with 2 stops: add %d, want 24", f.FuelToAdd)
	}

	// After the last stop, with more fuel than needed: no stop, 2.6 L to spare (D-178).
	base.StopsLeft = 0
	fuel = 89.6
	feed(6, 105000, false, rear, 0.0)
	if f := l.PitAdvice().Fuel; f.StopsPlanned != 0 || f.FuelToAdd != 0 || f.Spare < 2.59 || f.Spare > 2.61 {
		t.Errorf("after the last stop: %+v, want 0 stops and 2.6 L spare", f)
	}
	fuel = 42

	// On wet tyres the target is 30 psi, and dry laps are not used.
	base.WetTyres, base.StopsLeft = true, 1
	feed(6, 105000, false, rear, 0.0)
	if ty := l.PitAdvice().Tyres; !ty.Wet || ty.Target != 30 || ty.Laps != 0 || ty.Reason == "" {
		t.Errorf("wet: %+v", ty)
	}
}

// Stint plans (D-125): 3 hours left, 1:45 laps at 3.0 L, 60 L in the tank, a 120 L tank and a
// 65-minute stint limit. 103 laps to go → 309 L + 2 L margin = 311 L needed, 251 L to add.
// One stop can add at most 37 laps × 3.0 L = 111 L (65 min / 1:45 = 37 whole laps), so 3 stops.
func TestStintPlans(t *testing.T) {
	var laps []histstore.Lap
	for i := 0; i < 5; i++ {
		laps = append(laps, histstore.Lap{LapMs: 105000, Valid: true, FuelUsed: 3.0})
	}
	tl := game.Telemetry{Fuel: 60, TimeLeftKnown: true, TimeLeftMs: 3 * 3600 * 1000, ProgressKnown: true,
		StopsKnown: true, StopsLeft: 1, TankKnown: true, TankSize: 120}
	limit := 65 * 60 * 1000

	race := fuelAdvice(laps, tl, 2, stintPlan{maxStintMs: limit})
	if race.LapsToGo != 103 || race.StopsPlanned != 3 || race.StintFuel != 111 {
		t.Fatalf("race plan: %+v", race)
	}
	if race.FuelToAdd != 84 { // equal stints: 251 / 3 = 83.7 → 84
		t.Errorf("equal stints: add %d, want 84", race.FuelToAdd)
	}
	endu := fuelAdvice(laps, tl, 2, stintPlan{endurance: true, maxStintMs: limit})
	if endu.FuelToAdd != 111 { // a full stint first
		t.Errorf("endurance: add %d, want 111", endu.FuelToAdd)
	}
	// Endurance with two stops left: the last two stints are equal.
	tl.TimeLeftMs = 2 * 3600 * 1000 // 69 laps → 209 L needed, 149 L to add → 2 stops
	if f := fuelAdvice(laps, tl, 2, stintPlan{endurance: true, maxStintMs: limit}); f.StopsPlanned != 2 || f.FuelToAdd != 75 {
		t.Errorf("endurance, last two stints: %d stops, add %d; want 2, 75", f.StopsPlanned, f.FuelToAdd)
	}
	// No stint limit and no tank size known: the mandatory stops decide.
	tl.TankKnown, tl.StopsLeft = false, 2
	if f := fuelAdvice(laps, tl, 2, stintPlan{}); f.StopsPlanned != 2 || f.FuelToAdd != 75 || f.StintFuel != 0 {
		t.Errorf("no limits: %+v", f)
	}
	// Enough fuel in the tank and no mandatory stop: nothing to add.
	tl.Fuel, tl.StopsLeft = 300, 0
	if f := fuelAdvice(laps, tl, 2, stintPlan{}); f.StopsPlanned != 0 || f.FuelToAdd != 0 {
		t.Errorf("enough fuel: %+v", f)
	}
}
