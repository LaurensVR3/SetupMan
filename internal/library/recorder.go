package library

// Recording laps per setup version (spec D-44, D-48). Every live-check tick the game's
// telemetry snapshot is fed to a small state machine that notices completed laps.
// Nothing here writes to a game folder.

import (
	"errors"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/lastopened"
	"setupman/internal/naming"
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
	ttSum           [4]float64 // tyre temperatures summed over the lap, outside the pits (D-189)
	ttN             int
	wet             bool // rain tyres fitted
	maxStintMs      int  // the server's stint limit: the highest "stint time left" seen (D-125)
	wasInPits       bool
	laps            int
	// The game restarts its lap clock without counting a lap when the car crosses the line on its
	// way out of the pit box, or after a return to the garage (rig 2026-11-24, Imola). The lap
	// then starts there: lapClock is the clock last read, restartAt when it went back (zero = not)
	// with the fuel and pit state then, lastLapAt when the game last counted a lap.
	lapClock    int
	clockKnown  bool
	restartAt   time.Time
	restartFuel float64
	restartPit  bool
	lastLapAt   time.Time
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
	// setup per stint (D-177): when the current stint began (session start or leaving the pits),
	// whether the setup was picked by hand, and the car's setup values then (setupKey). A setup
	// picked by hand stays until those values change (another setup loaded in the pits).
	stintStart time.Time
	manual     bool
	setKey     string // the car's setupKey when the current setup was set (by SetupMan or by hand)
	lastKey    string
	// leaving a server (D-177): when the game went back to its main menu
	offSince time.Time
	// tyre advice (D-182): the cold pressures of the tyres on the car (from the first reading of the
	// session, or the pressures fitted at a stop), since when, and the hot average of the last lap
	coldBase  [4]float64
	coldKnown bool
	coldSince time.Time
	lastHot   [4]float64
	hotKnown  bool
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

// noteColdPressures keeps the cold pressures of the tyres on the car, the base of the tyre advice
// (D-182; owner 2026-11-24: the advice moved while the pit menu was changed, so one click became
// two). The first reading of a session is cold. Leaving the pits, the tyres are new when the car's
// pressures equal the pit menu's (they were fitted at those) or are clearly below the last lap's hot
// average (cold again, e.g. after the garage); otherwise the tyres stayed on and the base stays.
func (l *Library) noteColdPressures(r *recorder, t game.Telemetry, leaving bool) {
	if !t.PressuresKnown {
		return
	}
	if !r.coldKnown {
		r.coldBase, r.coldKnown, r.coldSince = round4(t.Pressures), true, time.Now()
		return
	}
	if !leaving {
		return
	}
	menu := t.PitMenuKnown
	for w := 0; menu && w < 4; w++ {
		menu = math.Abs(t.Pressures[w]-t.PitPressures[w]) <= 0.3
	}
	cooler := false
	if r.hotKnown {
		cooler = (r.lastHot[0]+r.lastHot[1]+r.lastHot[2]+r.lastHot[3])-(t.Pressures[0]+t.Pressures[1]+t.Pressures[2]+t.Pressures[3]) > 4*0.8
	}
	switch {
	case menu:
		r.coldBase, r.coldSince = round4(t.PitPressures), time.Now()
	case cooler:
		r.coldBase, r.coldSince = round4(t.Pressures), time.Now()
	}
}

func round4(p [4]float64) [4]float64 {
	for i := range p {
		p[i] = math.Round(p[i]*10) / 10
	}
	return p
}

// resumeMaxAge: how old a session may be to continue it after SetupMan restarted (A-5).
const resumeMaxAge = 6 * time.Hour

// resumeSession continues the newest recorded session when SetupMan was restarted during it (A-5;
// rig 2026-11-23: a restart at 19:29 split one practice in two). Only for the first session SetupMan
// sees after it started, and only when that session has the same car, track and kind, began after
// the game started (the same run of the game) and is at most a few hours old. Its laps, setup and
// extra log carry on.
func (l *Library) resumeSession(g game.Game, t game.Telemetry) *recorder {
	id := g.ID()
	if l.resumeTried == nil {
		l.resumeTried = map[string]bool{}
	}
	if l.resumeTried[id] {
		return nil
	}
	l.resumeTried[id] = true
	start := gameStart(g)
	s, err := l.db.LatestSession(id)
	if err != nil || s == nil || start.IsZero() || !s.StartedAt.After(start) || time.Since(s.StartedAt) > resumeMaxAge ||
		!strings.EqualFold(s.Car, t.Car) || !strings.EqualFold(s.Track, t.Track) || s.Kind != t.Session {
		return nil
	}
	laps, _ := l.db.LapsForSession(s.ID)
	r := &recorder{sessionID: s.ID, car: s.Car, track: strings.ToLower(s.Track), kind: s.Kind, lastLaps: t.CompletedLaps,
		lapStartFuel: t.Fuel, lapPit: true, wasInPits: inPits(t), matchPending: true, startLaps: t.CompletedLaps,
		stintStart: time.Now(), laps: len(laps), setupID: s.SetupID, versionID: s.VersionID, manual: s.Match == "manual",
		// the car's values now count as the ones of the kept setup, so a setup picked by hand still
		// gives way once another setup is loaded (D-177); unknown yet ("") is filled in later
		setKey: setupKey(t), lastKey: setupKey(t)}
	if s.SetupID != 0 {
		if st, _ := l.db.Setup(s.SetupID); st != nil {
			r.setupName = st.Name
		}
	}
	log.Printf("rec %s: session %d continues (SetupMan was restarted during it), %d laps so far", id, s.ID, len(laps))
	l.startExtraResume(g, s.ID)
	return r
}

// setupKey sums up the values that come from the loaded setup and that the driver rarely changes
// on track: the pit menu (its tyre pressures and fuel come from the setup's pit strategy). When
// they change, another setup was loaded (rig 2026-11-23: 2727 swapped for 3040 in the pits).
func setupKey(t game.Telemetry) string {
	if !t.PitMenuKnown {
		return ""
	}
	return fmt.Sprintf("%.1f %.1f %.1f %.1f|%.0f", t.PitPressures[0], t.PitPressures[1], t.PitPressures[2], t.PitPressures[3], t.PitFuel)
}

// offGrace: how long the game must stay in its main menu before the session counts as ended
// (a loading screen can report "off" for a moment).
var offGrace = 5 * time.Second

// prevSessionWindow: how recent the previous session must be for its setup to break a tie (D-180).
const prevSessionWindow = 4 * time.Hour

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
	if t.Off {
		// Back in the game's main menu (left the server or the session): the session ended (D-177).
		if r != nil {
			if r.offSince.IsZero() {
				r.offSince = time.Now()
			} else if time.Since(r.offSince) >= offGrace {
				log.Printf("rec %s: session ended (back in the main menu)", g.ID())
				l.recs[id], changed = nil, true
				l.stopExtra(id)
			}
		}
		return
	}
	if r != nil {
		r.offSince = time.Time{}
	}
	if !t.Live || t.Car == "" || t.Track == "" {
		return // pause, replay, setup screens: keep the session as it is
	}

	newSession := r == nil || !strings.EqualFold(r.car, t.Car) || !strings.EqualFold(r.track, t.Track) ||
		r.kind != t.Session || t.CompletedLaps < r.lastLaps
	if newSession && r == nil {
		if rr := l.resumeSession(g, t); rr != nil {
			r, newSession = rr, false
			l.recs[id], changed = r, true
		}
	}
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
		r.stintStart = time.Now()
		l.recs[id], changed = r, true
		logSession(g, t)
		l.startExtra(g, s.ID) // extra telemetry log for this session (D-162)
	}

	// Leaving the pits: the setup may have been changed there, so look again.
	if r.wasInPits && !inPits(t) {
		logPits(g, t, false)
		// Once a race is under way the setup can't be swapped (a stop changes only the pit strategy
		// items) and the fuel added fits no setup: keep the known setup there (D-177).
		r.matchPending = r.setupID == 0 || r.kind != "race" || t.CompletedLaps == r.startLaps
		r.stintStart = time.Now() // a new stint: its laps may be driven with another setup (D-177)
	} else if !r.wasInPits && inPits(t) {
		logPits(g, t, true)
	}
	l.noteColdPressures(r, t, r.wasInPits && !inPits(t))
	r.wasInPits = inPits(t)
	if k := setupKey(t); k != "" {
		r.lastKey = k
		if r.setKey == "" && r.setupID != 0 {
			// the setup was set before the car's values were known (picked by hand early, or kept
			// after a restart): these are its values, so a later change shows another setup (D-177)
			r.setKey = k
		}
	}
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
	if t.TyreTempsKnown && !inPits(t) {
		for i, v := range t.TyreTemps {
			r.ttSum[i] += v
		}
		r.ttN++
	}
	if t.WetKnown {
		r.wet = t.WetTyres
	}
	if t.StintKnown && t.StintLeftMs > r.maxStintMs {
		r.maxStintMs = t.StintLeftMs
	}

	if t.CurrentKnown {
		if r.clockKnown && r.lapClock > t.CurrentLapMs+1000 && t.CompletedLaps == r.lastLaps && time.Since(r.lastLapAt) > lapRestartWait {
			r.restartAt, r.restartFuel, r.restartPit = time.Now(), t.Fuel, inPits(t)
		}
		r.lapClock, r.clockKnown = t.CurrentLapMs, true
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
			r.lastHot, r.hotKnown = lap.Pressures, true
		}
		if r.ttN > 0 {
			for i := range r.ttSum {
				lap.TyreTemps[i] = r.ttSum[i] / float64(r.ttN)
			}
			lap.HasTyreTemps = true
		}
		lap.Wet = r.wet
		if err := l.db.InsertLap(lap); err != nil {
			log.Printf("recording a lap: %v", err)
		}
		logLap(g, t.CompletedLaps, lap, t)
		r.laps++
		changed = true
		// Start the next lap. Crossing the line in the pit lane makes the next lap an out-lap.
		r.lastLaps, r.lapStartFuel = t.CompletedLaps, t.Fuel
		r.startLap(inPits(t))
		r.restartAt, r.lastLapAt = time.Time{}, time.Now()
	} else if t.CompletedLaps > r.lastLaps {
		r.lastLaps = t.CompletedLaps
		r.restartAt, r.lastLapAt = time.Time{}, time.Now()
	} else if !r.restartAt.IsZero() && time.Since(r.restartAt) >= lapRestartWait {
		// the lap clock went back and the game counted no lap: what came before belongs to no
		// lap (out-lap from the pit box, return to the garage), the lap starts at the restart
		log.Printf("rec %s: lap clock restarted without a counted lap (fuel %.1f L, pits %v): the lap starts there", g.ID(), r.restartFuel, r.restartPit)
		r.lapStartFuel = r.restartFuel
		r.startLap(r.restartPit || inPits(t))
		r.restartAt = time.Time{}
	}
}

