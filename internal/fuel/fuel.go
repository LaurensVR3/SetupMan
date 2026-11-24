// Package fuel calculates how much fuel to take for a race or qualifying, with the
// rules agreed in the spec (D-54 … D-66). All amounts are whole litres, rounded up.
package fuel

import (
	"errors"
	"fmt"
	"math"
)

// Race is the input for a race.
type Race struct {
	Minutes    float64 `json:"minutes"`    // race length
	AvgLap     float64 `json:"avgLap"`     // average lap time, seconds
	FuelPerLap float64 `json:"fuelPerLap"` // litres
	Stops      int     `json:"stops"`      // mandatory pit stops (D-63)
	PitLoss    float64 `json:"pitLoss"`    // seconds lost per stop (D-56)
	Safety     float64 `json:"safety"`     // litres (D-55, default 2)
	Tank       float64 `json:"tank"`       // tank size in litres; 0 = unknown
	StintLaps  []int   `json:"stintLaps"`  // optional: laps of the first stints (D-63, older)
	// Optional pit window in race minutes (D-99): every stop must be made between these
	// minutes into the race. 0/0 = no window.
	WindowOpen  float64 `json:"windowOpen"`
	WindowClose float64 `json:"windowClose"`
}

// Qualifying is the input for qualifying (D-58, D-64, D-66: uses the best lap).
type Qualifying struct {
	Minutes    float64 `json:"minutes"`
	BestLap    float64 `json:"bestLap"` // seconds
	FuelPerLap float64 `json:"fuelPerLap"`
	Tank       float64 `json:"tank"`
}

// Stint is one stint of the plan.
type Stint struct {
	Laps      int     `json:"laps"`      // laps driven in this stint (race) or flying laps (qualifying)
	Fuel      float64 `json:"fuel"`      // litres to have (start) or to add (stop)
	FuelLabel string  `json:"fuelLabel"` // "Start with" / "Add at stop 1" / "Fill to"
}

// Plan is the result.
type Plan struct {
	Laps     int      `json:"laps"`     // total laps
	Stints   []Stint  `json:"stints"`   // in order
	Total    float64  `json:"total"`    // all fuel used, litres
	Steps    []string `json:"steps"`    // how it was calculated, in plain words
	Warnings []string `json:"warnings"` // e.g. more than the tank holds
}

func ceil(x float64) float64 { return math.Ceil(x - 1e-9) } // tolerate float noise (3.2*27 etc.)

