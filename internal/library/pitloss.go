package library

import (
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"setupman/internal/game"
	"setupman/internal/store"
	"setupman/internal/telemetry"
)

// PitLoss is the time a pit stop costs on a track, measured from the user's own recorded
// laps (D-173): the in-lap and out-lap together, minus as many normal laps of that session.
type PitLoss struct {
	Seconds float64   `json:"seconds"` // 0 = no stop recorded on this track yet
	Stops   int       `json:"stops"`   // number of stops the value comes from
	Class   string    `json:"class"`   // the class it was measured with ("" = any car)
	SameCar bool      `json:"sameCar"` // measured with this car only (game without classes)
	Last    time.Time `json:"last"`    // the most recent of those stops
	// The two parts (D-187), from the stops whose standing time the extra log shows: driving
	// through the pit lane (the same at every stop) and standing still (tyres, fuel, repairs,
	// brakes: it depends on the stop). 0 = not known.
	LaneSeconds  float64 `json:"laneSeconds"`
	StillSeconds float64 `json:"stillSeconds"`
	Timed        int     `json:"timed"` // stops with a known standing time
}

type pitStop struct {
	lossMs  int
	at      time.Time
	from    time.Time // start of the in-lap
	stillMs int       // standing still in the pit lane (from the extra log), 0 = unknown
}

// directGapMs: a lap follows the one before it without a break (no time in the garage)
// when the time between them is its own lap time, give or take a few seconds of polling.
const directGapMs = 5000

// pitStops finds the pit stops in one session's laps (oldest first). A stop is one or two
// laps through the pit lane that come straight after a lap on track and are followed
// straight away by a normal lap. Laps out of the garage (a break before them) start nothing,
// so leaving the pits at the start of a session isn't a stop. Valid or not doesn't matter
// for the pit laps (an invalid out-lap is common). The loss is measured against the
// session's own normal laps.
func pitStops(laps []store.Lap) []pitStop {
	ref := sessionRefMs(laps)
	if ref == 0 {
		return nil
	}
	direct := func(i int) bool {
		if i == 0 {
			return false
		}
		gap := laps[i].At.Sub(laps[i-1].At).Milliseconds() - int64(laps[i].LapMs)
		return gap > -directGapMs && gap < directGapMs
	}
	var out []pitStop
	for i := 0; i < len(laps); {
		if !laps[i].Pit || !direct(i) {
			i++
			continue
		}
		j := i
		for j+1 < len(laps) && laps[j+1].Pit && direct(j+1) {
			j++
		}
		if n := j - i + 1; n <= 2 && j+1 < len(laps) && direct(j+1) {
			sum := 0
			for k := i; k <= j; k++ {
				sum += laps[k].LapMs
			}
			if loss := sum - n*ref; loss > 0 && loss < 300000 {
				from := laps[i].At.Add(-time.Duration(laps[i].LapMs) * time.Millisecond)
				out = append(out, pitStop{lossMs: loss, at: laps[j].At, from: from})
			}
		}
		i = j + 1
	}
	return out
}

// sessionRefMs is the median of the session's normal laps (valid ones when there are at
// least two), 0 when there are too few to compare with.
func sessionRefMs(laps []store.Lap) int {
	var valid, all []int
	for _, l := range laps {
		if l.Pit || l.LapMs <= 0 {
			continue
		}
		all = append(all, l.LapMs)
		if l.Valid {
			valid = append(valid, l.LapMs)
		}
	}
	if len(valid) >= 2 {
		return medianInt(valid)
	}
	if len(all) >= 2 {
		return medianInt(all)
	}
	return 0
}

