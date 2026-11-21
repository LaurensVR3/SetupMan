package library

import (
	"setupman/internal/store"
)

// LapStats sums up laps (spec D-44, D-48).
type LapStats struct {
	Laps       int     `json:"laps"`       // all recorded laps
	Clean      int     `json:"clean"`      // valid laps that are not out-laps, in-laps or stops
	BestMs     int     `json:"bestMs"`     // best valid lap ever (0 = none)
	AvgMs      int     `json:"avgMs"`      // average of clean laps (0 = none)
	FuelPerLap float64 `json:"fuelPerLap"` // average fuel of clean laps with a known fuel use
	FuelLaps   int     `json:"fuelLaps"`
	// Track temperature range of the clean laps (D-141), when the game reported it.
	RoadMin  float64 `json:"roadMin,omitempty"`
	RoadMax  float64 `json:"roadMax,omitempty"`
	HasTemps bool    `json:"hasTemps,omitempty"`
}

// VersionLaps is LapStats for one setup version.
type VersionLaps struct {
	VersionID int64 `json:"versionId"`
	Seq       int   `json:"seq"`
	LapStats
}

// LapPoint is one clean lap for the lap time vs track temperature graph (D-48).
type LapPoint struct {
	RoadTemp float64 `json:"roadTemp"`
	AirTemp  float64 `json:"airTemp"`
	LapMs    int     `json:"lapMs"`
	Seq      int     `json:"seq"` // setup version
}

// SetupLaps is the lap-times part of the setup page.
type SetupLaps struct {
	Overall  LapStats        `json:"overall"`
	Versions []VersionLaps   `json:"versions"` // newest version first
	Sessions []store.Session `json:"sessions"`
	Points   []LapPoint      `json:"points"`
}

func statsOf(laps []store.Lap) LapStats {
	var s LapStats
	var sum, fuel float64
	for _, l := range laps {
		s.Laps++
		if l.Valid && (s.BestMs == 0 || l.LapMs < s.BestMs) {
			s.BestMs = l.LapMs
		}
		if l.Valid && !l.Pit {
			s.Clean++
			sum += float64(l.LapMs)
			if l.HasTemps {
				if !s.HasTemps || l.Road < s.RoadMin {
					s.RoadMin = l.Road
				}
				if !s.HasTemps || l.Road > s.RoadMax {
					s.RoadMax = l.Road
				}
				s.HasTemps = true
			}
		}
		if !l.Pit && l.FuelUsed > 0 {
			s.FuelLaps++
			fuel += l.FuelUsed
		}
	}
	if s.Clean > 0 {
		s.AvgMs = int(sum/float64(s.Clean) + 0.5)
	}
	if s.FuelLaps > 0 {
		s.FuelPerLap = fuel / float64(s.FuelLaps)
	}
	return s
}

// SetupLapTimes returns the recorded laps of a setup, per version.
func (l *Library) SetupLapTimes(setupID int64) (SetupLaps, error) {
	out := SetupLaps{Versions: []VersionLaps{}, Sessions: []store.Session{}, Points: []LapPoint{}}
	if l.db == nil {
		return out, nil
	}
	laps, err := l.db.LapsForSetup(setupID)
	if err != nil {
		return out, err
	}
	versions, err := l.db.Versions(setupID)
	if err != nil {
		return out, err
	}
	seq := map[int64]int{}
	for _, v := range versions {
		seq[v.ID] = v.Seq
	}
	out.Overall = statsOf(laps)
	byVersion := map[int64][]store.Lap{}
	for _, lp := range laps {
		byVersion[lp.VersionID] = append(byVersion[lp.VersionID], lp)
		if lp.Valid && !lp.Pit && lp.HasTemps {
			out.Points = append(out.Points, LapPoint{RoadTemp: lp.Road, AirTemp: lp.Air, LapMs: lp.LapMs, Seq: seq[lp.VersionID]})
		}
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if vl := byVersion[versions[i].ID]; len(vl) > 0 {
			out.Versions = append(out.Versions, VersionLaps{VersionID: versions[i].ID, Seq: versions[i].Seq, LapStats: statsOf(vl)})
		}
	}
	out.Sessions, err = l.db.SessionsForSetup(setupID)
	return out, err
}

// TrackLapTimes sums up all laps on a car and track (fills in the fuel calculator).
func (l *Library) TrackLapTimes(gameID, car, track string) (LapStats, error) {
	if l.db == nil {
		return LapStats{}, nil
	}
	laps, err := l.db.LapsForTrack(gameID, car, track)
	if err != nil {
		return LapStats{}, err
	}
	return statsOf(laps), nil
}

// TrackSetupLapTimes sums up the laps of each setup on a car and track (lap times in Compare, D-141).
func (l *Library) TrackSetupLapTimes(gameID, car, track string) (map[int64]LapStats, error) {
	out := map[int64]LapStats{}
	if l.db == nil {
		return out, nil
	}
	laps, err := l.db.LapsForTrack(gameID, car, track)
	if err != nil {
		return out, err
	}
	by := map[int64][]store.Lap{}
	for _, lp := range laps {
		if lp.SetupID != 0 {
			by[lp.SetupID] = append(by[lp.SetupID], lp)
		}
	}
	for id, ls := range by {
		out[id] = statsOf(ls)
	}
	return out, nil
}

// UnassignedSessions lists recorded sessions on a car and track whose setup isn't known yet.
func (l *Library) UnassignedSessions(gameID, car, track string) ([]store.Session, error) {
	if l.db == nil {
		return []store.Session{}, nil
	}
	return l.db.UnassignedSessions(gameID, car, track)
}
