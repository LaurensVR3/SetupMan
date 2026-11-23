package library

// Recording laps per setup version (spec D-44, D-48). Every live-check tick the game's
// telemetry snapshot is fed to a small state machine that notices completed laps.
// Nothing here writes to a game folder.

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/lastopened"
	"setupman/internal/store"
)

// recorder follows one game's current session.
type recorder struct {
	sessionID       int64
	car, track      string
	kind            string
	lastLaps        int
	setupID         int64
	versionID       int64
	setupName       string
	lapStartFuel    float64
	lapPit          bool // the lap touched the pit lane / pit box (out-lap, in-lap, stop)
	lapInvalid      bool
	airSum, roadSum float64
	tempN           int
	pSum            [4]float64 // hot tyre pressures summed over the lap, outside the pits (D-124)
	pN              int
	wet             bool // rain tyres fitted
	maxStintMs      int  // the server's stint limit: the highest "stint time left" seen (D-125)
	wasInPits       bool
	laps            int
	// matchPending: look for the driven setup as soon as the car is out of the pits with real
	// values. When a session loads, ACC first reports placeholders (0 L fuel, ABS 5, map 2;
	// rig test 2026-11-20), and the setup can still be changed in the pit box.
	matchPending bool
	// manual setup select (D-149): every setup in the folder for this car and track, and the
	// equally likely ones when SetupMan isn't sure
	choices   []Choice
	shortlist []Choice
	// the latest air and track temperature of the session, for naming saves made in the game's
	// menus (the setup screen reports no live session, rig log 2026-11-21)
	lastAir, lastRoad float64
	hasTemps          bool
	// recognising in the pits before the first lap (D-152): the laps done when the session
	// started, and the values last compared there
	startLaps int
	pitKey    string
}

// Choice is one setup offered in manual setup select.
type Choice struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Recording describes what is being recorded right now (for the top bar).
type Recording struct {
	Active    bool   `json:"active"`
	SessionID int64  `json:"sessionId,omitempty"`
	Game      string `json:"game,omitempty"`
	Car       string `json:"car,omitempty"`
	Track     string `json:"track,omitempty"`
	SetupID   int64  `json:"setupId,omitempty"`
	SetupName string `json:"setupName,omitempty"`
	Laps      int    `json:"laps"`
	// manual setup select (D-149): Likely = the equally likely setups when SetupMan isn't sure,
	// Choices = every setup in the folder for this car and track
	Likely  []Choice `json:"likely,omitempty"`
	Choices []Choice `json:"choices,omitempty"`
	// for Race prep (D-151): the session's kind and latest air and track temperature
	Kind     string  `json:"kind,omitempty"`
	Air      float64 `json:"air,omitempty"`
	Road     float64 `json:"road,omitempty"`
	HasTemps bool    `json:"hasTemps,omitempty"`
}

// Recording returns the current recording state.
func (l *Library) Recording() Recording {
	l.recMu.Lock()
	defer l.recMu.Unlock()
	for id, r := range l.recs {
		if r != nil && r.sessionID != 0 {
			rec := Recording{Active: true, SessionID: r.sessionID, Game: id, Car: r.car, Track: r.track, SetupID: r.setupID, SetupName: r.setupName, Laps: r.laps,
				Choices: append([]Choice(nil), r.choices...), Kind: r.kind, Air: r.lastAir, Road: r.lastRoad, HasTemps: r.hasTemps}
			if r.setupID == 0 {
				rec.Likely = append([]Choice(nil), r.shortlist...)
			}
			return rec
		}
	}
	return Recording{}
}

// recordTick feeds the telemetry of every visible game into its recorder.
func (l *Library) recordTick() {
	if l.db == nil {
		return
	}
	for _, g := range l.games {
		src, ok := g.(game.TelemetrySource)
		if !ok || !l.isVisible(g.ID()) {
			continue
		}
		t, running := src.Telemetry()
		l.feedTelemetry(g, t, running)
	}
}

