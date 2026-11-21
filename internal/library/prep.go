package library

// Race prep (spec D-141): which of your setups to use for a session at a given temperature.
// The ranking only uses what SetupMan knows about your own setups, and says why:
//  1. the session the setup is for (Q, R, Endurance) matches;
//  2. the temperature in its name covers the expected temperature (or is close to it);
//  3. your recorded laps at a similar temperature (±3 °C) are fastest;
//  4. the temperature in its name is closest; then your best lap ever; then the newest setup.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"setupman/internal/game"
	"setupman/internal/naming"
	"setupman/internal/store"
)

// PrepInput is what the driver expects for the session.
type PrepInput struct {
	Game     string  `json:"game"`
	Car      string  `json:"car"`
	Track    string  `json:"track"`
	Session  string  `json:"session"`  // "Q", "R" or "Endurance"
	Temp     float64 `json:"temp"`     // expected temperature, °C
	HasTemp  bool    `json:"hasTemp"`  // false = no temperature given
	TempKind string  `json:"tempKind"` // "air" or "track": which temperature Temp is (compared with the air or track temperature in names, D-145)
}

// PrepSetup is one setup in the ranking.
type PrepSetup struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Session  string   `json:"session"`
	InFolder bool     `json:"inFolder"` // in the game's setups folder (shown in the game)
	TempMin  int      `json:"tempMin"`
	TempMax  int      `json:"tempMax"`
	Stops    int      `json:"stops"`       // mandatory stops from the name or notes (D-69)
	Minutes  int      `json:"raceMinutes"` // race length from the name
	Near     LapStats `json:"near"`        // laps at a similar temperature
	All      LapStats `json:"all"`         // all laps with this setup
	Reasons  []string `json:"reasons"`     // why it is ranked here, in plain words
	Top      bool     `json:"top"`         // the recommended setup
	sessFit  int
	tempFit  int
	tempDist float64 // °C outside the name's temperature range (0 = inside)
	modified int64
}

// nearTemp: laps within this many °C of the expected temperature count as "similar".
const nearTemp = 3.0