// lapRestartWait: how long after the lap clock went back the game may still count the lap (it
// restarts the clock a reading or two before it counts; SetupMan reads every 2 seconds).
var lapRestartWait = 5 * time.Second

// startLap clears what is gathered per lap; pit: the lap starts in the pit lane or box.
func (r *recorder) startLap(pit bool) {
	r.lapInvalid, r.lapPit = false, pit
	r.airSum, r.roadSum, r.tempN = 0, 0, 0
	r.pSum, r.pN = [4]float64{}, 0
	r.ttSum, r.ttN = [4]float64{}, 0
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

	// the air temperature each setup was made for, from its name (D-181)
	rules := l.Settings().TempRules
	airs := make([]float64, len(names))
	for k, n := range names {
		if lo, hi, ok := naming.ParseWith(n, rules).TempFor("air"); ok {
			airs[k] = float64(lo+hi) / 2
		}
	}
	tm, _ := g.(game.SetupTempMatcher)
	byValues := func(among []int) []int { // the best-fitting indexes into ids
		if m == nil || len(among) == 0 {
			return nil
		}
		sub := make([][]byte, len(among))
		subAir := make([]float64, len(among))
		for k, i := range among {
			sub[k], subAir[k] = contents[i], airs[i]
		}
		found := []int(nil)
		if tm != nil {
			found = tm.MatchSetupTemps(t, sub, subAir)
		} else {
			found = m.MatchSetup(t, sub)
		}
		var out []int
		for _, k := range found {
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
	// Several fit equally well: the setup of the previous session on this car and track (in the last
	// few hours) wins when it is one of them (rig 2026-11-23: practice recognised 3040, qualifying
	// ten minutes later found 2635 and 3040 equally likely), D-180.
	curInShort := false
	for _, k := range short {
		curInShort = curInShort || ids[k] == r.setupID
	}
	if i < 0 && len(short) > 1 && !curInShort { // the setup being driven, when among them, stays
		if prev, err := l.db.PreviousSessionSetup(g.ID(), r.car, r.track, r.sessionID, time.Now().Add(-prevSessionWindow)); err == nil && prev != 0 {
			for _, k := range short {
				if ids[k] == prev {
					i, how = k, fmt.Sprintf("the setup of your previous session, among %s", shortText(names, short))
					short = nil
					break
				}
			}
		}
	}
	// A setup picked by hand stays while the car's setup values are the ones it had then; once
	// they change, another setup was loaded and SetupMan follows it again (D-177).
	now := setupKey(t)
	changed := now != "" && r.setKey != "" && now != r.setKey
	keepManual := r.manual && r.setupID != 0 && !changed
	r.shortlist = r.shortlist[:0]
	if i < 0 {
		for _, k := range short {
			r.shortlist = append(r.shortlist, Choice{ID: ids[k], Name: names[k]})
		}
		inShort := false
		for _, c := range r.shortlist {
			inShort = inShort || c.ID == r.setupID
		}
		if r.setupID != 0 && changed && !inShort && len(short) > 0 {
			// The values no longer fit the setup laps were going to: say "unknown" rather than
			// put the next laps under the wrong setup; manual setup select offers the likely ones.
			logMatch(g, t, len(ids), "", "", fmt.Sprintf("%q no longer fits the car's values; %s", r.setupName, shortText(names, short)))
			r.setupID, r.versionID, r.setupName, r.manual = 0, 0, "", false
			_ = l.db.SetSessionSetup(r.sessionID, 0, 0, "none", false)
			return
		}
		logMatch(g, t, len(ids), "", r.setupName, shortText(names, short))
		return // unsure: laps are recorded without a setup until the user picks one
	}
	if v := byValues(all); how == "the file the game opened last" && len(v) == 1 && v[0] != i {
		how += fmt.Sprintf(" (the values point to %q: changed in the game after loading?)", names[v[0]])
	}
	if keepManual && ids[i] != r.setupID {
		logMatch(g, t, len(ids), "", r.setupName, fmt.Sprintf("you chose it; the values point to %q but did not change since", names[i]))
		return
	}
	logMatch(g, t, len(ids), names[i], "", how)
	if ids[i] == r.setupID {
		r.setKey = now
		return
	}
	r.setupID, r.versionID, r.setupName, r.manual, r.setKey = ids[i], versions[i], names[i], false, now
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
			r.manual, r.setKey = setupID != 0, r.lastKey
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

// setupVersion is the version of a setup that is in the game folder (else its newest).
func (l *Library) setupVersion(setupID int64) (int64, error) {
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return 0, history.ErrNotFound
	}
	if s.DiskVersionID != 0 {
		return s.DiskVersionID, nil
	}
	if v, _ := l.db.LatestVersion(setupID); v != nil {
		return v.ID, nil
	}
	return 0, nil
}

// SetRecordingSetup says which setup is driven now, from the top bar (D-177). On track it applies
// to the current stint (since the car left the pits) and the laps after it; in the pits, to the
// laps from now on (the laps before were driven with what was loaded then). It stays until the
// car's setup values change. Earlier stints are changed on the session page.
func (l *Library) SetRecordingSetup(setupID int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	versionID, err := l.setupVersion(setupID)
	if err != nil {
		return err
	}
	l.recMu.Lock()
	var r *recorder
	for _, x := range l.recs {
		if x != nil && x.sessionID != 0 {
			r = x
		}
	}
	if r == nil {
		l.recMu.Unlock()
		return errors.New("no session is being recorded")
	}
	from := "now on"
	if !r.wasInPits && !r.stintStart.IsZero() {
		from = "the start of this stint"
		if err := l.db.SetLapsSetupSince(r.sessionID, r.stintStart, setupID, versionID); err != nil {
			l.recMu.Unlock()
			return err
		}
	}
	_ = l.db.SetSessionSetup(r.sessionID, setupID, versionID, "manual", false)
	r.setupID, r.versionID, r.setupName = setupID, versionID, ""
	if s, _ := l.db.Setup(setupID); s != nil {
		r.setupName = s.Name
	}
	r.manual, r.setKey, r.shortlist = true, r.lastKey, nil
	log.Printf("rec: setup set by hand to %q (from %s)", r.setupName, from)
	l.recMu.Unlock()
	l.publish(Event{Type: "history-changed"})
	l.publish(Event{Type: "recording"})
	return nil
}

// SetStintSetup gives the laps of one stint of a session another setup (session page, D-177).
func (l *Library) SetStintSetup(sessionID int64, stint int, setupID int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	var versionID int64
	if setupID != 0 {
		v, err := l.setupVersion(setupID)
		if err != nil {
			return err
		}
		versionID = v
	}
	rep, err := l.SessionReport(sessionID)
	if err != nil {
		return err
	}
	if stint < 1 || stint > len(rep.Stints) {
		return history.ErrNotFound
	}
	st := rep.Stints[stint-1]
	var ids []int64
	for _, lr := range rep.Laps {
		if lr.N >= st.FromLap && lr.N <= st.ToLap {
			ids = append(ids, lr.ID)
		}
	}
	if err := l.db.SetLapsSetup(sessionID, ids, setupID, versionID); err != nil {
		return err
	}
	last := stint == len(rep.Stints)
	if last { // the session's own setup follows its last stint (shown in lists)
		_ = l.db.SetSessionSetup(sessionID, setupID, versionID, "manual", false)
	}
	l.recMu.Lock()
	for _, r := range l.recs {
		if r != nil && r.sessionID == sessionID && last {
			r.setupID, r.versionID, r.setupName = setupID, versionID, ""
			if s, _ := l.db.Setup(setupID); s != nil {
				r.setupName = s.Name
			}
			r.manual, r.setKey, r.shortlist = setupID != 0, r.lastKey, nil
		}
	}
	l.recMu.Unlock()
	l.publish(Event{Type: "history-changed"})
	l.publish(Event{Type: "recording"})
	return nil
}
