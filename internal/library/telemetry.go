package library

// Telemetry suite (spec D-159 … D-162). The game's own telemetry files (ACC's MoTeC logs) are
// indexed in the background and linked to recorded sessions, laps and setups; they are only ever
// read. While a session is recorded, SetupMan writes its small extra log (D-162) to
// %APPDATA%\SetupMan\telemetry. Indexing waits while the player drives (D-13).

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/motec"
	"setupman/internal/store"
	"setupman/internal/telemetry"
	"setupman/internal/watch"
)

const (
	teleDebounce  = 10 * time.Second // the game writes a log over a while: wait until it is quiet
	teleFirstSync = 5 * time.Second  // after start, so SetupMan's own start stays quick
	teleBusyFile  = 30 * time.Second // a file changed this recently is still being written
	extraEvery    = 100 * time.Millisecond
	extraIdle     = time.Second // menus, pause, replay: look less often
)

type teleState struct {
	folder    string
	stopWatch func()
	timer     *time.Timer
	pending   bool // a change arrived while driving
	indexing  bool
	refresh   bool // putting saves together failed last time: try again
	err       string
}

type teleData struct {
	mu       sync.Mutex
	stopped  bool // SetupMan is quitting: no new index runs, a running one stops
	states   map[string]*teleState
	syncMu   sync.Mutex      // one index run at a time
	failed   map[string]bool // files that could not be read (logged once)
	extraMu  sync.Mutex
	extras   map[string]chan struct{} // per game: closes to stop the extra log
	extraWG  sync.WaitGroup           // running extra logs, so quitting waits for their files
	cacheMu  sync.Mutex
	outlines map[string]*telemetry.Outline // track outlines read from the library
	cache    struct {
		path string
		mod  time.Time
		log  *telemetry.ExtraLog
	}
}

func (l *Library) tele() *teleData {
	l.teleOnce.Do(func() {
		l.teleD = &teleData{states: map[string]*teleState{}, failed: map[string]bool{}, extras: map[string]chan struct{}{}}
	})
	return l.teleD
}

// teleFolder is the telemetry folder in use for a game ("" = none or hidden).
func (l *Library) teleFolder(g game.Game) string {
	tf, ok := g.(game.TelemetryFolder)
	if !ok || !l.isVisible(g.ID()) {
		return ""
	}
	if f := l.Settings().GameOrDefault(g.ID()).TelemetryFolder; f != "" {
		return f
	}
	return tf.TelemetryFolder()
}

// applyTelemetry starts or moves the telemetry folder watchers after a settings change.
func (l *Library) applyTelemetry() {
	t := l.tele()
	var stops []func() // called after unlocking: the watcher's callback takes t.mu, and stopping waits for it
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	t.mu.Lock()
	quitting := t.stopped
	t.mu.Unlock()
	if quitting {
		return
	}
	for _, g := range l.games {
		if _, ok := g.(game.TelemetryFolder); !ok {
			continue
		}
		id, folder := g.ID(), l.teleFolder(g)
		t.mu.Lock()
		if t.stopped {
			t.mu.Unlock()
			return
		}
		s := t.states[id]
		if s == nil {
			s = &teleState{}
			t.states[id] = s
		}
		if s.folder == folder && (folder == "" || s.stopWatch != nil) {
			t.mu.Unlock()
			continue
		}
		if s.stopWatch != nil {
			stops = append(stops, s.stopWatch)
			s.stopWatch = nil
		}
		s.folder, s.err = folder, ""
		if folder != "" {
			if stop, err := watch.Watch(folder, func() { l.teleChanged(id) }); err == nil {
				s.stopWatch = stop
			}
		}
		t.mu.Unlock()
		if folder != "" {
			t.mu.Lock()
			s.refresh = true // put the saves together again once per run, whatever changed
			t.mu.Unlock()
			time.AfterFunc(teleFirstSync, func() { l.teleSync(id) })
		}
	}
}

// teleChanged is called by the watcher; it indexes after a quiet period, unless driving.
func (l *Library) teleChanged(id string) {
	t := l.tele()
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.states[id]
	if s == nil || t.stopped {
		return
	}
	if s.timer != nil {
		s.timer.Reset(teleDebounce)
		return
	}
	s.timer = time.AfterFunc(teleDebounce, func() {
		t.mu.Lock()
		s.timer = nil
		t.mu.Unlock()
		l.teleSync(id)
	})
}

