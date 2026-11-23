package library

import (
	"sort"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/game"
	"setupman/internal/setup"
	"setupman/internal/telemetry"
)

// GameSummary is what a game tile and the settings page show (spec D-28, D-33).
type GameSummary struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Short          string         `json:"short"`    // e.g. "AC EVO", used in buttons ("Show in AC EVO")
	Ext            string         `json:"ext"`      // setup file type, e.g. ".json", ".ini"
	ReadOnly       string         `json:"readOnly"` // why SetupMan can only read this game ("" = it can manage it)
	Installed      bool           `json:"installed"`
	DetectedFolder string         `json:"detectedFolder"`
	Folder         string         `json:"folder"` // folder in use
	Visibility     string         `json:"visibility"`
	Visible        bool           `json:"visible"`
	Status         string         `json:"status"` // "ok", "empty", "no-folder", "error", "scanning", "hidden"
	Message        string         `json:"message,omitempty"`
	Cars           int            `json:"cars"`
	Tracks         int            `json:"tracks"`
	Setups         int            `json:"setups"`
	Duplicates     int            `json:"duplicates"` // groups of identical / near-identical setups
	ScannedAt      *time.Time     `json:"scannedAt,omitempty"`
	Live           game.LiveState `json:"live"`
	Laps           bool           `json:"laps"`   // SetupMan records laps and gives pit stop advice for this game
	ByFile         bool           `json:"byFile"` // the setup driven is recognised from the file the game opened last (D-147, D-149)
	// Telemetry suite (D-160, D-162): the game writes telemetry files SetupMan can read, where it
	// found them, and whether SetupMan can add its extra log
	Telemetry         bool   `json:"telemetry"`
	DetectedTelemetry string `json:"detectedTelemetry,omitempty"`
	ExtraLog          bool   `json:"extraLog"`
	// Staging (Phase 3)
	Managed         bool `json:"managed"`
	TidyOnGameClose bool `json:"tidyOnGameClose"`
	AutoRename      bool `json:"autoRename"`
	FreshCount      int  `json:"freshCount"` // new in-game saves not reviewed yet
	Queued          int  `json:"queued"`     // staging changes waiting for the player to stop driving
}

// CarSummary is what a car tile shows.
type CarSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Brand      string `json:"brand"`
	Class      string `json:"class"`
	Year       int    `json:"year,omitempty"`
	Tracks     int    `json:"tracks"`
	Setups     int    `json:"setups"`
	Duplicates int    `json:"duplicates"`
}

// ClassGroup is one class heading with its cars, A–Z (spec D-29).
type ClassGroup struct {
	Class string       `json:"class"`
	Cars  []CarSummary `json:"cars"`
}

// GameDetail is the game page.
type GameDetail struct {
	Game     GameSummary  `json:"game"`
	Classes  []ClassGroup `json:"classes"`
	Ignored  []game.Issue `json:"ignored"`
	Warnings []game.Issue `json:"warnings"`
}

// TrackSummary is what a track tile shows.
type TrackSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Setups     int    `json:"setups"`
	Duplicates int    `json:"duplicates"`
	// Filled on the car page (D-165): the circuit outline from telemetry and the fastest valid lap
	// recorded or in the telemetry, 0 = none
	Outline *telemetry.Outline `json:"outline,omitempty"`
	BestMs  int                `json:"bestMs,omitempty"`
}

// CarDetail is the car page.
type CarDetail struct {
	Game   GameSummary    `json:"game"`
	Car    CarSummary     `json:"car"`
	Tracks []TrackSummary `json:"tracks"`
}

