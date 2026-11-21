package library

// Temperature labels per author (D-146): the Settings overview of every author whose setup names
// carry a temperature, how SetupMan reads them now, and which reading the laps recorded with
// those setups fit. Nothing is changed here; the rules are saved with the settings.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"setupman/internal/naming"
	"setupman/internal/store"
)

// TempAuthor is one author (initials) whose setup names carry a temperature.
type TempAuthor struct {
	Key      string          `json:"key"`      // initials or a word in upper case; "*" = names matching no author
	Count    int             `json:"count"`    // setups with a temperature in the name
	Own      bool            `json:"own"`      // has a rule of its own (else the default)
	Rule     naming.TempRule `json:"rule"`     // the rule in use
	Examples []TempExample   `json:"examples"` // a few names and how they are read now
	Fit      TempFit         `json:"fit"`      // what recorded laps say
}

// TempExample is one setup name and how SetupMan reads its temperature now.
type TempExample struct {
	Name    string `json:"name"`
	Reading string `json:"reading"`
}

// TempFit counts setups of an author with recorded laps whose name temperature matches the
// measured temperatures (within 3 °C) in one reading.
type TempFit struct {
	AirTrack int `json:"airTrack"` // two numbers fit air and track
	Range    int `json:"range"`    // two numbers fit a range around the air or track temperature
	Air      int `json:"air"`      // one number fits the air temperature
	Track    int `json:"track"`    // one number fits the track temperature
}

// TempOverview is the Settings overview: the authors, and words that recur in the names that
// match no author (candidates for a rule of their own, e.g. "CDA").
type TempOverview struct {
	Authors []TempAuthor       `json:"authors"`
	Words   []naming.WordCount `json:"words"`
}

// TempAuthors lists every author with a temperature in their setup names, across all games, the
// driver's own initials first, then by number of setups.
func (l *Library) TempAuthors() (TempOverview, error) {
	res := TempOverview{Authors: []TempAuthor{}, Words: []naming.WordCount{}}
	out := res.Authors
	if l.db == nil {
		return res, ErrNoHistory
	}
	var loose []string        // names (with a temperature) matching no author
	skip := map[string]bool{} // track names: not author words
	st := l.Settings()
	byKey := map[string]*TempAuthor{}
	for _, g := range l.games {
		all, err := l.db.SetupsForGame(g.ID())
		if err != nil {
			return res, err
		}
		for _, s := range all {
			if !s.DeletedAt.IsZero() {
				continue
			}
			raw := naming.Parse(s.Name) // as written, before any rule
			if !raw.HasTemp() {
				continue
			}
			key := st.TempRules.Author(s.Name, raw)
			if key == naming.DefaultKey {
				loose = append(loose, s.Name)
				for _, w := range strings.FieldsFunc(strings.ToLower(s.Track), func(r rune) bool { return r == '_' || r == ' ' || r == '-' }) {
					skip[w] = true
					if len(w) > 3 {
						skip[w[:3]] = true // abbreviations such as SIL, WAT, HUN
					}
				}
			}
			a := byKey[key]
			if a == nil {
				_, own := st.TempRules[key]
				a = &TempAuthor{Key: key, Own: own && key != naming.DefaultKey, Rule: st.TempRules.Rule(key), Examples: []TempExample{}}
				byKey[key] = a
			}
			a.Count++
			addExample(a, s.Name, raw, naming.ParseWith(s.Name, st.TempRules))
			if laps, err := l.db.LapsForSetup(s.ID); err == nil {
				addFit(&a.Fit, raw, laps)
			}
		}
	}
	for _, a := range byKey {
		out = append(out, *a)
	}
	own := strings.ToUpper(st.Initials)
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Key == own) != (out[j].Key == own) {
			return out[i].Key == own
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	res.Authors = out
	if words := naming.AuthorWords(loose, 3, skip); len(words) > 12 {
		res.Words = words[:12]
	} else {
		res.Words = words
	}
	return res, nil
}

// addExample keeps up to two names with two numbers and two with one number, so both ways of
// writing an author uses are shown.
func addExample(a *TempAuthor, name string, raw, read naming.Meta) {
	single := raw.Air == 0 && raw.TempMin > 0 && raw.TempMin == raw.TempMax
	n := 0
	for _, e := range a.Examples {
		if isSingle(e.Name) == single {
			n++
		}
	}
	if n >= 2 {
		return
	}
	a.Examples = append(a.Examples, TempExample{Name: name, Reading: tempReading(read)})
}

func isSingle(name string) bool {
	m := naming.Parse(name)
	return m.Air == 0 && m.TempMin > 0 && m.TempMin == m.TempMax
}

// tempReading says how a name's temperature is read, e.g. "air 30 °C, track 40 °C".
func tempReading(m naming.Meta) string {
	switch {
	case m.Air > 0 && m.Track > 0:
		return fmt.Sprintf("air %d °C, track %d °C", m.Air, m.Track)
	case m.Air > 0:
		return fmt.Sprintf("air %d °C", m.Air)
	case m.Track > 0:
		return fmt.Sprintf("track %d °C", m.Track)
	case m.TempMin > 0 && m.TempMax > m.TempMin:
		return fmt.Sprintf("%d–%d °C", m.TempMin, m.TempMax)
	case m.TempMin > 0:
		return fmt.Sprintf("%d °C, air or track", m.TempMin)
	}
	return "no temperature"
}

// addFit compares a name's temperature (as written) with the average air and track temperature
// of the laps recorded with that setup.
func addFit(f *TempFit, raw naming.Meta, laps []store.Lap) {
	var air, road float64
	n := 0
	for _, lp := range laps {
		if lp.HasTemps {
			air, road, n = air+lp.Air, road+lp.Road, n+1
		}
	}
	if n == 0 {
		return
	}
	air, road = air/float64(n), road/float64(n)
	near := func(v int, t float64) bool { return math.Abs(float64(v)-t) <= 3 }
	inside := func(lo, hi int, t float64) bool { return float64(lo)-2 <= t && t <= float64(hi)+2 }
	switch {
	case raw.Air > 0 && raw.Track > 0:
		if near(raw.Air, air) && near(raw.Track, road) {
			f.AirTrack++
		} else if raw.Air <= raw.Track && (inside(raw.Air, raw.Track, air) || inside(raw.Air, raw.Track, road)) {
			f.Range++
		}
	case raw.TempMin > 0 && raw.TempMin == raw.TempMax:
		da, dt := math.Abs(float64(raw.TempMin)-air), math.Abs(float64(raw.TempMin)-road)
		if da <= 3 && da < dt {
			f.Air++
		} else if dt <= 3 && dt < da {
			f.Track++
		}
	}
}