// CalcRace plans a race (D-54, D-57, D-63, D-65, D-66).
func CalcRace(in Race) (Plan, error) {
	if in.Minutes <= 0 || in.AvgLap <= 0 || in.FuelPerLap <= 0 {
		return Plan{}, errors.New("enter the race length, average lap time and fuel per lap")
	}
	if in.Stops < 0 || in.PitLoss < 0 || in.Safety < 0 {
		return Plan{}, errors.New("stops, pit-stop time and safety margin can't be negative")
	}
	driving := in.Minutes*60 - float64(in.Stops)*in.PitLoss
	if driving <= 0 {
		return Plan{}, errors.New("the pit stops take longer than the race")
	}
	p := Plan{Laps: int(ceil(driving / in.AvgLap)), Steps: []string{}, Warnings: []string{}}
	p.Steps = append(p.Steps, fmt.Sprintf("Time driving: %s race − %d × %.0f s in the pits = %.0f s.",
		minutes(in.Minutes), in.Stops, in.PitLoss, driving))
	p.Steps = append(p.Steps, fmt.Sprintf("Laps: %.0f s ÷ %s = %.2f, rounded up to %d laps.", driving, lapText(in.AvgLap), driving/in.AvgLap, p.Laps))

	var stints []int
	var err error
	if in.Stops > 0 && (in.WindowOpen > 0 || in.WindowClose > 0) {
		var steps []string
		stints, steps, err = splitWindow(p.Laps, in.Stops, in.AvgLap, in.PitLoss, in.WindowOpen, in.WindowClose, in.Minutes)
		p.Steps = append(p.Steps, steps...)
	} else {
		stints, err = splitLaps(p.Laps, in.Stops+1, in.StintLaps)
	}
	if err != nil {
		return Plan{}, err
	}
	for i, laps := range stints {
		var st Stint
		if i == 0 {
			// Start fuel: first stint + the out-lap (formation lap), plus the safety margin.
			st = Stint{Laps: laps, Fuel: ceil(float64(laps+1)*in.FuelPerLap + in.Safety), FuelLabel: "Start with"}
			p.Steps = append(p.Steps, fmt.Sprintf("Start: (%d laps + 1 out-lap) × %.2f L + %.0f L safety = %.1f L, rounded up to %.0f L.",
				laps, in.FuelPerLap, in.Safety, float64(laps+1)*in.FuelPerLap+in.Safety, st.Fuel))
		} else {
			// At a stop: only this stint's laps; the safety margin is still in the tank.
			st = Stint{Laps: laps, Fuel: ceil(float64(laps) * in.FuelPerLap), FuelLabel: fmt.Sprintf("Add at stop %d", i)}
			p.Steps = append(p.Steps, fmt.Sprintf("Stop %d: %d laps × %.2f L = %.1f L, rounded up to %.0f L (the safety margin is still in the tank).",
				i, laps, in.FuelPerLap, float64(laps)*in.FuelPerLap, st.Fuel))
		}
		p.Total += st.Fuel
		p.Stints = append(p.Stints, st)
	}
	if in.Tank > 0 {
		if p.Stints[0].Fuel > in.Tank {
			p.Warnings = append(p.Warnings, fmt.Sprintf("The start fuel (%.0f L) is more than the tank holds (%.0f L). Add a stop or shorten the first stint.", p.Stints[0].Fuel, in.Tank))
		}
		for i, st := range p.Stints[1:] {
			if st.Fuel+in.Safety > in.Tank {
				p.Warnings = append(p.Warnings, fmt.Sprintf("Stint %d needs %.0f L plus the safety margin, more than the tank holds (%.0f L).", i+2, st.Fuel, in.Tank))
			}
		}
	}
	return p, nil
}

// splitLaps divides laps over stints in whole laps (D-65). Without a pit window the stints
// are as equal as possible, with extra laps in the later stints (D-66).
func splitLaps(laps, stints int, window []int) ([]int, error) {
	out := make([]int, stints)
	if len(window) > 0 {
		if len(window) != stints-1 {
			return nil, fmt.Errorf("the pit window needs the laps of the first %d stint(s)", stints-1)
		}
		used := 0
		for i, l := range window {
			if l <= 0 {
				return nil, errors.New("every stint in the pit window needs at least 1 lap")
			}
			out[i], used = l, used+l
		}
		if used >= laps {
			return nil, fmt.Errorf("the pit window uses %d of the %d laps, leaving none for the last stint", used, laps)
		}
		out[stints-1] = laps - used
		return out, nil
	}
	base, extra := laps/stints, laps%stints
	for i := range out {
		out[i] = base
		if i >= stints-extra {
			out[i]++
		}
	}
	return out, nil
}

// splitWindow divides laps so every stop falls inside the pit window (minutes into the race).
// Stops start from an even split; a stop that would be too early moves to the first lap
// after the window opens, one that would be too late to the last lap before it closes.
// The laps left are then spread evenly over the remaining stints.
func splitWindow(laps, stops int, avgLap, pitLoss, open, close, raceMin float64) ([]int, []string, error) {
	if close <= 0 {
		close = raceMin
	}
	if open < 0 || close <= open || close > raceMin {
		return nil, nil, errors.New("the pit window must open before it closes, within the race")
	}
	stints := stops + 1
	out := make([]int, 0, stints)
	steps := []string{fmt.Sprintf("Pit window: stops between %s and %s into the race.", minutes(open), minutes(close))}
	done := 0
	for k := 0; k < stops; k++ {
		left := laps - done
		lapsHere := splitEven(left, stints-k)[0]
		end := done + lapsHere                                                      // lap after which this stop is made
		at := func(l int) float64 { return float64(l)*avgLap + float64(k)*pitLoss } // seconds into the race
		if at(end) < open*60 {
			end = int(ceil((open*60 - float64(k)*pitLoss) / avgLap))
		}
		if at(end) > close*60 {
			end = int(math.Floor((close*60 - float64(k)*pitLoss) / avgLap))
		}
		if end <= done || end >= laps || at(end) < open*60-1e-6 {
			return nil, nil, errors.New("no lap ends inside the pit window; check the window and the lap time")
		}
		out = append(out, end-done)
		steps = append(steps, fmt.Sprintf("Stop %d after lap %d, about %s into the race.", k+1, end, minutes(at(end)/60)))
		done = end
	}
	out = append(out, laps-done)
	return out, steps, nil
}