// TrackDetail is the track page (spec D-30).
type TrackDetail struct {
	Game      GameSummary      `json:"game"`
	Car       CarSummary       `json:"car"`
	Track     TrackSummary     `json:"track"`
	Folder    string           `json:"folder"`
	Setups    []setup.Setup    `json:"setups"`
	Pairs     []PairSummary    `json:"pairs"`
	Clusters  compare.Clusters `json:"clusters"`
	Threshold float64          `json:"threshold"`
	// Version history (spec D-41): library id and session per setup (same order as Setups),
	// plus setups whose file is no longer in the game folder.
	SetupIDs []int64        `json:"setupIds"`
	Sessions []string       `json:"sessions"`
	Removed  []RemovedSetup `json:"removed"` // library only
	// Game version per setup (same order as Setups): from the name, else estimated from the
	// date the setup was last saved (D-103, D-118). VersionGuessed marks the estimates.
	Versions       []string `json:"versions"`
	VersionGuessed []bool   `json:"versionGuessed"`
	Fresh          []int64  `json:"fresh"` // setup ids of new in-game saves (D-80)
	Managed        bool     `json:"managed"`
	Queued         int      `json:"queued"`
}

// RemovedSetup is a setup that only exists in the library now.
type RemovedSetup struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Session string    `json:"session"`
	Date    time.Time `json:"date"` // when it was last saved (file date of its newest version)
	Version string    `json:"version"`
	Guessed bool      `json:"versionGuessed"`
}

// classOrder is the display order of classes; unknown classes follow alphabetically.
var classOrder = []string{"GT3", "GT4", "GT2", "Cup", "TCX"}

// Games returns every supported game (visible or not).
func (l *Library) Games() []GameSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]GameSummary, 0, len(l.games))
	for _, g := range l.games {
		out = append(out, l.summary(g))
	}
	return out
}

// Game returns the game page, or false when the game is unknown or has no scan.
func (l *Library) Game(id string) (GameDetail, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	g := l.game(id)
	if g == nil {
		return GameDetail{}, false
	}
	d := GameDetail{Game: l.summary(g), Classes: []ClassGroup{}, Ignored: []game.Issue{}, Warnings: []game.Issue{}}
	s := l.states[id]
	if s.scan == nil {
		return d, true
	}
	d.Ignored, d.Warnings = s.scan.Ignored, s.scan.Warnings
	byClass := map[string][]CarSummary{}
	for i := range s.scan.Cars {
		c := &s.scan.Cars[i]
		byClass[c.Class] = append(byClass[c.Class], l.carSummary(c))
	}
	for _, class := range sortedClasses(byClass) {
		cars := byClass[class]
		sort.Slice(cars, func(i, j int) bool {
			if cars[i].Name != cars[j].Name {
				return strings.ToLower(cars[i].Name) < strings.ToLower(cars[j].Name)
			}
			return cars[i].Year < cars[j].Year
		})
		d.Classes = append(d.Classes, ClassGroup{Class: class, Cars: cars})
	}
	return d, true
}

// Car returns the car page.
func (l *Library) Car(id, carID string) (CarDetail, bool) {
	d, ok := l.car(id, carID)
	if ok {
		l.addTrackRecords(id, d.Car.ID, d.Tracks)
	}
	return d, ok
}

func (l *Library) car(id, carID string) (CarDetail, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	g, c := l.findCar(id, carID)
	if c == nil {
		return CarDetail{}, false
	}
	d := CarDetail{Game: l.summary(g), Car: l.carSummary(c), Tracks: []TrackSummary{}}
	for i := range c.Tracks {
		d.Tracks = append(d.Tracks, l.trackSummary(&c.Tracks[i]))
	}
	return d, true
}

