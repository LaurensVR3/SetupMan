package library

// Session report (spec D-127): everything recorded in one session (practice, qualifying, race) on
// one page: a summary, the stints between pit stops, and every lap.

import (
	"math"

	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/store"
)

// SessionReport is the session page.
type SessionReport struct {
	Session   store.Session  `json:"session"`
	Short     string         `json:"short"`
	CarName   string         `json:"carName"`
	TrackName string         `json:"trackName"`
	SetupName string         `json:"setupName"` // "" = setup not known
	Summary   SessionSummary `json:"summary"`
	Stints    []StintReport  `json:"stints"`
	Laps      []LapRow       `json:"laps"`
}

// SessionSummary sums up a whole session.
type SessionSummary struct {
	LapStats
	ConsistencyMs int        `json:"consistencyMs"` // standard deviation of the clean laps, 0 = fewer than 2
	DrivingMs     int        `json:"drivingMs"`     // all lap times added up
	FuelUsed      float64    `json:"fuelUsed"`      // all known fuel use added up
	RoadMin       float64    `json:"roadMin"`
	RoadMax       float64    `json:"roadMax"`
	AirMin        float64    `json:"airMin"`
	AirMax        float64    `json:"airMax"`
	TempsKnown    bool       `json:"tempsKnown"`
	Pressures     [4]float64 `json:"pressures"` // average hot pressure over laps that are not out-laps, in-laps or stops
	HasPressures  bool       `json:"hasPressures"`
}

// StintReport is one stint: the laps between two pit stops.
type StintReport struct {
	N            int        `json:"n"`
	FromLap      int        `json:"fromLap"`
	ToLap        int        `json:"toLap"`
	DrivingMs    int        `json:"drivingMs"`
	Wet          bool       `json:"wet"`
	Pressures    [4]float64 `json:"pressures"`
	HasPressures bool       `json:"hasPressures"`
	LapStats
}

// LapRow is one lap of the session.
type LapRow struct {
	N    int    `json:"n"`
	Kind string `json:"kind"` // "out" (out-lap), "in" (in-lap), "stop" (in and out in one lap) or ""
	Best bool   `json:"best"` // the best valid lap of the session
	Seq  int    `json:"seq"`  // setup version driven, 0 = unknown
	store.Lap
}

// SessionReport returns the report of one session (history.ErrNotFound when it doesn't exist).
func (l *Library) SessionReport(sessionID int64) (SessionReport, error) {
	if l.db == nil {
		return SessionReport{}, ErrNoHistory
	}
	s, err := l.db.Session(sessionID)
	if err != nil {
		return SessionReport{}, err
	}
	if s == nil {
		return SessionReport{}, history.ErrNotFound
	}
	laps, err := l.db.LapsForSession(sessionID)
	if err != nil {
		return SessionReport{}, err
	}
	r := SessionReport{Session: *s, Stints: []StintReport{}, Laps: []LapRow{}}
	r.Session.Laps = len(laps)
	if g := l.game(s.Game); g != nil {
		r.Short = game.Short(g)
	}
	r.CarName, r.TrackName = l.readableNames(s.Game, s.Car, s.Track)
	if s.SetupID != 0 {
		if st, err := l.db.Setup(s.SetupID); err == nil && st != nil {
			r.SetupName = st.Name
		}
	}
	seq := map[int64]int{}
	for _, lp := range laps {
		if _, ok := seq[lp.VersionID]; !ok && lp.VersionID != 0 {
			if v, err := l.db.Version(lp.VersionID); err == nil && v != nil {
				seq[lp.VersionID] = v.Seq
			}
		}
	}
	r.Laps = lapRows(laps, seq)
	r.Stints = stints(r.Laps)
	r.Summary = summarize(laps)
	return r, nil
}

