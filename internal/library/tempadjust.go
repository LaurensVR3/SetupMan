package library

// Adjusting a setup for temperature (original feature list; Phase 6). No physics rules are
// invented: SetupMan learns from the user's own setups. Variants of one setup that carry a
// temperature in their name (e.g. "hungo25m 2028" and "hungo25m 2530": air and track, D-145) show
// how the user changes tyre pressures and brake ducts per °C of track temperature. Proposals are
// shown with their basis and only saved when the user chooses to (as a new setup).

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"setupman/internal/compare"
	"setupman/internal/history"
	"setupman/internal/naming"
	"setupman/internal/setup"
)

// tempFields are the settings a temperature change usually affects (all whole clicks).
var tempFields = []string{
	"basicSetup.tyres.tyrePressure[0]", "basicSetup.tyres.tyrePressure[1]",
	"basicSetup.tyres.tyrePressure[2]", "basicSetup.tyres.tyrePressure[3]",
	"advancedSetup.aeroBalance.brakeDuct[0]", "advancedSetup.aeroBalance.brakeDuct[1]",
}

// TempRule is what was learned for one setting.
type TempRule struct {
	Path            string  `json:"path"`
	Label           string  `json:"label"`
	ClicksPerDegree float64 `json:"clicksPerDegree"` // change per +1 °C (negative = lower when warmer)
	Pairs           int     `json:"pairs"`           // how many pairs of your setups this is based on
}

// TempChange is one proposed change.
type TempChange struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	From  int    `json:"from"`
	To    int    `json:"to"`
}

// TempProposal is the answer for one setup and target temperature.
type TempProposal struct {
	From    string            `json:"from"` // e.g. "air 23 °C, track 27 °C"
	To      string            `json:"to"`
	Changes []TempChange      `json:"changes"`
	Rules   []TempRule        `json:"rules"`
	Pairs   int               `json:"pairs"`   // pairs of your setups the rules come from
	Edits   map[string]string `json:"edits"`   // ready for EditSetup
	NewName string            `json:"newName"` // suggested name for the adjusted setup
	Message string            `json:"message,omitempty"`
}

type tempSample struct {
	track  string
	name   string
	mid    float64
	fields map[string]float64
	all    []setup.Field
}

// learnTempRules looks at the user's setups of one car (all tracks).
func (l *Library) learnTempRules(gameID, car string) ([]TempRule, int, error) {
	g := l.game(gameID)
	if g == nil || l.db == nil {
		return nil, 0, errors.New("not available")
	}
	all, err := l.db.SetupsForGame(gameID)
	if err != nil {
		return nil, 0, err
	}
	var samples []tempSample
	for _, s := range all {
		tt, ok := trackTemp(s.Meta)
		if !strings.EqualFold(s.Car, car) || !ok {
			continue
		}
		v, _ := l.db.LatestVersion(s.ID)
		if v == nil {
			continue
		}
		f, err := g.ParseSetup(v.Content)
		if err != nil {
			continue
		}
		m := map[string]float64{}
		for _, x := range f {
			m[x.Path] = x.Num
		}
		samples = append(samples, tempSample{track: s.Track, name: s.Name, mid: tt, fields: m, all: f})
	}
	slopes := map[string][]float64{}
	pairs := 0
	for i := range samples {
		for j := i + 1; j < len(samples); j++ {
			a, b := samples[i], samples[j]
			if !strings.EqualFold(a.track, b.track) || math.Abs(a.mid-b.mid) < 2 {
				continue
			}
			// Only variants of one setup: very alike apart from what temperature changes.
			if compare.Compare(a.all, b.all).Score < 70 && !naming.SameName(a.name, b.name) {
				continue
			}
			pairs++
			for _, p := range tempFields {
				va, okA := a.fields[p]
				vb, okB := b.fields[p]
				if okA && okB {
					slopes[p] = append(slopes[p], (vb-va)/(b.mid-a.mid))
				}
			}
		}
	}
	var rules []TempRule
	for _, p := range tempFields {
		s := slopes[p]
		if len(s) == 0 {
			continue
		}
		sort.Float64s(s)
		med := s[len(s)/2]
		if len(s)%2 == 0 {
			med = (s[len(s)/2-1] + s[len(s)/2]) / 2
		}
		rules = append(rules, TempRule{Path: p, Label: labelOf(p), ClicksPerDegree: math.Round(med*100) / 100, Pairs: len(s)})
	}
	return rules, pairs, nil
}