// telemetryTick runs with every live check: when the player stops driving, index what arrived meanwhile.
func (l *Library) telemetryTick(now game.LiveState) {
	if now == game.LiveDriving {
		return
	}
	t := l.tele()
	var ids []string
	t.mu.Lock()
	for id, s := range t.states {
		if s.pending {
			s.pending = false
			ids = append(ids, id)
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		go l.teleSync(id)
	}
}

// teleSync brings the index of one game's telemetry folder up to date and links new files.
func (l *Library) teleSync(id string) {
	defer guard("telemetry index")
	g := l.game(id)
	if g == nil || l.db == nil {
		return
	}
	t := l.tele()
	t.syncMu.Lock()
	defer t.syncMu.Unlock()
	t.mu.Lock()
	s := t.states[id]
	if s == nil || s.folder == "" || t.stopped {
		t.mu.Unlock()
		return
	}
	folder := s.folder
	t.mu.Unlock()
	if l.Live() == game.LiveDriving {
		t.mu.Lock()
		s.pending = true
		t.mu.Unlock()
		return
	}

	entries, err := os.ReadDir(folder)
	t.mu.Lock()
	if err != nil {
		s.err = err.Error()
	} else {
		s.err, s.indexing = "", true
	}
	t.mu.Unlock()
	if err != nil {
		return
	}
	l.publish(Event{Type: "telemetry", Game: id})
	defer func() {
		t.mu.Lock()
		s.indexing = false
		t.mu.Unlock()
		l.publish(Event{Type: "telemetry", Game: id})
	}()

	known, err := l.db.TelemetryKnown(id)
	if err != nil {
		log.Printf("telemetry index %s: %v", id, err)
		t.mu.Lock()
		s.err = "SetupMan could not read its telemetry index: " + err.Error()
		t.mu.Unlock()
		return
	}
	seen := map[string]bool{}
	added, retry, interrupted, moved := 0, false, false, false
	failed, firstErr := 0, error(nil) // files that could not be saved: logged once per sync, shown on the page
	for _, e := range entries {
		name := e.Name()
		key := strings.ToLower(name)
		if e.IsDir() || !strings.HasSuffix(key, ".ld") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		seen[key] = true
		if k, ok := known[key]; ok && !telemetry.Changed(k, info.Size(), info.ModTime()) {
			if k.Missing {
				l.db.SetTelemetryMissing(k.ID, false)
				moved = true
			}
			continue
		}
		if time.Since(info.ModTime()) < teleBusyFile {
			retry = true // still being written
			continue
		}
		t.mu.Lock()
		quitting := t.stopped
		t.mu.Unlock()
		if quitting {
			return // SetupMan is quitting: the rest is indexed next time
		}
		if l.Live() == game.LiveDriving {
			interrupted = true
			break
		}
		f, laps, err := telemetry.IndexFile(folder, name, id)
		if err != nil {
			if !t.failed[key] {
				t.failed[key] = true
				if !errors.Is(err, motec.ErrFormat) {
					log.Printf("telemetry: %s: %v", name, err)
				}
			}
			continue
		}
		if err := l.db.SaveTelemetryFile(f, laps); err != nil {
			if failed++; firstErr == nil {
				firstErr = err
			}
			continue
		}
		added++
		time.Sleep(5 * time.Millisecond) // gentle on the disk during the first index
	}
	if !interrupted {
		for key, k := range known {
			if !seen[key] && !k.Missing {
				l.db.SetTelemetryMissing(k.ID, true) // removed by the user; metadata kept
				moved = true
			}
		}
	}
	t.mu.Lock()
	refresh := added > 0 || moved || s.refresh // a deleted or returning save changes how sessions are put together
	quitting := t.stopped
	t.mu.Unlock()
	if quitting {
		return // SetupMan is quitting: the rest happens at the next start
	}
	var refreshErr error
	if refresh {
		if refreshErr = l.db.RefreshTelemetrySaves(id); refreshErr == nil {
			refreshErr = l.db.RefreshTelemetryFull(id)
		}
		if refreshErr != nil {
			log.Printf("telemetry %s: %v", id, refreshErr)
		}
	}
	t.mu.Lock()
	s.refresh = refreshErr != nil // try again next time
	switch {
	case failed > 0:
		log.Printf("telemetry %s: %d files could not be saved, first error: %v", id, failed, firstErr)
		s.err = fmt.Sprintf("SetupMan could not save %d telemetry files to its library: %v", failed, firstErr)
	case refreshErr != nil:
		s.err = "SetupMan could not update its telemetry index: " + refreshErr.Error()
	}
	t.mu.Unlock()
	linked := l.linkTelemetry(id)
	l.teleOutlines(id)
	if added > 0 || linked > 0 {
		log.Printf("telemetry %s: %d files indexed, %d linked to sessions (%s)", id, added, linked, folder)
		freeMemory()
	}
	t.mu.Lock()
	if interrupted {
		s.pending = true
	}
	t.mu.Unlock()
	if retry {
		time.AfterFunc(teleBusyFile, func() { l.teleChanged(id) })
	}
}

// linkTelemetry links files to the sessions SetupMan recorded (D-160) and returns how many it linked.
func (l *Library) linkTelemetry(id string) int {
	files, err := l.db.TelemetryUnlinked(id)
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range files {
		end := f.StartedAt.Add(time.Duration(f.DurationMs)*time.Millisecond + 2*time.Minute)
		sessions, err := l.db.SessionsBetween(id, f.Car, f.Track, f.StartedAt.Add(-15*time.Minute), end)
		if err != nil || len(sessions) == 0 {
			continue
		}
		laps, err := l.db.TelemetryLaps(f.ID)
		if err != nil {
			continue
		}
		recorded := map[int64][]store.Lap{}
		for _, s := range sessions {
			recorded[s.ID], _ = l.db.LapsForSession(s.ID)
		}
		sid, links := telemetry.Match(f, laps, sessions, recorded)
		if sid == 0 {
			continue
		}
		if err := l.db.LinkTelemetry(f.ID, sid, links); err == nil {
			n++
		}
	}
	return n
}

// ---------- extra log (D-162) ----------

// startExtra starts the extra log for a newly recorded session.
func (l *Library) startExtra(g game.Game, sessionID int64) {
	xl, ok := g.(game.ExtraLogger)
	if !ok || l.TelemetryDir == "" || l.Settings().GameOrDefault(g.ID()).ExtraLogOff {
		return
	}
	l.stopExtra(g.ID())
	stop := make(chan struct{})
	t := l.tele()
	t.extraMu.Lock()
	t.extras[g.ID()] = stop
	t.extraMu.Unlock()
	t.extraWG.Add(1)
	go l.runExtra(xl, g.ID(), sessionID, stop)
}

// stopExtra ends the extra log of a game (the session ended or the game closed).
func (l *Library) stopExtra(id string) {
	t := l.tele()
	t.extraMu.Lock()
	if stop := t.extras[id]; stop != nil {
		close(stop)
		delete(t.extras, id)
	}
	t.extraMu.Unlock()
}

// runExtra reads the extra channels about 10 times per second while the session is live and
// writes them to the session's extra log. The file is only created once there is something to write.
func (l *Library) runExtra(xl game.ExtraLogger, id string, sessionID int64, stop chan struct{}) {
	defer l.tele().extraWG.Done()
	defer guard("extra telemetry log")
	chans := xl.ExtraChannels()
	vals := make([]float32, len(chans))
	var r game.ExtraReader
	var w *telemetry.ExtraWriter
	defer func() {
		if r != nil {
			r.Close()
		}
		if w != nil {
			if err := w.Close(); err != nil {
				log.Printf("extra telemetry log: %v", err)
			}
		}
	}()
	start := time.Now()
	wait := extraEvery
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-timer.C:
		}
		wait = l.extraStep(xl, id, sessionID, chans, vals, start, &r, &w)
		if wait == 0 {
			return
		}
		timer.Reset(wait)
	}
}

