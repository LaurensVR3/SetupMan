package fuel

import (
	"reflect"
	"testing"
)

// The examples agreed with the user in the spec are the test cases.

func TestRaceNoStop(t *testing.T) { // D-54: 45 min, 1:45.0, 3.2 L/lap, 2 L safety → 89 L
	p, err := CalcRace(Race{Minutes: 45, AvgLap: 105, FuelPerLap: 3.2, Safety: 2})
	if err != nil {
		t.Fatal(err)
	}
	if p.Laps != 26 || len(p.Stints) != 1 || p.Stints[0].Fuel != 89 {
		t.Errorf("got %d laps, stints %+v", p.Laps, p.Stints)
	}
}

func TestRaceOneStopWithPitLoss(t *testing.T) { // D-63: 45 min, 1 stop, 25 s, 1:45.0 → 26 laps
	p, err := CalcRace(Race{Minutes: 45, AvgLap: 105, FuelPerLap: 3.2, Stops: 1, PitLoss: 25, Safety: 2})
	if err != nil {
		t.Fatal(err)
	}
	if p.Laps != 26 {
		t.Errorf("laps = %d, want 26", p.Laps)
	}
}

func TestRaceSplit(t *testing.T) { // D-66: 60 min, 1 stop, 25 s, 1:45.0, 3.2 L → 35 laps = 17 + 18; 60 L, +58 L
	p, err := CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3.2, Stops: 1, PitLoss: 25, Safety: 2})
	if err != nil {
		t.Fatal(err)
	}
	if p.Laps != 35 || p.Stints[0].Laps != 17 || p.Stints[1].Laps != 18 {
		t.Fatalf("laps %d, stints %+v", p.Laps, p.Stints)
	}
	if p.Stints[0].Fuel != 60 || p.Stints[1].Fuel != 58 || p.Total != 118 {
		t.Errorf("fuel %v + %v = %v", p.Stints[0].Fuel, p.Stints[1].Fuel, p.Total)
	}
}

func TestRacePitWindowAndTank(t *testing.T) {
	p, err := CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3.2, Stops: 1, PitLoss: 25, Safety: 2, StintLaps: []int{20}, Tank: 60})
	if err != nil {
		t.Fatal(err)
	}
	if p.Stints[0].Laps != 20 || p.Stints[1].Laps != 15 {
		t.Errorf("pit window ignored: %+v", p.Stints)
	}
	if len(p.Warnings) == 0 {
		t.Error("expected a tank warning: 21 laps × 3.2 + 2 > 60 L")
	}
	if _, err := CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3.2, Stops: 1, StintLaps: []int{40}}); err == nil {
		t.Error("a pit window longer than the race was accepted")
	}
}

func TestSplitEvenExtraLater(t *testing.T) {
	got, _ := splitLaps(35, 3, nil)
	if !reflect.DeepEqual(got, []int{11, 12, 12}) {
		t.Errorf("got %v", got)
	}
}

func TestQualifying(t *testing.T) { // D-64: 15 min, 1:44 → 9 laps → out+3 (13 L), out+4 (16 L)
	p, err := CalcQualifying(Qualifying{Minutes: 15, BestLap: 104, FuelPerLap: 3.2})
	if err != nil {
		t.Fatal(err)
	}
	if p.Laps != 9 || len(p.Stints) != 2 {
		t.Fatalf("laps %d, stints %+v", p.Laps, p.Stints)
	}
	if p.Stints[0].Laps != 3 || p.Stints[0].Fuel != 13 || p.Stints[1].Laps != 4 || p.Stints[1].Fuel != 16 {
		t.Errorf("stints %+v", p.Stints)
	}
	short, _ := CalcQualifying(Qualifying{Minutes: 10, BestLap: 104, FuelPerLap: 3.2})
	if len(short.Stints) != 1 || short.Laps != 6 || short.Stints[0].Fuel != 20 { // 6 laps × 3.2 = 19.2 → 20
		t.Errorf("short qualifying: %+v", short)
	}
}

func TestBadInput(t *testing.T) {
	if _, err := CalcRace(Race{}); err == nil {
		t.Error("empty race accepted")
	}
	if _, err := CalcRace(Race{Minutes: 1, AvgLap: 100, FuelPerLap: 3, Stops: 3, PitLoss: 30}); err == nil {
		t.Error("pit stops longer than the race accepted")
	}
	if _, err := CalcQualifying(Qualifying{Minutes: 1, BestLap: 100, FuelPerLap: 3}); err == nil {
		t.Error("qualifying without a flying lap accepted")
	}
}

func TestPitWindowMinutes(t *testing.T) {
	// 60 min, 1:45 laps, 1 stop of 60 s: 34 laps. An even split stops after lap 17 (29.75 min),
	// before a 30–40 min window opens, so the stop moves to lap 18 (31.5 min).
	p, err := CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3, Stops: 1, PitLoss: 60, Safety: 2, WindowOpen: 30, WindowClose: 40})
	if err != nil {
		t.Fatal(err)
	}
	if p.Laps != 34 || p.Stints[0].Laps != 18 || p.Stints[1].Laps != 16 {
		t.Errorf("stints %+v", p.Stints)
	}
	// Too late: window 10–20 min → last lap ending before 20 min is lap 11 (19.25 min).
	p, _ = CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3, Stops: 1, PitLoss: 60, WindowOpen: 10, WindowClose: 20})
	if p.Stints[0].Laps != 11 {
		t.Errorf("late window: %+v", p.Stints)
	}
	// Two stops in one window 20–40.
	p, err = CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3, Stops: 2, PitLoss: 60, WindowOpen: 20, WindowClose: 40})
	if err != nil || len(p.Stints) != 3 {
		t.Fatalf("two stops: %+v %v", p.Stints, err)
	}
	if _, err := CalcRace(Race{Minutes: 60, AvgLap: 105, FuelPerLap: 3, Stops: 1, WindowOpen: 40, WindowClose: 30}); err == nil {
		t.Error("upside-down window accepted")
	}
}
