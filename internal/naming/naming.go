// Package naming reads information from setup file names (spec D-47, D-49, D-61).
//
// Example: "ABC R 1.10.4 2327 TC25" → initials ABC, session R, game version 1.10.4,
// air 23 °C and track 27 °C as ACC shows them (D-145), own code TC25. Everything is a guess the
// user can edit.
package naming

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Sessions.
const (
	SessionQ    = "Q"
	SessionR    = "R"
	SessionEndu = "Endurance"
)

// DefaultEnduranceMinutes: a stint longer than this counts as endurance (spec D-69;
// a per-game setting, provisional until the user has used SetupMan for a few races).
const DefaultEnduranceMinutes = 45

// Meta is what could be read from a name.
type Meta struct {
	Initials    string `json:"initials"`
	Session     string `json:"session"`     // as written in the name: "Q", "R", "Endurance" or ""
	RaceMinutes int    `json:"raceMinutes"` // e.g. 45 from "45m"; 0 = unknown
	Stops       int    `json:"stops"`       // e.g. 1 from "1 stp"; editable metadata (D-69)
	GameVersion string `json:"gameVersion"`
	// Air and Track: the temperatures as the game shows them, written as four digits (3040 = air
	// 30 °C, track 40 °C; also "30 40", "R3040", "3040R"; D-145). 0 = unknown.
	Air   int `json:"air"`
	Track int `json:"track"`
	// TempMin / TempMax: a range written with a dash ("23-27C"), or one temperature ("23C", both
	// the same) whose kind (air or track) the name doesn't say. 0 = unknown.
	TempMin int      `json:"tempMin"`
	TempMax int      `json:"tempMax"`
	LapTime string   `json:"lapTime"` // e.g. "1:42.9"
	Codes   []string `json:"codes"`   // own codes, e.g. "TC25"
}

var (
	reVersion  = regexp.MustCompile(`^v?(\d{1,2})\.(\d{1,2})(?:\.(\d{1,2}))?$`)
	reTempPair = regexp.MustCompile(`^(\d{1,2})-(\d{1,2})c?$`)
	reTemp4    = regexp.MustCompile(`^(\d{2})(\d{2})$`)
	reTemp4Ses = regexp.MustCompile(`^(\d{2})(\d{2})([qr])$`)    // 2123R: 21–23 °C, race
	reTempNum  = regexp.MustCompile(`^(\d{1,2})(?:c|°|°c)$`)     // 19c, 21°C
	reSesVar   = regexp.MustCompile(`^([qr])[a-z]$`)             // QF, RF, RS: a qualifying or race variant
	reDuration = regexp.MustCompile(`^(.*?)(\d{1,4})(m|min|h)$`) // up to 1440m (24h)
	reCopy     = regexp.MustCompile(`^\(\d+\)$`)
	reCode     = regexp.MustCompile(`^[A-Za-z]+\d+$`)          // TC25, tc046
	reSesTemp4 = regexp.MustCompile(`^([qrw])(\d{2})(\d{2})$`) // R1516: race, 15–16 °C; W2429: 24–29 °C
	reInitials = regexp.MustCompile(`^[A-Z]{2,6}$`)
	// a provider written with a dash in front: "-abesports"
	reDashInitials = regexp.MustCompile(`^-[A-Za-z][A-Za-z0-9]+$`)
	reStops        = regexp.MustCompile(`^(\d)(?:stp|stop|stops)$`)
	stopWords      = map[string]bool{"stp": true, "stop": true, "stops": true}
	sessionQ       = map[string]bool{"q": true, "quali": true, "qualy": true, "qualifying": true, "qual": true}
	sessionR       = map[string]bool{"r": true, "race": true}
	sessionEndu    = map[string]bool{"endu": true, "endurance": true, "enduro": true}
)

type token struct {
	text string // original text
	low  string
	used bool // recognised as a session / temperature / version / lap-time / copy marker
}

func tokenize(name string) []token {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || unicode.IsSpace(r) })
	out := make([]token, 0, len(parts))
	for _, p := range parts {
		out = append(out, token{text: p, low: strings.ToLower(p)})
	}
	return out
}