// Prep ranks the setups of a car and track for a session.
func (l *Library) Prep(in PrepInput) ([]PrepSetup, error) {
	out := []PrepSetup{}
	if l.db == nil {
		return out, ErrNoHistory
	}
	setups, err := l.db.SetupsForTrack(in.Game, in.Car, strings.ToLower(in.Track))
	if err != nil {
		return out, err
	}
	laps, err := l.db.LapsForTrack(in.Game, in.Car, strings.ToLower(in.Track))
	if err != nil {
		return out, err
	}
	bySetup := map[int64][]store.Lap{}
	for _, lp := range laps {
		bySetup[lp.SetupID] = append(bySetup[lp.SetupID], lp)
	}
	G := l.shortName(in.Game)
	for _, s := range setups {
		p := PrepSetup{ID: s.ID, Name: s.Name, Session: s.Session, InFolder: s.RelPath != "",
			TempMin: s.Meta.TempMin, TempMax: s.Meta.TempMax, Stops: s.Meta.Stops, Minutes: s.Meta.RaceMinutes}
		if v, _ := l.db.LatestVersion(s.ID); v != nil {
			p.modified = v.FileModified.Unix()
		}
		p.All = statsOf(bySetup[s.ID])
		var near []store.Lap
		for _, lp := range bySetup[s.ID] {
			t := lp.Air
			if in.TempKind == "track" {
				t = lp.Road
			}
			if in.HasTemp && lp.HasTemps && math.Abs(t-in.Temp) <= nearTemp {
				near = append(near, lp)
			}
		}
		p.Near = statsOf(near)

		// 1. Session.
		switch {
		case s.Session == in.Session && s.Session != "":
			p.sessFit = 2
			p.Reasons = append(p.Reasons, sessionWord(s.Session)+" setup")
		case s.Session == "":
			p.sessFit = 1
			p.Reasons = append(p.Reasons, "Session not known")
		default:
			p.Reasons = append(p.Reasons, sessionWord(s.Session)+" setup, not "+strings.ToLower(sessionWord(in.Session)))
		}
		// 2. Temperature in the name: air or track as the game shows them (3040 = air 30, track 40,
		// D-145), compared with the expected temperature of the same kind; or a range.
		loI, hiI, hasT := s.Meta.TempFor(in.TempKind)
		lo, hi := float64(loI), float64(hiI)
		switch {
		case !in.HasTemp:
			p.tempFit = 0
		case !hasT:
			p.tempFit = 4
			p.Reasons = append(p.Reasons, "No temperature in the name")
		default:
			d := 0.0
			if in.Temp < lo {
				d = lo - in.Temp
			} else if in.Temp > hi {
				d = in.Temp - hi
			}
			p.tempDist = d
			band := nameTemps(s.Meta, in.TempKind)
			switch {
			case d < 1: // names hold whole degrees
				p.tempFit = 0
				p.Reasons = append(p.Reasons, "Made for "+band)
			case d <= 2:
				p.tempFit = 1
				p.Reasons = append(p.Reasons, fmt.Sprintf("Made for %s (%.0f °C off)", band, d))
			case d <= 5:
				p.tempFit = 2
				p.Reasons = append(p.Reasons, fmt.Sprintf("Made for %s (%.0f °C off)", band, d))
			default:
				p.tempFit = 3
				p.Reasons = append(p.Reasons, fmt.Sprintf("Made for %s (%.0f °C off)", band, d))
			}
		}
		// 3./4. Your laps.
		if p.Near.AvgMs > 0 {
			p.Reasons = append(p.Reasons, fmt.Sprintf("Your average at %.0f–%.0f °C: %s (%d laps)", in.Temp-nearTemp, in.Temp+nearTemp, msText(p.Near.AvgMs), p.Near.Clean))
		} else if p.All.BestMs > 0 {
			p.Reasons = append(p.Reasons, "Best lap "+msText(p.All.BestMs)+", none at this temperature")
		} else {
			p.Reasons = append(p.Reasons, "No laps recorded")
		}
		if !p.InFolder {
			p.Reasons = append(p.Reasons, "Hidden from "+G)
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.sessFit != b.sessFit {
			return a.sessFit > b.sessFit
		}
		if a.tempFit != b.tempFit {
			return a.tempFit < b.tempFit
		}
		if x, y := lapOrMax(a.Near.AvgMs), lapOrMax(b.Near.AvgMs); x != y {
			return x < y
		}
		if a.tempDist != b.tempDist { // without laps to decide, the closer temperature first
			return a.tempDist < b.tempDist
		}
		if x, y := lapOrMax(a.All.BestMs), lapOrMax(b.All.BestMs); x != y {
			return x < y
		}
		return a.modified > b.modified
	})
	if len(out) > 0 {
		out[0].Top = true
	}
	return out, nil
}

func lapOrMax(ms int) int {
	if ms <= 0 {
		return math.MaxInt
	}
	return ms
}

func sessionWord(s string) string {
	switch s {
	case naming.SessionQ:
		return "Qualifying"
	case naming.SessionR:
		return "Race"
	case naming.SessionEndu:
		return "Endurance"
	}
	return "Session"
}

// shortName is a game's short name, e.g. "ACC".
func (l *Library) shortName(gameID string) string {
	if g := l.game(gameID); g != nil {
		return game.Short(g)
	}
	return "the game"
}

// nameTemps describes the temperature in a setup name for the reasons, e.g. "air 30 °C, track
// 40 °C" (the kind compared first), "23–27 °C" or "23 °C".
func nameTemps(m naming.Meta, kind string) string {
	air, track := fmt.Sprintf("air %d °C", m.Air), fmt.Sprintf("track %d °C", m.Track)
	switch {
	case m.Air > 0 && m.Track > 0 && kind == "track":
		return track + ", " + air
	case m.Air > 0 && m.Track > 0:
		return air + ", " + track
	case m.TempMin > 0 && m.TempMax > m.TempMin:
		return fmt.Sprintf("%d–%d °C", m.TempMin, m.TempMax)
	default:
		return fmt.Sprintf("%d °C", m.TempMin)
	}
}