// Track returns the track page.
func (l *Library) Track(id, carID, trackID string) (TrackDetail, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	g, c := l.findCar(id, carID)
	if c == nil {
		return TrackDetail{}, false
	}
	for i := range c.Tracks {
		t := &c.Tracks[i]
		if t.ID != strings.ToLower(trackID) {
			continue
		}
		d := TrackDetail{
			Game: l.summary(g), Car: l.carSummary(c), Track: l.trackSummary(t), Folder: t.Folder,
			Setups: t.Setups, Pairs: summarizePairs(t.Pairs), Clusters: l.cluster(t), Threshold: l.settings.Threshold,
			SetupIDs: make([]int64, len(t.Setups)), Sessions: make([]string, len(t.Setups)), Removed: []RemovedSetup{},
			Versions: make([]string, len(t.Setups)), VersionGuessed: make([]bool, len(t.Setups)),
			Fresh: []int64{}, Managed: l.managedLocked(g), Queued: l.QueuedCount(),
		}
		if l.db != nil {
			if lib, err := l.db.SetupsForTrack(id, c.ID, t.ID); err == nil {
				byPath := map[string]int{}
				for i, s := range t.Setups {
					byPath[strings.ToLower(s.RelPath)] = i
				}
				for _, ls := range lib {
					if ls.RelPath == "" {
						saved := ls.CreatedAt
						if v, _ := l.db.LatestVersion(ls.ID); v != nil && !v.FileModified.IsZero() {
							saved = v.FileModified
						}
						r := RemovedSetup{ID: ls.ID, Name: ls.Name, Session: ls.Session, Date: saved} // last saved, like the setups in the folder
						r.Version, r.Guessed = l.versionFor(id, ls.Meta.GameVersion, saved)
						d.Removed = append(d.Removed, r)
					} else if i, ok := byPath[strings.ToLower(ls.RelPath)]; ok {
						d.SetupIDs[i], d.Sessions[i] = ls.ID, ls.Session
						d.Versions[i], d.VersionGuessed[i] = l.versionFor(id, ls.Meta.GameVersion, t.Setups[i].Modified)
						if ls.Fresh {
							d.Fresh = append(d.Fresh, ls.ID)
						}
					}
				}
			}
		}
		return d, true
	}
	return TrackDetail{}, false
}

// ---------- helpers (callers hold l.mu) ----------

func (l *Library) summary(g game.Game) GameSummary {
	s := l.states[g.ID()]
	sum := GameSummary{
		ID: g.ID(), Name: g.Name(), Short: game.Short(g), Ext: game.Ext(g), ReadOnly: game.ReadOnlyReason(g), Installed: s.det.Installed, DetectedFolder: s.det.Folder,
		Folder: s.folder, Visibility: s.cfg.Visibility, Visible: s.visible, Live: l.live,
		Managed: l.managedLocked(g), TidyOnGameClose: s.cfg.TidyOnGameClose, AutoRename: s.cfg.AutoRename, Queued: l.QueuedCount(),
	}
	_, sum.Laps = g.(game.TelemetrySource)
	if tf, ok := g.(game.TelemetryFolder); ok {
		sum.Telemetry, sum.DetectedTelemetry = true, tf.TelemetryFolder()
	}
	_, sum.ExtraLog = g.(game.ExtraLogger)
	if c, ok := g.(game.SetupCacher); sum.Laps && (!ok || !c.CachesSetups()) {
		sum.ByFile = true
	}
	if sum.Visibility == "" {
		sum.Visibility = "auto"
	}
	switch {
	case !s.visible:
		sum.Status = "hidden"
	case s.folder == "":
		sum.Status, sum.Message = "no-folder", "No setup folder found. Choose one in Settings."
	case s.err != "":
		sum.Status, sum.Message = "error", s.err
	case s.scan == nil:
		sum.Status = "scanning"
	default:
		sum.Status = "ok"
		if l.db != nil {
			sum.FreshCount, _ = l.db.CountFresh(g.ID())
		}
		t := s.scan.ScannedAt
		sum.ScannedAt = &t
		sum.Cars = len(s.scan.Cars)
		tracks := map[string]bool{} // each track once, however many cars have setups for it
		for i := range s.scan.Cars {
			c := &s.scan.Cars[i]
			for j := range c.Tracks {
				tracks[strings.ToLower(c.Tracks[j].ID)] = true
				sum.Setups += len(c.Tracks[j].Setups)
				sum.Duplicates += dupCount(l.cluster(&c.Tracks[j]))
			}
		}
		sum.Tracks = len(tracks)
		if sum.Setups == 0 {
			sum.Status, sum.Message = "empty", s.scan.Hint
		}
	}
	return sum
}