func inPits(t game.Telemetry) bool { return t.InPit || (t.PitLaneKnown && t.InPitLane) }

// pitKey sums up the values setup recognition compares: fuel in whole litres (it drops while
// driving down the pit lane; rig log 2026-11-21: 14.0 then 13.9 L looked twice), not the tyre
// pressures (they change as the tyres warm up).
func pitKey(t game.Telemetry) string {
	return fmt.Sprintf("%.0f|%d|%d|%d|%v|%.1f %.1f %.1f %.1f|%.0f", t.Fuel, t.TC, t.ABS, t.Map, t.PitMenuKnown,
		t.PitPressures[0], t.PitPressures[1], t.PitPressures[2], t.PitPressures[3], t.PitFuel)
}

// feedTelemetry advances the recorder of one game with one snapshot.
func (l *Library) feedTelemetry(g game.Game, t game.Telemetry, running bool) {
	l.recMu.Lock()
	defer l.recMu.Unlock()
	id := g.ID()
	r := l.recs[id]
	changed := false
	defer func() {
		if changed {
			l.publish(Event{Type: "recording"})
		}
	}()

	if !running {
		if r != nil {
			l.recs[id], changed = nil, true // game closed: the session ended
			l.stopExtra(id)
		}
		return
	}
	if !t.Live || t.Car == "" || t.Track == "" {
		return // menus, pause, replay: keep the session as it is
	}

	newSession := r == nil || !strings.EqualFold(r.car, t.Car) || !strings.EqualFold(r.track, t.Track) ||
		r.kind != t.Session || t.CompletedLaps < r.lastLaps
	if newSession {
		s := &store.Session{Game: id, Car: t.Car, Track: strings.ToLower(t.Track), Kind: t.Session}
		if err := l.db.InsertSession(s); err != nil {
			log.Printf("recording: %v", err)
			return
		}
		r = &recorder{sessionID: s.ID, car: t.Car, track: strings.ToLower(t.Track), kind: t.Session, lastLaps: t.CompletedLaps,
			lapStartFuel: t.Fuel, lapPit: true, wasInPits: inPits(t)}
		r.matchPending = true
		r.startLaps = t.CompletedLaps
		l.recs[id], changed = r, true
		logSession(g, t)
		l.startExtra(g, s.ID) // extra telemetry log for this session (D-162)
	}

	// Leaving the pits: the setup may have been changed there, so look again.
	if r.wasInPits && !inPits(t) {
		logPits(g, t, false)
		r.matchPending = true
	} else if !r.wasInPits && inPits(t) {
		logPits(g, t, true)
	}
	r.wasInPits = inPits(t)
	if r.matchPending && t.Fuel > 0 && !inPits(t) {
		r.matchPending = false
		l.matchSetup(g, r, t)
		changed = true
	} else if inPits(t) && t.Fuel > 0 && t.CompletedLaps == r.startLaps {
		// Before the first lap, look already in the pit box and pit lane (user, rig 2026-11-21:
		// "Would be handy to do it while still in pitlane"), again whenever the car's values change
		// (a setup loaded in the pit box); leaving the pits looks once more.
		if key := pitKey(t); key != r.pitKey {
			r.pitKey = key
			l.matchSetup(g, r, t)
			changed = true
		}
	}
	if r.lapStartFuel <= 0 && t.Fuel > 0 {
		r.lapStartFuel = t.Fuel // the first real fuel reading of the session (placeholders read 0 L)
	}

	if inPits(t) {
		r.lapPit = true
	}
	if t.ValidKnown && !t.ValidLap {
		r.lapInvalid = true
	}
	if t.TempsKnown {
		r.airSum, r.roadSum, r.tempN = r.airSum+t.AirTemp, r.roadSum+t.RoadTemp, r.tempN+1
		r.lastAir, r.lastRoad, r.hasTemps = t.AirTemp, t.RoadTemp, true
	}
	if t.PressuresKnown && !inPits(t) {
		for i, v := range t.Pressures {
			r.pSum[i] += v
		}
		r.pN++
	}
	if t.WetKnown {
		r.wet = t.WetTyres
	}
	if t.StintKnown && t.StintLeftMs > r.maxStintMs {
		r.maxStintMs = t.StintLeftMs
	}

	if t.CompletedLaps > r.lastLaps && t.LastLapMs > 0 {
		lap := &store.Lap{SessionID: r.sessionID, SetupID: r.setupID, VersionID: r.versionID, LapMs: t.LastLapMs,
			Valid: !r.lapInvalid, Pit: r.lapPit || t.CompletedLaps-r.lastLaps > 1}
		if used := r.lapStartFuel - t.Fuel; used > 0 && used < 30 {
			lap.FuelUsed = used // a refuelled lap (negative) or a jump is left unknown
		}
		if r.tempN > 0 {
			lap.Air, lap.Road, lap.HasTemps = r.airSum/float64(r.tempN), r.roadSum/float64(r.tempN), true
		}
		if r.pN > 0 {
			for i := range r.pSum {
				lap.Pressures[i] = r.pSum[i] / float64(r.pN)
			}
			lap.HasPressures = true
		}
		lap.Wet = r.wet
		if err := l.db.InsertLap(lap); err != nil {
			log.Printf("recording a lap: %v", err)
		}
		logLap(g, t.CompletedLaps, lap, t)
		r.laps++
		changed = true
		// Start the next lap. Crossing the line in the pit lane makes the next lap an out-lap.
		r.lastLaps, r.lapStartFuel, r.lapInvalid = t.CompletedLaps, t.Fuel, false
		r.lapPit = inPits(t)
		r.airSum, r.roadSum, r.tempN = 0, 0, 0
		r.pSum, r.pN = [4]float64{}, 0
	} else if t.CompletedLaps > r.lastLaps {
		r.lastLaps = t.CompletedLaps
	}
}

