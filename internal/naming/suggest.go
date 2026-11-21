package naming

import (
	"fmt"
	"strconv"
	"strings"
)

// Suggest builds a tidy name from a setup's details, in the order of the original feature
// list: <Provider/initials> <Session> <Weather> <Temperature> <GameVersion> <Other>.
// Example: initials ABC, race, dry, air 23 °C and track 27 °C, 1.10.4, code TC25 →
// "ABC R 2327 1.10.4 TC25" (D-145).
// wet is true when the setup runs wet tyres. Parts that are unknown are left out.
//
// original is the current name: its words SetupMan doesn't recognise (e.g. "hungo", "296 HUN")
// are kept at the end as <Other>, so renaming never loses information.
func Suggest(m Meta, session string, wet bool, original string) string {
	var parts []string
	if m.Initials != "" {
		parts = append(parts, m.Initials)
	}
	switch session {
	case SessionQ:
		parts = append(parts, "Q")
	case SessionR:
		parts = append(parts, "R")
	case SessionEndu:
		parts = append(parts, "ENDU")
	}
	if m.RaceMinutes > 0 && session != SessionQ {
		parts = append(parts, fmt.Sprintf("%dm", m.RaceMinutes))
	}
	if m.Stops > 0 {
		parts = append(parts, fmt.Sprintf("%dstop", m.Stops))
	}
	if wet {
		parts = append(parts, "WET")
	}
	switch {
	case m.Air > 0 && m.Track > 0:
		parts = append(parts, fmt.Sprintf("%02d%02d", m.Air, m.Track)) // air, track
	case m.Air > 0:
		parts = append(parts, fmt.Sprintf("%dC", m.Air)) // air only (read back by the author's rule, D-146)
	case m.Track > 0:
		parts = append(parts, fmt.Sprintf("%dC", m.Track))
	}
	if m.Air == 0 && m.Track == 0 && m.TempMin > 0 && m.TempMax > 0 {
		if m.TempMin == m.TempMax {
			parts = append(parts, fmt.Sprintf("%dC", m.TempMin)) // one temperature: 23C
		} else {
			parts = append(parts, fmt.Sprintf("%02d%02d", m.TempMin, m.TempMax))
		}
	}
	if m.GameVersion != "" {
		parts = append(parts, m.GameVersion)
	}
	if m.LapTime != "" {
		parts = append(parts, strings.ReplaceAll(m.LapTime, ":", "."))
	}
	parts = append(parts, m.Codes...)
	parts = append(parts, Rest(original, m)...)
	return strings.Join(parts, " ")
}

// Rest returns the words of a name that are not initials, session, temperature, version,
// lap time, duration, stops, codes or copy markers, in their original spelling.
func Rest(name string, m Meta) []string {
	toks := tokenize(name)
	codes := map[string]bool{}
	for _, c := range m.Codes {
		codes[c] = true
	}
	// The name's own temperature tokens are recognised as written, whatever the author's rule
	// makes of them (D-146): they are replaced by the temperature in m.
	tm := Parse(name)
	var out []string
	for i, t := range toks {
		low := t.low
		next := ""
		if i+1 < len(toks) {
			next = toks[i+1].low
		}
		switch {
		case i == 0 && m.Initials != "" && (t.text == m.Initials || t.text == "-"+m.Initials):
			continue
		case codes[t.text], reCopy.MatchString(low), stopWords[low], reStops.MatchString(low),
			sessionQ[strings.TrimRight(low, "0123456789")], sessionR[strings.TrimRight(low, "0123456789")], sessionEndu[low],
			reVersion.MatchString(low):
			continue
		case isNumber(low) && stopWords[next]: // "1" in "1 stp"
			continue
		case reTempPair.MatchString(low):
			if lo, hi := tempOf(low); lo == tm.TempMin && hi == tm.TempMax && lo > 0 {
				continue
			}
		case reTemp4.MatchString(low):
			if a, b := tempOf(low); a == tm.Air && b == tm.Track && a > 0 {
				continue
			}
		case tm.Air > 0 && isTempOf(low, tm) && pairAround(toks, i, tm):
			continue
		case reSesTemp4.MatchString(low):
			if g := reSesTemp4.FindStringSubmatch(low); g[2] == fmt.Sprintf("%02d", tm.Air) && g[3] == fmt.Sprintf("%02d", tm.Track) {
				if g[1] == "w" {
					out = append(out, t.text[:1]) // keep the W
				}
				continue
			}
		case tm.TempMin > 0 && tm.TempMin == tm.TempMax && reTempNum.MatchString(low) && isNum(low, tm.TempMin):
			continue
		case reTemp4Ses.MatchString(low):
			if g := reTemp4Ses.FindStringSubmatch(low); g[1] == fmt.Sprintf("%02d", tm.Air) && g[2] == fmt.Sprintf("%02d", tm.Track) {
				continue
			}
		}
		if g := reDuration.FindStringSubmatch(low); g != nil {
			if g[1] == "" {
				continue
			}
			out = append(out, t.text[:len(g[1])]) // "hungo" from "hungo25m"
			continue
		}
		out = append(out, t.text)
	}
	return out
}

// isNum reports whether a token ("23", "23c") is the number n.
func isNum(low string, n int) bool {
	d, ok := tempNum(low)
	return ok && d == strconv.Itoa(n)
}

// isTempOf reports whether a token is the air or track temperature ("21", "21c").
func isTempOf(low string, m Meta) bool {
	d, ok := tempNum(low)
	return ok && (d == strconv.Itoa(m.Air) || d == strconv.Itoa(m.Track))
}

func tempOf(low string) (int, int) {
	var g []string
	if g = reTempPair.FindStringSubmatch(low); g == nil {
		g = reTemp4.FindStringSubmatch(low)
	}
	if g == nil {
		return 0, 0
	}
	a, _ := strconv.Atoi(g[1])
	b, _ := strconv.Atoi(g[2])
	return a, b
}

// pairAround reports whether token i is part of a "21 25" air and track pair.
func pairAround(toks []token, i int, m Meta) bool {
	n := func(j int) int {
		if j < 0 || j >= len(toks) {
			return -1
		}
		d, ok := tempNum(toks[j].low)
		if !ok {
			return -1
		}
		v, _ := strconv.Atoi(d)
		return v
	}
	return (n(i) == m.Air && n(i+1) == m.Track) || (n(i-1) == m.Air && n(i) == m.Track)
}