// lapRows numbers the laps, names the pit laps and marks the best lap. Laps that touched the pit
// lane come in groups: on its own at the start it is the out-lap; in a group of two or more the
// first is the in-lap and the others are out-laps; on its own later it is a stop where the
// timing line lies in the pit lane (in and out in one lap).
func lapRows(laps []store.Lap, seq map[int64]int) []LapRow {
	rows := make([]LapRow, len(laps))
	best := -1
	for i, lp := range laps {
		rows[i] = LapRow{N: i + 1, Seq: seq[lp.VersionID], Lap: lp}
		if lp.Valid && (best < 0 || lp.LapMs < laps[best].LapMs) {
			best = i
		}
	}
	if best >= 0 {
		rows[best].Best = true
	}
	for i := 0; i < len(rows); {
		if !rows[i].Pit {
			i++
			continue
		}
		j := i
		for j < len(rows) && rows[j].Pit {
			j++
		}
		switch {
		case i == 0:
			for k := i; k < j; k++ {
				rows[k].Kind = "out"
			}
		case j-i == 1:
			rows[i].Kind = "stop"
		default:
			rows[i].Kind = "in"
			for k := i + 1; k < j; k++ {
				rows[k].Kind = "out"
			}
		}
		i = j
	}
	return rows
}

// stints splits the laps at the pit stops: an in-lap ends a stint, an out-lap starts one, and a
// stop lap ends the stint it belongs to.
func stints(rows []LapRow) []StintReport {
	var out []StintReport
	var cur []LapRow
	flush := func() {
		if len(cur) == 0 {
			return
		}
		laps := make([]store.Lap, len(cur))
		st := StintReport{N: len(out) + 1, FromLap: cur[0].N, ToLap: cur[len(cur)-1].N}
		var psum [4]float64
		pn := 0
		for i, r := range cur {
			laps[i] = r.Lap
			st.DrivingMs += r.LapMs
			if r.Wet {
				st.Wet = true
			}
			if !r.Pit && r.HasPressures {
				for k, v := range r.Pressures {
					psum[k] += v
				}
				pn++
			}
		}
		if pn > 0 {
			for k := range psum {
				st.Pressures[k] = math.Round(psum[k]/float64(pn)*10) / 10
			}
			st.HasPressures = true
		}
		st.LapStats = statsOf(laps)
		out = append(out, st)
		cur = nil
	}
	for _, r := range rows {
		if r.Kind == "out" && len(cur) > 0 && cur[len(cur)-1].Kind != "out" {
			flush()
		}
		cur = append(cur, r)
		if r.Kind == "in" || r.Kind == "stop" {
			flush()
		}
	}
	flush()
	if out == nil {
		out = []StintReport{}
	}
	return out
}

func summarize(laps []store.Lap) SessionSummary {
	s := SessionSummary{LapStats: statsOf(laps)}
	var clean []float64
	var psum [4]float64
	pn := 0
	for _, lp := range laps {
		s.DrivingMs += lp.LapMs
		s.FuelUsed += lp.FuelUsed
		if lp.Valid && !lp.Pit {
			clean = append(clean, float64(lp.LapMs))
		}
		if lp.HasTemps {
			if !s.TempsKnown {
				s.RoadMin, s.RoadMax, s.AirMin, s.AirMax, s.TempsKnown = lp.Road, lp.Road, lp.Air, lp.Air, true
			}
			s.RoadMin, s.RoadMax = math.Min(s.RoadMin, lp.Road), math.Max(s.RoadMax, lp.Road)
			s.AirMin, s.AirMax = math.Min(s.AirMin, lp.Air), math.Max(s.AirMax, lp.Air)
		}
		if !lp.Pit && lp.HasPressures {
			for k, v := range lp.Pressures {
				psum[k] += v
			}
			pn++
		}
	}
	if len(clean) >= 2 {
		mean := 0.0
		for _, v := range clean {
			mean += v
		}
		mean /= float64(len(clean))
		sq := 0.0
		for _, v := range clean {
			sq += (v - mean) * (v - mean)
		}
		s.ConsistencyMs = int(math.Sqrt(sq/float64(len(clean)-1)) + 0.5)
	}
	if pn > 0 {
		for k := range psum {
			s.Pressures[k] = math.Round(psum[k]/float64(pn)*10) / 10
		}
		s.HasPressures = true
	}
	return s
}