// extraStep takes one reading; it returns how long to wait for the next (0 = stop logging).
func (l *Library) extraStep(xl game.ExtraLogger, id string, sessionID int64, chans []game.ExtraChannel, vals []float32,
	start time.Time, r *game.ExtraReader, w **telemetry.ExtraWriter) time.Duration {
	if l.Settings().GameOrDefault(id).ExtraLogOff {
		return 0 // switched off in Settings during the session
	}
	if *r == nil {
		rd, err := xl.OpenExtra()
		if err != nil {
			return 2 * time.Second
		}
		*r = rd
	}
	live, ok := (*r).Read(vals)
	if !ok {
		(*r).Close()
		*r = nil
		return 2 * time.Second
	}
	if !live {
		return extraIdle
	}
	if *w == nil {
		h := telemetry.ExtraHeader{Game: id, Session: sessionID, Start: start, Hz: int(time.Second / extraEvery)}
		for _, c := range chans {
			h.Channels = append(h.Channels, telemetry.ExtraChannel{Name: c.Name, Unit: c.Unit})
		}
		ew, err := telemetry.CreateExtra(telemetry.ExtraPath(l.TelemetryDir, id, sessionID), h)
		if err != nil {
			log.Printf("extra telemetry log: %v", err)
			return 0
		}
		*w = ew
		log.Printf("extra telemetry log started for session %d (%d channels)", sessionID, len(chans))
	}
	if err := (*w).Write(time.Since(start).Seconds(), vals); err != nil {
		log.Printf("extra telemetry log: %v", err)
		return 0
	}
	return extraEvery
}