// matchSetup works out the setup being driven among the setups in the game folder for this car
// and track (the user can correct it per session). First choice (D-147): the setup file the game
// opened last since it started. When the game opened several at once, the values the car reports
// (fuel, TC, ABS, map) decide among those; when it opened none (or Windows doesn't record it),
// the values decide among all.
func (l *Library) matchSetup(g game.Game, r *recorder, t game.Telemetry) {
	if _, ro := g.(game.ReadOnly); ro {
		return // one save file for all setups: nothing to recognise from files
	}
	m, _ := g.(game.SetupMatcher)
	setups, err := l.db.SetupsForTrack(g.ID(), r.car, r.track)
	if err != nil {
		return
	}
	root := l.folderOf(g.ID())
	var ids, versions []int64
	var names, paths []string
	var contents [][]byte
	for _, s := range setups {
		if s.RelPath == "" {
			continue
		}
		vid := s.DiskVersionID
		v, err := l.db.Version(vid)
		if vid == 0 || err != nil || v == nil {
			if v, err = l.db.LatestVersion(s.ID); err != nil || v == nil {
				continue
			}
		}
		ids, versions, names, contents = append(ids, s.ID), append(versions, v.ID), append(names, s.Name), append(contents, v.Content)
		p := ""
		if root != "" {
			p = filepath.Join(root, filepath.FromSlash(s.RelPath))
		}
		paths = append(paths, p)
	}

	byValues := func(among []int) []int { // the best-fitting indexes into ids
		if m == nil || len(among) == 0 {
			return nil
		}
		sub := make([][]byte, len(among))
		for k, i := range among {
			sub[k] = contents[i]
		}
		var out []int
		for _, k := range m.MatchSetup(t, sub) {
			if k >= 0 && k < len(among) {
				out = append(out, among[k])
			}
		}
		return out
	}
	all := make([]int, len(ids))
	for i := range all {
		all[i] = i
	}

	// The choices for manual setup select: every setup in the folder for this car and track.
	r.choices = r.choices[:0]
	for k := range ids {
		r.choices = append(r.choices, Choice{ID: ids[k], Name: names[k]})
	}
	sort.Slice(r.choices, func(a, b int) bool { return strings.ToLower(r.choices[a].Name) < strings.ToLower(r.choices[b].Name) })

	i, how := -1, ""
	var short []int // equally likely setups, offered first in manual setup select
	// The file the game opened last decides first, except for games that keep their setups in
	// memory: ACC reads every setup when it lists them and none when one is loaded (D-149, D-153),
	// so for ACC the file times are not even looked at.
	byFile := true
	if c, ok := g.(game.SetupCacher); ok && c.CachesSetups() {
		byFile = false
	}
	if st := lastopened.Check(); byFile && st.Enabled {
		start := gameStart(g)
		opened := l.openedSince(start, paths)
		logOpened(g, start, names, opened)
		switch newest := newestOpened(opened); {
		case len(newest) == 1:
			i, how = newest[0], "the file the game opened last"
		case len(newest) > 1:
			switch v := byValues(newest); {
			case len(v) == 1:
				i, how = v[0], fmt.Sprintf("the values, among %d files the game opened together", len(newest))
			case len(v) > 1:
				short = v
			}
		}
	} else if byFile {
		logOpened(g, time.Time{}, names, nil) // says the Windows setting is off
	}
	if i < 0 && short == nil {
		switch v := byValues(all); {
		case len(v) == 1:
			i, how = v[0], "the values"
		case len(v) > 1:
			short = v
		}
	}
	r.shortlist = r.shortlist[:0]
	if i < 0 {
		for _, k := range short {
			r.shortlist = append(r.shortlist, Choice{ID: ids[k], Name: names[k]})
		}
		logMatch(g, t, len(ids), "", r.setupName, shortText(names, short))
		return // unsure: laps are recorded without a setup until the user picks one
	}
	if v := byValues(all); how == "the file the game opened last" && len(v) == 1 && v[0] != i {
		how += fmt.Sprintf(" (the values point to %q: changed in the game after loading?)", names[v[0]])
	}
	// A setup the user set by hand for this session stays.
	if s, err := l.db.Session(r.sessionID); err == nil && s != nil && s.Match == "manual" {
		return
	}
	logMatch(g, t, len(ids), names[i], "", how)
	r.setupID, r.versionID, r.setupName = ids[i], versions[i], names[i]
	_ = l.db.SetSessionSetup(r.sessionID, ids[i], versions[i], "auto", false)
}