func (l *Library) carSummary(c *game.Car) CarSummary {
	cs := CarSummary{ID: c.ID, Name: c.Name, Brand: c.Brand, Class: c.Class, Year: c.Year, Tracks: len(c.Tracks)}
	for i := range c.Tracks {
		cs.Setups += len(c.Tracks[i].Setups)
		cs.Duplicates += dupCount(l.cluster(&c.Tracks[i]))
	}
	return cs
}

func (l *Library) trackSummary(t *game.Track) TrackSummary {
	return TrackSummary{ID: t.ID, Name: t.Name, Setups: len(t.Setups), Duplicates: dupCount(l.cluster(t))}
}

func (l *Library) cluster(t *game.Track) compare.Clusters {
	fps := make([]string, len(t.Setups))
	names := make([]string, len(t.Setups))
	for i, s := range t.Setups {
		fps[i], names[i] = s.Fingerprint, s.Name
	}
	return compare.Cluster(fps, names, t.Pairs, l.settings.Threshold)
}

func (l *Library) findCar(id, carID string) (game.Game, *game.Car) {
	g := l.game(id)
	if g == nil || l.states[id].scan == nil {
		return nil, nil
	}
	scan := l.states[id].scan
	for i := range scan.Cars {
		if strings.EqualFold(scan.Cars[i].ID, carID) {
			return g, &scan.Cars[i]
		}
	}
	return nil, nil
}

// dupCount is the number of groups of (near) duplicate setups. An identical pair inside
// a near-identical group counts once, as one group.
func dupCount(c compare.Clusters) int {
	n := 0
	for _, id := range c.Cluster {
		n = max(n, id)
	}
	return n
}

func sortedClasses(byClass map[string][]CarSummary) []string {
	rank := func(c string) int {
		for i, k := range classOrder {
			if k == c {
				return i
			}
		}
		return len(classOrder)
	}
	var out []string
	for c := range byClass {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// PairSummary is one pair of setups on the track page: the score and a short description of
// the differences (the full differences are only sent when two setups are compared).
type PairSummary struct {
	A         int     `json:"a"`
	B         int     `json:"b"`
	Score     float64 `json:"score"`
	Identical bool    `json:"identical"`
	Clicks    int     `json:"clicks"`
	Differ    int     `json:"differ"`  // settings that differ (counted ones)
	Summary   string  `json:"summary"` // e.g. "Rear wing +1, TC -1, …"
}

func summarizePairs(pairs []compare.Pair) []PairSummary {
	out := make([]PairSummary, len(pairs))
	for i, p := range pairs {
		out[i] = PairSummary{A: p.A, B: p.B, Score: p.Score, Identical: p.Identical, Clicks: p.Clicks, Differ: p.Differ, Summary: p.Summary}
	}
	return out
}

// addTrackRecords adds each track's outline and fastest lap to the car page (outside the state
// lock: it reads the library database).
func (l *Library) addTrackRecords(gameID, car string, tracks []TrackSummary) {
	if l.db == nil {
		return
	}
	tele, _ := l.db.TelemetryBestPerCar(gameID, car)
	for i := range tracks {
		t := &tracks[i]
		t.Outline = l.outline(gameID, t.ID)
		t.BestMs = tele[strings.ToLower(t.ID)]
		if st, err := l.TrackLapTimes(gameID, car, t.ID); err == nil && st.BestMs > 0 && (t.BestMs == 0 || st.BestMs < t.BestMs) {
			t.BestMs = st.BestMs
		}
	}
}