// stopTelemetry ends watchers and extra logs (SetupMan quits).
func (l *Library) stopTelemetry() {
	t := l.tele()
	var stops []func()
	t.mu.Lock()
	t.stopped = true
	for _, s := range t.states {
		if s.stopWatch != nil {
			stops = append(stops, s.stopWatch)
			s.stopWatch = nil
		}
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
	}
	t.mu.Unlock()
	for _, stop := range stops { // outside the lock (see applyTelemetry)
		stop()
	}
	// let an index run that is going finish its file (it stops before the next), at most 3 s
	synced := make(chan struct{})
	go func() { t.syncMu.Lock(); close(synced) }() // held on purpose: no index runs after quitting
	select {
	case <-synced:
	case <-time.After(3 * time.Second):
	}
	t.extraMu.Lock()
	for id, stop := range t.extras {
		close(stop)
		delete(t.extras, id)
	}
	t.extraMu.Unlock()
	// let the extra log finish its file (it closes within one reading), at most 2 s
	done := make(chan struct{})
	go func() { t.extraWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// extraLog returns the extra log of a session (nil when there is none), kept in memory while the
// same log is looked at.
func (l *Library) extraLog(gameID string, sessionID int64) *telemetry.ExtraLog {
	if sessionID == 0 || l.TelemetryDir == "" {
		return nil
	}
	p := telemetry.ExtraPath(l.TelemetryDir, gameID, sessionID)
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	t := l.tele()
	t.cacheMu.Lock()
	defer t.cacheMu.Unlock()
	if t.cache.path == p && t.cache.mod.Equal(st.ModTime()) {
		return t.cache.log
	}
	x, err := telemetry.ReadExtra(p)
	if err != nil {
		log.Printf("extra telemetry log %s: %v", filepath.Base(p), err)
		return nil
	}
	// the log must be this session's: after restoring an older library a new session can get the
	// number of one whose log is still there
	if x.Header.Game != gameID || x.Header.Session != sessionID {
		return nil
	}
	if s, _ := l.db.Session(sessionID); s == nil || x.Header.Start.Sub(s.StartedAt).Abs() > 15*time.Minute {
		return nil
	}
	t.cache.path, t.cache.mod, t.cache.log = p, st.ModTime(), x
	return x
}

// ---------- views for the Telemetry pages ----------

// TeleTrackCar is one car driven on a track.
type TeleTrackCar struct {
	Car     string    `json:"car"`
	CarName string    `json:"carName"`
	Brand   string    `json:"brand"`
	Class   string    `json:"class"`
	Files   int       `json:"files"` // sessions with full laps
	Laps    int       `json:"laps"`  // full laps
	BestMs  int       `json:"bestMs"`
	Last    time.Time `json:"last"`
}

// TeleTrack is one track in the telemetry overview (owner request 2026-11-23: track, then car,
// then setup), with the cars driven there, fastest first.
type TeleTrack struct {
	Track     string             `json:"track"`
	TrackName string             `json:"trackName"`
	Cars      []TeleTrackCar     `json:"cars"`
	Files     int                `json:"files"` // sessions with full laps
	Laps      int                `json:"laps"`
	Bytes     int64              `json:"bytes"`
	BestMs    int                `json:"bestMs"`
	BestCar   string             `json:"bestCar"` // the car of the fastest lap (readable)
	Last      time.Time          `json:"last"`
	Linked    int                `json:"linked"` // sessions with a known setup
	Outline   *telemetry.Outline `json:"outline,omitempty"`
}

// TeleOverview is the telemetry of one game (overview page, D-159 T4).
type TeleOverview struct {
	Game       string         `json:"game"`
	Short      string         `json:"short"`
	Folder     string         `json:"folder"`
	Detected   string         `json:"detected"`
	Error      string         `json:"error,omitempty"`
	Indexing   bool           `json:"indexing"`
	Files      int            `json:"files"`
	Laps       int            `json:"laps"`
	Bytes      int64          `json:"bytes"`
	First      time.Time      `json:"first"`
	Last       time.Time      `json:"last"`
	Linked     int            `json:"linked"`
	Empty      int            `json:"empty"`     // sessions without a full lap (ACC saved no lap markers, or only an out-lap)
	DiskFiles  int            `json:"diskFiles"` // every file in the folder, earlier saves of a session included
	DiskBytes  int64          `json:"diskBytes"`
	ExtraOn    bool           `json:"extraOn"`
	ExtraFiles int            `json:"extraFiles"`
	ExtraBytes int64          `json:"extraBytes"`
	Tracks     []TeleTrack    `json:"tracks"`
	Recent     []TeleFileView `json:"recent"` // the latest sessions with laps
}

// TeleFileView is a telemetry file with readable names.
type TeleFileView struct {
	store.TeleFile
	CarName   string             `json:"carName"`
	Brand     string             `json:"brand"`
	Class     string             `json:"class"`
	TrackName string             `json:"trackName"`
	HasExtra  bool               `json:"hasExtra"`
	Outline   *telemetry.Outline `json:"outline,omitempty"` // overview only
}

// carNames: readable car (with brand and class) and track names.
func (l *Library) teleNames(gameID, car, track string) (carName, brand, class, trackName string) {
	carName, trackName = l.readableNames(gameID, car, track)
	if n, ok := l.game(gameID).(game.Namer); ok {
		cn, b, c := n.CarInfo(car)
		if carName == car {
			carName = cn
		}
		brand, class = b, c
		if trackName == track {
			trackName = n.TrackName(track)
		}
	}
	return
}

// TelemetryGames lists the games that have telemetry files (ACC now; AC later, D-161).
func (l *Library) TelemetryGames() []TeleOverview {
	out := []TeleOverview{}
	for _, g := range l.games {
		if _, ok := g.(game.TelemetryFolder); ok && l.isVisible(g.ID()) {
			ov, err := l.TelemetryOverview(g.ID())
			if err != nil {
				log.Printf("telemetry overview %s: %v", g.ID(), err)
				ov.Error = "SetupMan could not read its telemetry index: " + err.Error()
			}
			out = append(out, ov)
		}
	}
	return out
}

// TelemetryOverview sums up the telemetry of one game per car and track.
func (l *Library) TelemetryOverview(gameID string) (TeleOverview, error) {
	g := l.game(gameID)
	if g == nil {
		return TeleOverview{}, history.ErrNotFound
	}
	ov := TeleOverview{Game: gameID, Short: game.Short(g), Tracks: []TeleTrack{}, Recent: []TeleFileView{}, Folder: l.teleFolder(g),
		ExtraOn: !l.Settings().GameOrDefault(gameID).ExtraLogOff}
	if _, ok := g.(game.ExtraLogger); !ok {
		ov.ExtraOn = false
	}
	if tf, ok := g.(game.TelemetryFolder); ok {
		ov.Detected = tf.TelemetryFolder()
	}
	t := l.tele()
	t.mu.Lock()
	if s := t.states[gameID]; s != nil {
		ov.Indexing, ov.Error = s.indexing, s.err
	}
	t.mu.Unlock()
	if l.db == nil {
		return ov, nil
	}
	files, err := l.db.TelemetryFiles(gameID, "", "")
	if err != nil {
		return ov, err
	}
	byTrack := map[string]*TeleTrack{}
	cars := map[string]map[string]*TeleTrackCar{}
	var keys []string
	for _, f := range files {
		if f.Laps == 0 { // nothing to analyse: counted, not listed
			ov.Empty++
			continue
		}
		k := strings.ToLower(f.Track)
		tt := byTrack[k]
		if tt == nil {
			tt = &TeleTrack{Track: f.Track, Cars: []TeleTrackCar{}}
			_, _, _, tt.TrackName = l.teleNames(gameID, f.Car, f.Track)
			byTrack[k], cars[k] = tt, map[string]*TeleTrackCar{}
			keys = append(keys, k)
		}
		ck := strings.ToLower(f.Car)
		c := cars[k][ck]
		if c == nil {
			c = &TeleTrackCar{Car: f.Car}
			c.CarName, c.Brand, c.Class, _ = l.teleNames(gameID, f.Car, f.Track)
			cars[k][ck] = c
		}
		c.Files++
		c.Laps += f.Laps
		if f.BestMs > 0 && (c.BestMs == 0 || f.BestMs < c.BestMs) {
			c.BestMs = f.BestMs
		}
		if f.StartedAt.After(c.Last) {
			c.Last = f.StartedAt
		}
		tt.Files++
		tt.Laps += f.Laps
		tt.Bytes += f.Size
		if f.BestMs > 0 && (tt.BestMs == 0 || f.BestMs < tt.BestMs) {
			tt.BestMs, tt.BestCar = f.BestMs, c.CarName
		}
		if f.StartedAt.After(tt.Last) {
			tt.Last = f.StartedAt
		}
		if f.SetupID != 0 {
			tt.Linked++
			ov.Linked++
		}
		ov.Files++
		ov.Laps += f.Laps
		ov.Bytes += f.Size
		if ov.First.IsZero() || f.StartedAt.Before(ov.First) {
			ov.First = f.StartedAt
		}
		if f.StartedAt.After(ov.Last) {
			ov.Last = f.StartedAt
		}
	}
	for _, k := range keys {
		tt := byTrack[k]
		for _, c := range cars[k] {
			tt.Cars = append(tt.Cars, *c)
		}
		sort.Slice(tt.Cars, func(i, j int) bool {
			a, b := tt.Cars[i], tt.Cars[j]
			if (a.BestMs == 0) != (b.BestMs == 0) {
				return b.BestMs == 0
			}
			if a.BestMs != b.BestMs {
				return a.BestMs < b.BestMs
			}
			return a.CarName < b.CarName
		})
		ov.Tracks = append(ov.Tracks, *tt)
	}
	sort.SliceStable(ov.Tracks, func(i, j int) bool { return ov.Tracks[i].TrackName < ov.Tracks[j].TrackName })
	ov.DiskFiles, ov.DiskBytes, _ = l.db.TelemetryDisk(gameID)
	for i := range ov.Tracks {
		ov.Tracks[i].Outline = l.outline(gameID, ov.Tracks[i].Track)
	}
	for _, f := range files {
		if len(ov.Recent) == 6 {
			break
		}
		if f.Laps > 0 {
			v := l.teleView(f)
			v.Outline = l.outline(gameID, f.Track)
			ov.Recent = append(ov.Recent, v)
		}
	}
	if l.TelemetryDir != "" {
		if m, _ := filepath.Glob(filepath.Join(l.TelemetryDir, gameID+"-*.smx.gz")); len(m) > 0 {
			for _, p := range m {
				if st, err := os.Stat(p); err == nil {
					ov.ExtraFiles++
					ov.ExtraBytes += st.Size()
				}
			}
		}
	}
	return ov, nil
}

func (l *Library) teleView(f store.TeleFile) TeleFileView {
	v := TeleFileView{TeleFile: f}
	v.CarName, v.Brand, v.Class, v.TrackName = l.teleNames(f.Game, f.Car, f.Track)
	if f.SetupID != 0 {
		if s, _ := l.db.Setup(f.SetupID); s != nil {
			v.SetupName = s.Name
		}
	}
	if f.SessionID != 0 && l.TelemetryDir != "" {
		_, err := os.Stat(telemetry.ExtraPath(l.TelemetryDir, f.Game, f.SessionID))
		v.HasExtra = err == nil
	}
	return v
}

func (l *Library) teleViews(files []store.TeleFile) []TeleFileView {
	out := make([]TeleFileView, 0, len(files))
	for _, f := range files {
		out = append(out, l.teleView(f))
	}
	return out
}

// TelemetryFiles lists the files of a car and track (either may be "").
func (l *Library) TelemetryFiles(gameID, car, track string) ([]TeleFileView, error) {
	if l.db == nil {
		return []TeleFileView{}, nil
	}
	files, err := l.db.TelemetryFiles(gameID, car, track)
	if err != nil {
		return nil, err
	}
	return l.teleViews(files), nil
}

// TelemetryForSetup lists the files driven with a setup (setup page).
func (l *Library) TelemetryForSetup(setupID int64) ([]TeleFileView, error) {
	if l.db == nil {
		return []TeleFileView{}, nil
	}
	files, err := l.db.TelemetryForSetup(setupID)
	if err != nil {
		return nil, err
	}
	return l.teleViews(files), nil
}

// TelemetryForSession lists the files of a recorded session (session page).
func (l *Library) TelemetryForSession(sessionID int64) ([]TeleFileView, error) {
	if l.db == nil {
		return []TeleFileView{}, nil
	}
	files, err := l.db.TelemetryForSession(sessionID)
	if err != nil {
		return nil, err
	}
	return l.teleViews(files), nil
}

// TeleFileDetail is one file with its laps and channels.
type TeleFileDetail struct {
	File      TeleFileView            `json:"file"`
	Laps      []store.TeleLap         `json:"laps"`
	Channels  []telemetry.ChannelInfo `json:"channels"`
	Reference *TeleRef                `json:"reference,omitempty"` // the car and track's reference lap
	Turns     []telemetry.Turn        `json:"turns,omitempty"`     // the track's turns (official numbers where known, D-171)
	Official  bool                    `json:"officialTurns"`       // the turns are the official numbers
}

// TeleRef points at a lap.
type TeleRef struct {
	FileID int64 `json:"fileId"`
	N      int   `json:"n"`
}

// ErrTeleGone: the game's telemetry file of a lap is no longer where SetupMan indexed it.
var ErrTeleGone = errors.New("this session's telemetry file is no longer in the game's telemetry folder")

// teleOpen opens the MoTeC file of an indexed file (read-only).
func (l *Library) teleOpen(f *store.TeleFile) (*motec.File, error) {
	g := l.game(f.Game)
	if g == nil {
		return nil, history.ErrNotFound
	}
	folder := l.teleFolder(g)
	if folder == "" || f.Missing {
		return nil, ErrTeleGone
	}
	lf, err := motec.Open(filepath.Join(folder, f.Name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrTeleGone
	}
	return lf, err
}

// TelemetryFile returns one file with its laps and the channels it holds.
func (l *Library) TelemetryFile(id int64) (*TeleFileDetail, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	f, err := l.db.TelemetryFile(id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, history.ErrNotFound
	}
	laps, err := l.db.TelemetrySessionLaps(id) // with laps only earlier saves of the session still have (D-167)
	if err != nil {
		return nil, err
	}
	d := &TeleFileDetail{File: l.teleView(*f), Laps: laps, Channels: []telemetry.ChannelInfo{}}
	if lf, err := l.teleOpen(f); err == nil {
		d.Channels = telemetry.Channels(lf, l.extraLog(f.Game, f.SessionID))
		lf.Close()
	}
	if fid, n, ok, _ := l.db.TelemetryReference(f.Game, f.Car, f.Track); ok {
		d.Reference = &TeleRef{FileID: fid, N: n}
	}
	d.Turns, d.Official = l.turnsOf(f.Game, f.Track)
	return d, nil
}

// TelemetryLap reads channels of one lap; withMap adds the track map (D-159 T3).
func (l *Library) TelemetryLap(fileID int64, n int, names []string, withMap bool) (*telemetry.LapData, error) {
	defer freeMemory() // a lap with 200 Hz channels briefly needs a few MB; give it back to Windows (as after scans, D-138)
	if l.db == nil {
		return nil, ErrNoHistory
	}
	f, err := l.db.TelemetryFile(fileID)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, history.ErrNotFound
	}
	laps, err := l.db.TelemetryLaps(fileID)
	if err != nil {
		return nil, err
	}
	var lap *store.TeleLap
	for i := range laps {
		if laps[i].N == n {
			lap = &laps[i]
		}
	}
	if lap == nil {
		return nil, history.ErrNotFound
	}
	lf, err := l.teleOpen(f)
	if err != nil {
		return nil, err
	}
	defer lf.Close()
	extra := l.extraLog(f.Game, f.SessionID)
	var xlap *telemetry.ExtraLap
	if extra != nil && lap.Complete {
		if x, ok := extra.FindLap(lap.LapMs); ok {
			xlap = &x
		}
	}
	d, err := telemetry.ReadLap(lf, *lap, names, extra, xlap)
	if err != nil || !withMap {
		return d, err
	}
	m, err := telemetry.MapFromMotion(lf, *lap)
	if err != nil {
		return nil, err
	}
	if m != nil && xlap != nil && len(d.Dist) > 1 {
		dist := func(t float64) float64 { // lap time → metres, from the speed-based distance
			i := (t - d.DistT0) * float64(d.DistHz)
			j := min(max(int(i), 0), len(d.Dist)-1)
			return float64(d.Dist[j])
		}
		if pm := telemetry.MapFromPosition(extra, *xlap, dist, m.Clockwise()); pm != nil {
			m = pm
		}
	}
	d.Map = m
	return d, nil
}

// ErrSetupMismatch: the setup chosen for a telemetry session is for another car or game.
var ErrSetupMismatch = errors.New("that setup is for another car")

// SetTelemetrySetup says which setup a file was driven with. For a file linked to a recorded
// session this changes the session (and moves its laps along, as "Wrong setup?" does, D-154).
func (l *Library) SetTelemetrySetup(fileID, setupID int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	f, err := l.db.TelemetryFile(fileID)
	if err != nil {
		return err
	}
	if f == nil {
		return history.ErrNotFound
	}
	if f.KeptBy != 0 { // an earlier save: the session is shown as (and set on) its kept save
		fileID = f.KeptBy
		if f, err = l.db.TelemetryFile(fileID); err != nil || f == nil {
			return history.ErrNotFound
		}
	}
	var versionID int64
	if setupID != 0 {
		s, err := l.db.Setup(setupID)
		if err != nil || s == nil {
			return history.ErrNotFound
		}
		if s.Game != f.Game || !strings.EqualFold(s.Car, f.Car) {
			return ErrSetupMismatch
		}
		versionID = s.DiskVersionID
		if versionID == 0 {
			if v, _ := l.db.LatestVersion(setupID); v != nil {
				versionID = v.ID
			}
		}
	}
	if f.SessionID != 0 {
		return l.SetSessionSetup(f.SessionID, setupID)
	}
	if err := l.db.SetTelemetrySetup(fileID, setupID, versionID); err != nil {
		return err
	}
	l.publish(Event{Type: "telemetry", Game: f.Game})
	return nil
}

// SetTelemetryNote saves the note of a file (n < 0) or of one lap.
func (l *Library) SetTelemetryNote(fileID int64, n int, note string) error {
	if l.db == nil {
		return ErrNoHistory
	}
	note = strings.TrimSpace(note)
	if r := []rune(note); len(r) > 2000 {
		note = string(r[:2000]) // never half a character
	}
	var err error
	if n < 0 {
		if f, _ := l.db.TelemetryFile(fileID); f != nil && f.KeptBy != 0 {
			fileID = f.KeptBy // the session's note is on the save it is shown as
		}
		err = l.db.SetTelemetryNote(fileID, note)
	} else {
		err = l.db.SetTelemetryLapNote(fileID, n, note)
	}
	if errors.Is(err, store.ErrTeleNotFound) {
		return history.ErrNotFound
	}
	if err == nil {
		l.publish(Event{Type: "telemetry"})
	}
	return err
}

// SetTelemetryReference makes a lap the reference lap of its car and track, or clears it.
func (l *Library) SetTelemetryReference(fileID int64, n int, on bool) error {
	if l.db == nil {
		return ErrNoHistory
	}
	err := l.db.SetTelemetryReference(fileID, n, on)
	if errors.Is(err, store.ErrTeleNotFound) {
		return history.ErrNotFound
	}
	if err == nil {
		l.publish(Event{Type: "telemetry"})
	}
	return err
}

// ReindexTelemetry indexes a game's telemetry folder now (Telemetry page, "Check for new files").
// It also looks for the folder again and restarts its watcher: the folder may have appeared after
// SetupMan started (MoTeC only just turned on in the game), or been deleted and made again.
func (l *Library) ReindexTelemetry(id string) error {
	if l.game(id) == nil {
		return history.ErrNotFound
	}
	t := l.tele()
	var stop func()
	t.mu.Lock()
	if s := t.states[id]; s != nil {
		stop, s.stopWatch, s.folder = s.stopWatch, nil, ""
	}
	t.mu.Unlock()
	if stop != nil {
		stop() // outside the lock (see applyTelemetry)
	}
	l.applyTelemetry()
	go l.teleSync(id)
	return nil
}

// TeleRefLap is the reference lap of a car and track.
type TeleRefLap struct {
	FileID    int64     `json:"fileId"`
	N         int       `json:"n"`
	LapMs     int       `json:"lapMs"`
	StartedAt time.Time `json:"startedAt"`
}

// TeleTrackCarView is one car on the track page, with its reference lap and its setups for the
// track (to say which one a session was driven with).
type TeleTrackCarView struct {
	TeleTrackCar
	Reference *TeleRefLap `json:"reference,omitempty"`
	Setups    []Choice    `json:"setups"`
}

// TeleTrackView is the telemetry of one track, every car (track page; owner request 2026-11-23:
// track, then car, then setup, with laps of different cars and classes side by side).
type TeleTrackView struct {
	Track     string             `json:"track"`
	TrackName string             `json:"trackName"`
	Short     string             `json:"short"`
	Cars      []TeleTrackCarView `json:"cars"`  // fastest first; cars without a full lap last
	Files     []TeleFileView     `json:"files"` // sessions, newest first
	Laps      []TeleLapView      `json:"laps"`  // every full lap, fastest first
	Outline   *telemetry.Outline `json:"outline,omitempty"`
	Turns     []telemetry.Turn   `json:"turns,omitempty"` // the turns (official numbers where known, D-171)
	Official  bool               `json:"officialTurns"`
}

// TelemetryTrack returns the sessions and laps of every car on one track.
func (l *Library) TelemetryTrack(gameID, track string) (*TeleTrackView, error) {
	g := l.game(gameID)
	if g == nil {
		return nil, history.ErrNotFound
	}
	v := &TeleTrackView{Track: track, Short: game.Short(g), Cars: []TeleTrackCarView{}, Files: []TeleFileView{}, Laps: []TeleLapView{}}
	_, _, _, v.TrackName = l.teleNames(gameID, "", track)
	if l.db == nil {
		return v, nil
	}
	files, err := l.TelemetryFiles(gameID, "", track)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, history.ErrNotFound
	}
	v.Files = files
	v.TrackName = files[0].TrackName
	cars := map[string]*TeleTrackCarView{}
	var order []string
	for _, f := range files {
		k := strings.ToLower(f.Car)
		c := cars[k]
		if c == nil {
			c = &TeleTrackCarView{TeleTrackCar: TeleTrackCar{Car: f.Car}, Setups: []Choice{}}
			c.CarName, c.Brand, c.Class, _ = l.teleNames(gameID, f.Car, track)
			cars[k] = c
			order = append(order, k)
		}
		if f.Laps > 0 {
			c.Files++
			c.Laps += f.Laps
			if f.BestMs > 0 && (c.BestMs == 0 || f.BestMs < c.BestMs) {
				c.BestMs = f.BestMs
			}
		}
		if f.StartedAt.After(c.Last) {
			c.Last = f.StartedAt
		}
	}
	names := map[int64]string{}
	for _, k := range order {
		c := cars[k]
		if fid, n, ok, _ := l.db.TelemetryReference(gameID, c.Car, track); ok {
			if laps, err := l.db.TelemetryLaps(fid); err == nil {
				for _, lp := range laps {
					if lp.N == n {
						c.Reference = &TeleRefLap{FileID: fid, N: n, LapMs: lp.LapMs}
					}
				}
			}
			if f, _ := l.db.TelemetryFile(fid); f != nil && c.Reference != nil {
				c.Reference.StartedAt = f.StartedAt
			}
		}
		if setups, err := l.db.SetupsForTrack(gameID, c.Car, track); err == nil {
			for _, s := range setups {
				if s.DeletedAt.IsZero() {
					c.Setups = append(c.Setups, Choice{ID: s.ID, Name: s.Name})
					names[s.ID] = s.Name
				}
			}
			sort.Slice(c.Setups, func(i, j int) bool { return strings.ToLower(c.Setups[i].Name) < strings.ToLower(c.Setups[j].Name) })
		}
		v.Cars = append(v.Cars, *c)
	}
	sort.SliceStable(v.Cars, func(i, j int) bool {
		a, b := v.Cars[i], v.Cars[j]
		if (a.BestMs == 0) != (b.BestMs == 0) {
			return b.BestMs == 0
		}
		return a.BestMs < b.BestMs
	})
	v.Outline = l.outline(gameID, track)
	v.Turns, v.Official = l.turnsOf(gameID, track)
	rows, err := l.db.TelemetryTrackLaps(gameID, "", track)
	if err != nil {
		return nil, err
	}
	v.Laps = make([]TeleLapView, 0, len(rows))
	for _, r := range rows {
		lv := TeleLapView{TeleLapRow: r}
		if c := cars[strings.ToLower(r.Car)]; c != nil {
			lv.CarName, lv.Class = c.CarName, c.Class
		}
		if r.SetupID != 0 {
			if _, ok := names[r.SetupID]; !ok {
				if s, _ := l.db.Setup(r.SetupID); s != nil {
					names[r.SetupID] = s.Name
				}
			}
			lv.SetupName = names[r.SetupID]
		}
		v.Laps = append(v.Laps, lv)
	}
	return v, nil
}

// ---------- track outlines (tiles and headers) ----------

func outlineKey(game, track string) string {
	return "tele-outline/" + game + "/" + strings.ToLower(track)
}

// outlineGap: an outline is drawn only from a lap whose calculated line came back to its start
// within this share of the lap length before closing; a larger gap draws a visibly wrong circuit
// (seen: Brands Hatch as a figure of eight, COTA squashed).
const outlineGap = 0.04

// teleOutlines draws the outline of every track that has none yet, from the fastest of its five
// fastest full laps whose line closes well. It reads a few laps per track, once; the result is
// kept in the library.
func (l *Library) teleOutlines(id string) {
	cands, err := l.db.TelemetryBestPerTrack(id, 5)
	if err != nil {
		return
	}
	byTrack := map[string][]store.TeleBest{}
	var tracks []string
	for _, b := range cands {
		if byTrack[b.Track] == nil {
			tracks = append(tracks, b.Track)
		}
		byTrack[b.Track] = append(byTrack[b.Track], b)
	}
	made := 0
	for _, track := range tracks {
		if v, _ := l.db.GetKV(outlineKey(id, track)); v != "" {
			continue
		}
		var o *telemetry.Outline
		for _, b := range byTrack[track] {
			if l.Live() == game.LiveDriving {
				return
			}
			if m := l.lapMap(b.FileID, b.N); m != nil && m.Gap <= outlineGap {
				o = telemetry.MakeOutline(m, 160)
				break
			}
		}
		if o == nil {
			continue
		}
		if js, err := json.Marshal(o); err == nil {
			l.db.SetKV(outlineKey(id, track), string(js))
			made++
		}
	}
	if made > 0 {
		t := l.tele()
		t.cacheMu.Lock()
		t.outlines = nil // read again
		t.cacheMu.Unlock()
		l.publish(Event{Type: "telemetry", Game: id})
	}
}

// outline returns the outline of a track (nil when not drawn yet).
func (l *Library) outline(game, track string) *telemetry.Outline {
	t := l.tele()
	t.cacheMu.Lock()
	defer t.cacheMu.Unlock()
	if t.outlines == nil {
		t.outlines = map[string]*telemetry.Outline{}
	}
	k := outlineKey(game, track)
	if o, ok := t.outlines[k]; ok {
		return o
	}
	var o *telemetry.Outline
	if v, _ := l.db.GetKV(k); v != "" {
		o = &telemetry.Outline{}
		if json.Unmarshal([]byte(v), o) != nil {
			o = nil
		}
	}
	if o != nil { // only outlines: a track without one (or any name sent to the API) is not kept
		t.outlines[k] = o
	}
	return o
}

// TeleStint is the brakes over a session (D-170): every timed lap with its pad and disc life and
// brake temperatures.
type TeleStint struct {
	Wheels   [4]string            `json:"wheels"`
	Laps     []telemetry.StintLap `json:"laps"`
	HasExtra bool                 `json:"hasExtra"` // SetupMan's extra log was running (pad and disc life)
}

// TelemetryStint returns the brakes over a session: pad and disc life per lap from the extra log,
// brake temperatures from the extra log or, for laps it doesn't cover, from the MoTeC file.
func (l *Library) TelemetryStint(fileID int64) (*TeleStint, error) {
	defer freeMemory()
	if l.db == nil {
		return nil, ErrNoHistory
	}
	f, err := l.db.TelemetryFile(fileID)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, history.ErrNotFound
	}
	if f.KeptBy != 0 { // the session as shown
		fileID = f.KeptBy
		if f, err = l.db.TelemetryFile(fileID); err != nil || f == nil {
			return nil, history.ErrNotFound
		}
	}
	laps, err := l.db.TelemetrySessionLaps(fileID)
	if err != nil {
		return nil, err
	}
	extra := l.extraLog(f.Game, f.SessionID)
	st := &TeleStint{Wheels: telemetry.Wheels, Laps: telemetry.Stint(laps, extra), HasExtra: extra != nil}
	// brake temperatures from the MoTeC files for laps the extra log has none for (one file at a time)
	byFile := map[int64][]int{}
	for i, s := range st.Laps {
		if s.Temp == nil && s.Data {
			byFile[s.FileID] = append(byFile[s.FileID], i)
		}
	}
	names := []string{"BRAKE_TEMP_LF", "BRAKE_TEMP_RF", "BRAKE_TEMP_LR", "BRAKE_TEMP_RR"} // FL FR RL RR
	byN := map[[2]int64]store.TeleLap{}
	for _, lp := range laps {
		byN[[2]int64{lp.FileID, int64(lp.N)}] = lp
	}
	for id, idx := range byFile {
		df, err := l.db.TelemetryFile(id)
		if err != nil || df == nil {
			continue
		}
		lf, err := l.teleOpen(df)
		if err != nil {
			continue
		}
		for _, i := range idx {
			s := &st.Laps[i]
			d, err := telemetry.ReadLap(lf, byN[[2]int64{s.FileID, int64(s.N)}], names, nil, nil)
			if err != nil || len(d.Channels) != 4 {
				continue
			}
			s.Temp, s.TempMax = make([]float32, 4), make([]float32, 4)
			for w, c := range d.Channels {
				sum, hi := 0.0, float32(0)
				for _, v := range c.Data {
					sum += float64(v)
					hi = max(hi, v)
				}
				if len(c.Data) > 0 {
					s.Temp[w], s.TempMax[w] = float32(sum/float64(len(c.Data))), hi
				}
			}
		}
		lf.Close()
	}
	return st, nil
}