// shortText names the equally likely setups for the log.
func shortText(names []string, short []int) string {
	if len(short) == 0 {
		return ""
	}
	q := make([]string, len(short))
	for k, i := range short {
		q[k] = fmt.Sprintf("%q", names[i])
	}
	return "equally likely: " + strings.Join(q, ", ")
}

// SetSessionSetup lets the user say which setup was driven in a session; its laps move along.
func (l *Library) SetSessionSetup(sessionID, setupID int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	var versionID int64
	if setupID != 0 {
		s, err := l.db.Setup(setupID)
		if err != nil || s == nil {
			return history.ErrNotFound
		}
		versionID = s.DiskVersionID
		if versionID == 0 {
			if v, _ := l.db.LatestVersion(setupID); v != nil {
				versionID = v.ID
			}
		}
	}
	if err := l.db.SetSessionSetup(sessionID, setupID, versionID, "manual", true); err != nil {
		return err
	}
	l.recMu.Lock()
	for _, r := range l.recs {
		if r != nil && r.sessionID == sessionID {
			r.setupID, r.versionID, r.setupName = setupID, versionID, ""
			r.shortlist = nil
			if s, _ := l.db.Setup(setupID); s != nil {
				r.setupName = s.Name
			}
		}
	}
	l.recMu.Unlock()
	l.publish(Event{Type: "history-changed"})
	l.publish(Event{Type: "recording"})
	return nil
}