// Parse reads what it can from a setup name (without the .json extension).
func Parse(name string) Meta {
	var m Meta
	toks := tokenize(name)

	// Initials: a "-name" prefix (e.g. "-abesports_HUN") or a leading all-capitals token.
	if len(toks) > 0 {
		first := toks[0].text
		switch {
		case reDashInitials.MatchString(first): // one dash, then a letter: "---24h-R" is no provider
			m.Initials = first[1:]
		// A name that is only initials ("BGK", up to 4 letters) counts too; a longer single word
		// ("HOTLAP") or a word with a meaning ("WET") does not.
		case (len(toks) > 1 || len(first) <= 4) && reInitials.MatchString(first) && !sessionQ[toks[0].low] && !sessionR[toks[0].low] && !sessionEndu[toks[0].low] &&
			!stopWords[toks[0].low] && toks[0].low != "wet" && toks[0].low != "dry":
			m.Initials = first
		}
	}

	for i := range toks {
		t := &toks[i]
		low := t.low
		switch {
		case reCopy.MatchString(low): // "(1)" from Windows copies
			t.used = true
		case reSesTemp4.MatchString(low): // R1516 → race, air 15, track 16; W2429 → air 24, track 29 (the W stays in the name)
			g := reSesTemp4.FindStringSubmatch(low)
			if !m.HasTemp() && setAirTrack(&m, g[2], g[3]) {
				switch g[1] {
				case "q":
					m.Session, t.used = SessionQ, true
				case "r":
					if m.Session == "" {
						m.Session = SessionR
					}
					t.used = true
				default:
					t.low = g[1] // keep the letter for name matching and suggestions
				}
			}
		case sessionQ[strings.TrimRight(low, "0123456789")]: // q, quali, quali2, Q2
			m.Session, t.used = SessionQ, true
		case sessionR[strings.TrimRight(low, "0123456789")]: // r, race, race2
			if m.Session == "" {
				m.Session = SessionR
			}
			t.used = true
		case sessionEndu[low]:
			m.Session, t.used = SessionEndu, true
		case reTempPair.MatchString(low): // 23-27C: a range
			g := reTempPair.FindStringSubmatch(low)
			if !m.HasTemp() && setTemp(&m, g[1], g[2]) {
				t.used = true
			}
		case reVersion.MatchString(low):
			g := reVersion.FindStringSubmatch(low)
			mid, _ := strconv.Atoi(g[2])
			// 1.42.9 is a lap time (1:42.9); 1.10.4 / v1.9.2 is a game version.
			if !strings.HasPrefix(low, "v") && g[3] != "" && mid > 12 {
				m.LapTime = g[1] + ":" + g[2] + "." + g[3]
			} else {
				m.GameVersion = strings.TrimPrefix(low, "v")
			}
			t.used = true
		case reTemp4.MatchString(low): // 3040 → air 30, track 40
			g := reTemp4.FindStringSubmatch(low)
			if !m.HasTemp() && setAirTrack(&m, g[1], g[2]) {
				t.used = true
			}
		case reTemp4Ses.MatchString(low): // 2123R → air 21, track 23, race
			g := reTemp4Ses.FindStringSubmatch(low)
			if !m.HasTemp() && setAirTrack(&m, g[1], g[2]) {
				t.used = true
				if g[3] == "q" {
					m.Session = SessionQ
				} else if m.Session == "" {
					m.Session = SessionR
				}
			}
		case reSesVar.MatchString(low): // QF / RF / RS: the session; the letters stay in the name
			if low[0] == 'q' {
				m.Session = SessionQ
			} else if m.Session == "" {
				m.Session = SessionR
			}
		default:
			if g := reDuration.FindStringSubmatch(low); g != nil { // 25m, 45min, 2h, hungo25m
				n, _ := strconv.Atoi(g[2])
				if g[3] == "h" {
					n *= 60
				}
				if n >= 5 {
					m.RaceMinutes = n
					if m.Session == "" {
						m.Session = SessionR
					}
					if g[1] == "" {
						t.used = true
					} else {
						t.low = g[1] // keep the word part ("hungo") for name matching
					}
				}
			}
		}
	}

	// Two plain numbers in a row, air then track: "21 25", "20_25", "19c 21c".
	if !m.HasTemp() {
		for i := 0; i+1 < len(toks); i++ {
			a, okA := tempNum(toks[i].low)
			b, okB := tempNum(toks[i+1].low)
			if toks[i].used || toks[i+1].used || !okA || !okB {
				continue
			}
			if setAirTrack(&m, a, b) {
				toks[i].used, toks[i+1].used = true, true
				break
			}
		}
	}

	// One temperature on its own: "23C" → 23–23 (air or track is not said).
	if !m.HasTemp() {
		for i := range toks {
			if g := reTempNum.FindStringSubmatch(toks[i].low); g != nil && !toks[i].used && setTemp(&m, g[1], g[1]) {
				toks[i].used = true
				break
			}
		}
	}

	// Pit stops: "1stop", "2stops", "1 stp", "2 stops".
	for i, t := range toks {
		if g := reStops.FindStringSubmatch(t.low); g != nil {
			m.Stops, _ = strconv.Atoi(g[1])
		} else if stopWords[t.low] && i > 0 && isNumber(toks[i-1].low) && !toks[i-1].used {
			m.Stops, _ = strconv.Atoi(toks[i-1].low)
		}
	}

	for _, t := range toks {
		if !t.used && reCode.MatchString(t.text) && t.text != m.Initials && !reSesTemp4.MatchString(strings.ToLower(t.text)) && !sessionQ[strings.TrimRight(t.low, "0123456789")] && !sessionR[strings.TrimRight(t.low, "0123456789")] {
			m.Codes = append(m.Codes, t.text)
		}
	}
	if m.Codes == nil {
		m.Codes = []string{}
	}
	return m
}

