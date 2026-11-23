package library

import (
	"sort"
	"strings"
	"time"

	"setupman/internal/game"
	"setupman/internal/store"
)

// PitLoss is the time a pit stop costs on a track, measured from the user's own recorded
// laps (D-173): the in-lap and out-lap together, minus as many normal laps of that session.
type PitLoss struct {
	Seconds float64   `json:"seconds"` // 0 = no stop recorded on this track yet
	Stops   int       `json:"stops"`   // number of stops the value comes from
	Class   string    `json:"class"`   // the class it was measured with ("" = any car)
	SameCar bool      `json:"sameCar"` // measured with this car only (game without classes)
	Last    time.Time `json:"last"`    // the most recent of those stops
}

type pitStop struct {
	lossMs int
	at     time.Time
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
				out = append(out, pitStop{lossMs: loss, at: laps[j].At})
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
	return out, nil
}
