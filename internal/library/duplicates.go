package library

// Removing duplicates (spec D-106): sets of setups that are identical in every scored setting
// ("exact"), or at least the near-duplicate threshold alike ("near"), per car and track. The
// user keeps one setup per set; the others go to the bin (D-102).

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"setupman/internal/naming"
)

// DupSetup is one setup in a duplicate set.
type DupSetup struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
	Session  string    `json:"session"`
}

// DupSet is a set of duplicates on one car and track.
type DupSet struct {
	Car       string     `json:"car"`
	Track     string     `json:"track"`
	CarName   string     `json:"carName"`
	TrackName string     `json:"trackName"`
	Identical bool       `json:"identical"` // every pair identical in all scored settings
	MinScore  float64    `json:"minScore"`  // lowest similarity inside the set
	Setups    []DupSetup `json:"setups"`    // newest first
	Keep      int64      `json:"keep"`      // proposed setup to keep: see proposeKeep
}

// Duplicates lists duplicate sets for one car and track, or for every track of a game when car
// is "". near=false only counts setups identical in every scored setting.
func (l *Library) Duplicates(gameID, car, track string, near bool) []DupSet {
	type key struct{ car, track string }
	var keys []key
	l.mu.RLock()
	if st := l.states[gameID]; st != nil && st.scan != nil {
		for _, c := range st.scan.Cars {
			for _, t := range c.Tracks {
				if car == "" || (strings.EqualFold(c.ID, car) && t.ID == strings.ToLower(track)) {
					keys = append(keys, key{c.ID, t.ID})
				}
			}
		}
	}
	threshold := l.settings.Threshold
	l.mu.RUnlock()

	out := []DupSet{}
	for _, k := range keys {
		td, ok := l.Track(gameID, k.car, k.track)
		if !ok || len(td.Setups) < 2 {
			continue
		}
		// Union-find over the pairs that count as duplicates.
		parent := make([]int, len(td.Setups))
		for i := range parent {
			parent[i] = i
		}
		var find func(int) int
		find = func(i int) int {
			for parent[i] != i {
				parent[i] = parent[parent[i]]
				i = parent[i]
			}
			return i
		}
		for _, p := range td.Pairs {
			if p.Identical || (near && p.Score >= threshold) {
				parent[find(p.A)] = find(p.B)
			}
		}
		groups := map[int][]int{}
		for i := range td.Setups {
			groups[find(i)] = append(groups[find(i)], i)
		}
		for _, members := range groups {
			if len(members) < 2 {
				continue
			}
			set := DupSet{Car: k.car, Track: k.track, CarName: td.Car.Name, TrackName: td.Track.Name, Identical: true, MinScore: 100}
			in := map[int]bool{}
			for _, m := range members {
				in[m] = true
				if td.SetupIDs[m] != 0 {
					s := td.Setups[m]
					set.Setups = append(set.Setups, DupSetup{ID: td.SetupIDs[m], Name: s.Name, Modified: s.Modified, Session: td.Sessions[m]})
				}
			}
			for _, p := range td.Pairs {
				if in[p.A] && in[p.B] {
					set.Identical = set.Identical && p.Identical
					set.MinScore = min(set.MinScore, p.Score)
				}
			}
			if len(set.Setups) < 2 {
				continue
			}
			sort.Slice(set.Setups, func(i, j int) bool { return set.Setups[i].Modified.After(set.Setups[j].Modified) })
			set.Keep = proposeKeep(set.Setups)
			out = append(out, set)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CarName != out[j].CarName {
			return out[i].CarName < out[j].CarName
		}
		return out[i].TrackName < out[j].TrackName
	})
	return out
}

// reCopyMark finds the marks a copy gets: "quali (2)", "quali(1)", "quali - Copy", "quali copy 2".
var reCopyMark = regexp.MustCompile(`(?i)(\s*\(\d+\)|[\s_-]+copy(\s*\(?\d+\)?)?)$`)

// proposeKeep picks the setup to keep in a duplicate set: the one whose name has the fewest copy
// marks such as "(2)" ("BGK (6)" over "BGK (6) (1)", D-112); among those, the one whose name says
// the most in the naming standard (initials, session, temperature, version: "ABC R 1.10.3 3040"
// over "1.10.4 test", rig test R-10); then the newest. setups is sorted newest first.
func proposeKeep(setups []DupSetup) int64 {
	best, bestMarks, bestParts := setups[0].ID, -1, -1
	for _, s := range setups {
		m, p := copyMarks(s.Name), standardParts(s.Name)
		if bestMarks < 0 || m < bestMarks || (m == bestMarks && p > bestParts) {
			best, bestMarks, bestParts = s.ID, m, p
		}
	}
	return best
}

// standardParts counts the parts of the naming standard a name has (D-49).
func standardParts(name string) int {
	m := naming.Parse(name)
	n := 0
	for _, ok := range []bool{m.Initials != "", m.Session != "", m.HasTemp(), m.GameVersion != ""} {
		if ok {
			n++
		}
	}
	return n
}

// copyMarks counts the copy marks at the end of a name.
func copyMarks(name string) int {
	n := 0
	for name = strings.TrimSpace(name); reCopyMark.MatchString(name); n++ {
		name = strings.TrimSpace(reCopyMark.ReplaceAllString(name, ""))
	}
	return n
}
