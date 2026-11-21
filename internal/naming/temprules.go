package naming

import (
	"regexp"
	"sort"
	"strings"
)

// Temperature labels per author (D-146). There is no standard: most names give air and track as
// four digits ("3040" = air 30 °C, track 40 °C, as ACC shows them), some give only the air
// temperature ("23C"), a few a range ("23-27C" or "2327"). How each author writes them is set
// per author: the initials at the front of the name, or any word in it (e.g. "CDA" in
// "R 2327 1.10.4 CDA M LFM"). Names that match no rule follow the default ("*").

// The ways of reading two numbers ("3040", "30 40") and one number ("23C").
const (
	FourAirTrack = "air-track" // air, then track (default)
	FourRange    = "range"     // from … to
	SingleAir    = "air"       // the air temperature (default)
	SingleTrack  = "track"     // the track temperature
	SingleEither = "either"    // not said: compared with air or track, whichever is asked
)

// TempRule says how one author writes temperatures in setup names.
type TempRule struct {
	Four   string `json:"four"`   // FourAirTrack or FourRange; "" = default
	Single string `json:"single"` // SingleAir, SingleTrack or SingleEither; "" = default
}

// TempRules holds the rule per author: initials or a word in upper case, "*" = everyone else.
type TempRules map[string]TempRule

// DefaultKey is the rule for names that match no author.
const DefaultKey = "*"

// Author is the rule key a name belongs to: its initials, else the first word of the name that
// has a rule of its own, else DefaultKey.
func (r TempRules) Author(name string, m Meta) string {
	if m.Initials != "" {
		return strings.ToUpper(m.Initials)
	}
	for _, t := range tokenize(name) {
		if k := strings.ToUpper(t.text); k != DefaultKey {
			if _, ok := r[k]; ok {
				return k
			}
		}
	}
	return DefaultKey
}

// Rule returns the rule for an author key, filled in with the defaults.
func (r TempRules) Rule(key string) TempRule {
	out := TempRule{Four: FourAirTrack, Single: SingleAir}
	for _, k := range []string{DefaultKey, strings.ToUpper(key)} {
		if x, ok := r[k]; ok {
			if x.Four != "" {
				out.Four = x.Four
			}
			if x.Single != "" {
				out.Single = x.Single
			}
		}
	}
	return out
}

// ParseWith reads a name like Parse, then reads its temperatures the way its author writes them.
func ParseWith(name string, rules TempRules) Meta {
	m := Parse(name)
	rule := rules.Rule(rules.Author(name, m))
	if m.Air > 0 && m.Track > 0 && rule.Four == FourRange {
		lo, hi := m.Air, m.Track
		m.Air, m.Track = 0, 0
		if lo <= hi {
			m.TempMin, m.TempMax = lo, hi
		}
	}
	if m.Air == 0 && m.Track == 0 && m.TempMin > 0 && m.TempMin == m.TempMax {
		switch rule.Single {
		case SingleAir:
			m.Air, m.TempMin, m.TempMax = m.TempMin, 0, 0
		case SingleTrack:
			m.Track, m.TempMin, m.TempMax = m.TempMin, 0, 0
		}
	}
	return m
}

// reWord: a word that can name an author ("CDA", "abesports", "FRI3"), not a number.
var reWord = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,19}$`)

// AuthorWords returns the words that occur in at least min of the names (counted once per name),
// most frequent first, leaving out what the naming standard reads itself (sessions, wet/dry,
// copies, codes such as TC25) and the given words (e.g. track names). For suggesting rules.
func AuthorWords(names []string, min int, skip map[string]bool) []WordCount {
	count := map[string]int{}
	spell := map[string]string{}
	for _, n := range names {
		seen := map[string]bool{}
		for _, t := range tokenize(n) {
			low := t.low
			if !reWord.MatchString(t.text) || seen[low] || skip[low] || reCode.MatchString(t.text) ||
				sessionQ[strings.TrimRight(low, "0123456789")] || sessionR[strings.TrimRight(low, "0123456789")] || sessionEndu[low] ||
				stopWords[low] || reSesVar.MatchString(low) || reDuration.MatchString(low) || reSesTemp4.MatchString(low) ||
				low == "wet" || low == "dry" || low == "copy" || low == "test" || low == "new" || low == "final" || low == "mod" || low == "old" {
				continue
			}
			seen[low] = true
			count[low]++
			if spell[low] == "" {
				spell[low] = t.text
			}
		}
	}
	out := []WordCount{}
	for w, c := range count {
		if c >= min {
			out = append(out, WordCount{Word: strings.ToUpper(spell[w]), Count: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
	return out
}

// WordCount is a word and in how many names it occurs.
type WordCount struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}