func splitEven(laps, stints int) []int {
	out := make([]int, stints)
	base, extra := laps/stints, laps%stints
	for i := range out {
		out[i] = base
		if i >= stints-extra {
			out[i]++
		}
	}
	return out
}

// QualifyingSwitchMinutes: shorter sessions are one stint, longer ones two (agreed rule).
const QualifyingSwitchMinutes = 12

// CalcQualifying plans qualifying (D-58, D-64, D-66).
func CalcQualifying(in Qualifying) (Plan, error) {
	if in.Minutes <= 0 || in.BestLap <= 0 || in.FuelPerLap <= 0 {
		return Plan{}, errors.New("enter the session length, best lap time and fuel per lap")
	}
	p := Plan{Laps: int(ceil(in.Minutes * 60 / in.BestLap)), Steps: []string{}, Warnings: []string{}}
	p.Steps = append(p.Steps, fmt.Sprintf("Laps: %s ÷ %s = %.2f, rounded up to %d laps (out-laps included).",
		minutes(in.Minutes), lapText(in.BestLap), in.Minutes*60/in.BestLap, p.Laps))
	stints := 1
	if in.Minutes >= QualifyingSwitchMinutes {
		stints = 2
	}
	flying := p.Laps - stints // every stint starts with an out-lap; no in-lap (D-58)
	if flying < stints {
		return Plan{}, errors.New("the session is too short for a flying lap")
	}
	if stints == 1 {
		p.Steps = append(p.Steps, fmt.Sprintf("Shorter than %d minutes: one stint, 1 out-lap + %d flying laps.", QualifyingSwitchMinutes, flying))
	} else {
		p.Steps = append(p.Steps, fmt.Sprintf("%d minutes or longer: two stints, each with an out-lap; %d flying laps, the second stint gets the extra one.", QualifyingSwitchMinutes, flying))
	}
	first := flying / stints
	for i := 0; i < stints; i++ {
		laps := first
		if i == stints-1 {
			laps = flying - first*(stints-1)
		}
		f := ceil(float64(laps+1) * in.FuelPerLap)
		label := "Start with"
		if i > 0 {
			label = "Fill to"
		}
		p.Stints = append(p.Stints, Stint{Laps: laps, Fuel: f, FuelLabel: label})
		p.Steps = append(p.Steps, fmt.Sprintf("Stint %d: (1 out-lap + %d flying laps) × %.2f L = %.1f L, rounded up to %.0f L.",
			i+1, laps, in.FuelPerLap, float64(laps+1)*in.FuelPerLap, f))
		p.Total += f
		if in.Tank > 0 && f > in.Tank {
			p.Warnings = append(p.Warnings, fmt.Sprintf("Stint %d needs %.0f L, more than the tank holds (%.0f L).", i+1, f, in.Tank))
		}
	}
	return p, nil
}

func minutes(m float64) string {
	if m == math.Trunc(m) {
		return fmt.Sprintf("%.0f min", m)
	}
	return fmt.Sprintf("%.1f min", m)
}

// lapText shows seconds as a lap time, e.g. 105 → "1:45.0".
func lapText(sec float64) string {
	m := int(sec) / 60
	return fmt.Sprintf("%d:%04.1f", m, sec-float64(m*60))
}