func medianInt(v []int) int {
	s := append([]int(nil), v...)
	sort.Ints(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// TrackPitLoss measures the pit stop time loss on a track for a car (D-173). The pit lane
// is the same for every car, so stops with any car count; stops with cars of the same class
// are used when there are any.
func (l *Library) TrackPitLoss(gameID, car, track string) (PitLoss, error) {
	if l.db == nil {
		return PitLoss{}, nil
	}
	laps, cars, err := l.db.LapsOnTrack(gameID, track)
	if err != nil {
		return PitLoss{}, err
	}
	bySession := map[int64][]store.Lap{}
	var order []int64
	for _, lp := range laps {
		if _, ok := bySession[lp.SessionID]; !ok {
			order = append(order, lp.SessionID)
		}
		bySession[lp.SessionID] = append(bySession[lp.SessionID], lp)
	}
	namer, _ := l.game(gameID).(game.Namer)
	classOf := func(c string) string {
		if namer == nil {
			return ""
		}
		_, _, cl := namer.CarInfo(c)
		return cl
	}
	myClass := classOf(car)
	var mine, all []pitStop
	for _, id := range order {
		st := pitStops(bySession[id])
		if len(st) > 0 {
			l.addStillTimes(gameID, id, st)
		}
		all = append(all, st...)
		c := cars[id]
		if (myClass != "" && classOf(c) == myClass) || (myClass == "" && strings.EqualFold(c, car)) {
			mine = append(mine, st...)
		}
	}
	use, out := all, PitLoss{}
	if len(mine) > 0 {
		use, out.Class, out.SameCar = mine, myClass, myClass == ""
	}
	if len(use) == 0 {
		return PitLoss{}, nil
	}
	ms := make([]int, len(use))
	for i, s := range use {
		ms[i] = s.lossMs
		if s.at.After(out.Last) {
			out.Last = s.at
		}
	}
	out.Seconds = float64(medianInt(ms)) / 1000
	out.Stops = len(use)
	var lane, still []int
	for _, s := range use {
		if s.stillMs > 0 && s.stillMs < s.lossMs {
			lane, still = append(lane, s.lossMs-s.stillMs), append(still, s.stillMs)
		}
	}
	if len(lane) > 0 {
		out.LaneSeconds = float64(medianInt(lane)) / 1000
		out.StillSeconds = float64(medianInt(still)) / 1000
		out.Timed = len(lane)
	}
	return out, nil
}

// stillCache keeps the standing times found in each extra log (read once per file version).
var stillCache = struct {
	sync.Mutex
	m map[string]stillEntry
}{m: map[string]stillEntry{}}

type stillEntry struct {
	mod   time.Time
	spans [][2]time.Time // standing still in the pit lane, from … to
}

// addStillTimes fills in how long the car stood still at each stop, from the session's extra log
// (D-187): rows in the pit lane where the car moved less than 2 cm since the row before, in spans of
// at least 2 s, between the start of the in-lap and the end of the out-lap. ACC's live data doesn't
// say what the pit menu planned (repairs, brakes), so the standing time is measured.
func (l *Library) addStillTimes(gameID string, sessionID int64, stops []pitStop) {
	if l.TelemetryDir == "" {
		return
	}
	spans := extraStillSpans(telemetry.ExtraPath(l.TelemetryDir, gameID, sessionID))
	for k := range stops {
		for _, sp := range spans {
			if !sp[0].Before(stops[k].from) && !sp[1].After(stops[k].at) {
				stops[k].stillMs += int(sp[1].Sub(sp[0]).Milliseconds())
			}
		}
	}
}

func extraStillSpans(path string) [][2]time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	stillCache.Lock()
	defer stillCache.Unlock()
	if e, ok := stillCache.m[path]; ok && e.mod.Equal(info.ModTime()) {
		return e.spans
	}
	x, err := telemetry.ReadExtra(path)
	if err != nil {
		return nil
	}
	spans := stillSpans(x)
	stillCache.m[path] = stillEntry{mod: info.ModTime(), spans: spans}
	return spans
}

// stillSpans finds where the car stood still in the pit lane in an extra log.
func stillSpans(x *telemetry.ExtraLog) [][2]time.Time {
	pit, px, pz := x.Index("PIT_LANE"), x.Index("POS_X"), x.Index("POS_Z")
	if pit < 0 || px < 0 || pz < 0 {
		return nil
	}
	at := func(i int) time.Time {
		return x.Header.Start.Add(time.Duration(float64(x.T[i]) * float64(time.Second)))
	}
	var out [][2]time.Time
	from := -1
	for i := 1; i <= len(x.T); i++ {
		still := false
		if i < len(x.T) {
			d := math.Hypot(float64(x.Data[px][i]-x.Data[px][i-1]), float64(x.Data[pz][i]-x.Data[pz][i-1]))
			still = x.Data[pit][i] == 1 && d < 0.02
		}
		switch {
		case still && from < 0:
			from = i
		case !still && from >= 0:
			if x.T[i-1]-x.T[from] >= 2 {
				out = append(out, [2]time.Time{at(from), at(i - 1)})
			}
			from = -1
		}
	}
	return out
}