// trackTurns returns where a track's turns are (share of the lap), worked out once from its seven
// fastest full laps (any car) and kept in the library; worked out again while fewer laps were
// known. nil when there are too few laps: the analysis page then finds turns on its first lap.
// turnsOf returns a track's turns: the official numbers when SetupMan has them (D-171), else its
// own count of the track's slow points.
func (l *Library) turnsOf(gameID, track string) ([]telemetry.Turn, bool) {
	if t := telemetry.OfficialTurns(track); t != nil {
		return t, true
	}
	return telemetry.CountedTurns(l.trackTurns(gameID, track)), false
}

func (l *Library) trackTurns(gameID, track string) []float64 {
	const use = 7
	key := "tele-turns/" + gameID + "/" + strings.ToLower(track)
	var kept struct {
		Tried int       `json:"tried"` // full laps there were to read (some may not have been readable)
		Turns []float64 `json:"turns"`
	}
	if v, _ := l.db.GetKV(key); v != "" && json.Unmarshal([]byte(v), &kept) == nil && kept.Tried >= use {
		return kept.Turns
	}
	rows, err := l.db.TelemetryTrackLaps(gameID, "", track)
	if err != nil || len(rows) < 3 || (kept.Tried > 0 && min(len(rows), use) <= kept.Tried) {
		return kept.Turns
	}
	var laps []telemetry.LapSpeed
	for _, r := range rows[:min(len(rows), use)] {
		f, err := l.db.TelemetryFile(r.FileID)
		if err != nil || f == nil {
			continue
		}
		lf, err := l.teleOpen(f)
		if err != nil {
			continue
		}
		d, err := telemetry.ReadLap(lf, r.TeleLap, []string{"SPEED"}, nil, nil)
		lf.Close()
		if err != nil || len(d.Channels) == 0 || len(d.Channels[0].Data) != len(d.Dist) {
			continue
		}
		laps = append(laps, telemetry.LapSpeed{Speed: d.Channels[0].Data, Dist: d.Dist})
	}
	turns := telemetry.FindTurns(laps)
	if turns == nil {
		return kept.Turns
	}
	kept.Tried, kept.Turns = min(len(rows), use), turns
	if js, err := json.Marshal(kept); err == nil {
		l.db.SetKV(key, string(js))
	}
	return turns
}

// lapMap calculates the line of one lap from its MoTeC file (nil when it can't).
func (l *Library) lapMap(fileID int64, n int) *telemetry.TrackMap {
	f, err := l.db.TelemetryFile(fileID)
	if err != nil || f == nil {
		return nil
	}
	laps, err := l.db.TelemetryLaps(fileID)
	if err != nil {
		return nil
	}
	for _, lp := range laps {
		if lp.N != n {
			continue
		}
		lf, err := l.teleOpen(f)
		if err != nil {
			return nil
		}
		defer lf.Close()
		m, _ := telemetry.MapFromMotion(lf, lp)
		return m
	}
	return nil
}

// TeleLapView is a full lap with its car's and setup's names.
type TeleLapView struct {
	store.TeleLapRow
	CarName   string `json:"carName"`
	Class     string `json:"class"`
	SetupName string `json:"setupName,omitempty"`
}