// tempNum reads a number that may be a temperature: "21", "21c", "21°c".
func tempNum(low string) (string, bool) {
	if isNumber(low) {
		return low, true
	}
	if g := reTempNum.FindStringSubmatch(low); g != nil {
		return g[1], true
	}
	return "", false
}

// setAirTrack reads "30" "40" as air 30 °C and track 40 °C (D-145). Air 5–45 °C, track 5–65 °C,
// and the track not more than 5 °C colder than the air (so a year like "2014" is not a temperature).
func setAirTrack(m *Meta, a, b string) bool {
	air, ok1 := airTrack(a, b)
	if !ok1 {
		return false
	}
	m.Air, m.Track = air[0], air[1]
	return true
}

func airTrack(a, b string) ([2]int, bool) {
	air, err1 := strconv.Atoi(a)
	track, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || air < 5 || air > 45 || track < 5 || track > 65 || track < air-5 {
		return [2]int{}, false
	}
	return [2]int{air, track}, true
}

// HasTemp reports whether the name gives a temperature (air and track, or a range).
func (m Meta) HasTemp() bool { return m.Air > 0 || m.Track > 0 || m.TempMin > 0 }

// TempFor is the temperature a name gives for "air" or "track" (D-145): the air or track value,
// else the range (whose kind the name doesn't say). lo = hi for one temperature; ok = false
// when the name has none.
func (m Meta) TempFor(kind string) (lo, hi int, ok bool) {
	switch {
	case kind == "track" && m.Track > 0:
		return m.Track, m.Track, true
	case kind != "track" && m.Air > 0:
		return m.Air, m.Air, true
	case m.TempMin > 0:
		return m.TempMin, max(m.TempMin, m.TempMax), true
	}
	return 0, 0, false
}

func setTemp(m *Meta, a, b string) bool {
	lo, err1 := strconv.Atoi(a)
	hi, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || lo < 5 || hi > 50 || lo > hi {
		return false
	}
	m.TempMin, m.TempMax = lo, hi
	return true
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// GuessSession decides the session for a setup (spec D-47, D-67, D-69):
//   - "Q" or an explicit "endurance" in the name is kept;
//   - with a known race length, a stint (race length ÷ (stops + 1)) longer than
//     enduranceMinutes means Endurance, otherwise Race;
//   - otherwise whatever the name said (R or nothing).
//
// Brake pads are deliberately not used (D-68). A manual choice by the user always wins
// (handled by the caller).
func GuessSession(m Meta, enduranceMinutes int) string {
	if m.Session == SessionQ || m.Session == SessionEndu {
		return m.Session
	}
	if enduranceMinutes <= 0 {
		enduranceMinutes = DefaultEnduranceMinutes
	}
	if m.RaceMinutes > 0 {
		if float64(m.RaceMinutes)/float64(m.Stops+1) > float64(enduranceMinutes) {
			return SessionEndu
		}
		return SessionR
	}
	return m.Session
}

// SameName reports whether two names are the same apart from session, temperature,
// version and lap-time parts (spec D-61). Names with nothing else left ("quali", "race")
// never match.
func SameName(a, b string) bool {
	ca, cb := Core(a), Core(b)
	return len(ca) >= 2 && ca == cb
}

// Core is the name with session, temperature, version, lap-time, duration and copy parts
// removed, in lower case. Two setups with the same core count as "the same name" (D-61):
// "LA 296 HUN Q" and "LA 296 HUN R" both give "la 296 hun".
func Core(name string) string {
	toks := tokenize(name)
	var words []string
	for i := range toks {
		t := toks[i]
		low := t.low
		switch {
		case reCopy.MatchString(low),
			sessionQ[strings.TrimRight(low, "0123456789")],
			sessionR[strings.TrimRight(low, "0123456789")],
			sessionEndu[low],
			reVersion.MatchString(low):
			continue
		case reTempPair.MatchString(low):
			g := reTempPair.FindStringSubmatch(low)
			if setTemp(&Meta{}, g[1], g[2]) {
				continue
			}
		case reTemp4.MatchString(low):
			g := reTemp4.FindStringSubmatch(low)
			if _, ok := airTrack(g[1], g[2]); ok {
				continue
			}
		}
		if g := reDuration.FindStringSubmatch(low); g != nil {
			if g[1] == "" {
				continue
			}
			low = g[1]
		}
		words = append(words, low)
	}
	// Drop a trailing pair of plain numbers that form air and track temperature ("21 25").
	if n := len(words); n >= 2 && isNumber(words[n-2]) && isNumber(words[n-1]) && setAirTrack(&Meta{}, words[n-2], words[n-1]) {
		words = words[:n-2]
	}
	return strings.Join(words, " ")
}