func labelOf(path string) string {
	switch path {
	case "basicSetup.tyres.tyrePressure[0]":
		return "psi FL"
	case "basicSetup.tyres.tyrePressure[1]":
		return "psi FR"
	case "basicSetup.tyres.tyrePressure[2]":
		return "psi RL"
	case "basicSetup.tyres.tyrePressure[3]":
		return "psi RR"
	case "advancedSetup.aeroBalance.brakeDuct[0]":
		return "Brake ducts front"
	case "advancedSetup.aeroBalance.brakeDuct[1]":
		return "Brake ducts rear"
	}
	return path
}

// trackTemp is the track temperature a setup name gives (D-145): the track value, else the middle
// of a range or the one temperature whose kind the name doesn't say.
func trackTemp(m naming.Meta) (float64, bool) {
	switch {
	case m.Track > 0:
		return float64(m.Track), true
	case m.TempMin > 0:
		return float64(m.TempMin+max(m.TempMin, m.TempMax)) / 2, true
	}
	return 0, false
}

// ProposeTemperature works out how to adjust a setup to another air and track temperature. The
// changes follow the track temperature; the air temperature goes into the new name (D-145).
func (l *Library) ProposeTemperature(setupID int64, toAir, toTrack int) (TempProposal, error) {
	if l.db == nil {
		return TempProposal{}, ErrNoHistory
	}
	if toAir < 5 || toAir > 45 || toTrack < 5 || toTrack > 65 {
		return TempProposal{}, errors.New("enter the air temperature (5–45 °C) and the track temperature (5–65 °C), as the game shows them")
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return TempProposal{}, history.ErrNotFound
	}
	p := TempProposal{To: fmt.Sprintf("air %d °C, track %d °C", toAir, toTrack), Changes: []TempChange{}, Rules: []TempRule{}, Edits: map[string]string{}}
	fromTrack, ok := trackTemp(s.Meta)
	if !ok {
		return p, errors.New("this setup has no temperature yet: fill in the air and track temperature on the Notes & details tab first")
	}
	p.From = nameTemps(s.Meta, "air")
	rules, pairs, err := l.learnTempRules(s.Game, s.Car)
	if err != nil {
		return p, err
	}
	p.Rules, p.Pairs = rules, pairs
	if len(rules) == 0 {
		p.Message = "SetupMan can't propose changes yet: it learns from your own setups of this car that have a temperature in their name, and needs at least one pair of variants of the same setup at different temperatures."
		return p, nil
	}
	v, _ := l.db.LatestVersion(s.ID)
	g := l.game(s.Game)
	fields, err := g.ParseSetup(v.Content)
	if err != nil {
		return p, err
	}
	cur := map[string]float64{}
	for _, f := range fields {
		cur[f.Path] = f.Num
	}
	delta := float64(toTrack) - fromTrack
	for _, r := range rules {
		old, ok := cur[r.Path]
		if !ok {
			continue
		}
		nv := int(math.Round(old + r.ClicksPerDegree*delta))
		if nv < 0 {
			nv = 0
		}
		if nv != int(old) {
			p.Changes = append(p.Changes, TempChange{Path: r.Path, Label: r.Label, From: int(old), To: nv})
			p.Edits[r.Path] = strconv.Itoa(nv)
			// Keep the first pit strategy's tyre pressures in step with the main ones.
			if pp := pitPath(r.Path); pp != "" {
				if ov, ok := cur[pp]; ok {
					if npv := int(math.Round(ov + r.ClicksPerDegree*delta)); npv >= 0 && npv != int(ov) {
						p.Edits[pp] = strconv.Itoa(npv)
					}
				}
			}
		}
	}
	m := s.Meta
	m.Air, m.Track, m.TempMin, m.TempMax = toAir, toTrack, 0, 0
	// New details plus the words of the old name SetupMan does not recognise (not its old temperature).
	p.NewName = strings.TrimSpace(naming.Suggest(m, s.Session, false, "") + " " + strings.Join(naming.Rest(s.Name, s.Meta), " "))
	if p.NewName == "" || strings.EqualFold(p.NewName, s.Name) {
		p.NewName = fmt.Sprintf("%s %02d%02d", s.Name, toAir, toTrack)
	}
	if len(p.Changes) == 0 {
		p.Message = "Based on your own setups, nothing needs to change for this temperature."
	}
	return p, nil
}

func pitPath(path string) string {
	const pre = "basicSetup.tyres.tyrePressure"
	if rest, ok := strings.CutPrefix(path, pre); ok {
		return "basicSetup.strategy.pitStrategy#1.tyres.tyrePressure" + rest
	}
	return ""
}
