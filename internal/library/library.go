// Package library keeps the latest scan of every game in memory, rescans
// automatically when a setup folder changes (but not while the player is driving),
// and tells listeners (the web UI) about updates. Spec D-31, D-32, D-37, D-38.
package library

import (
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/settings"
	"setupman/internal/store"
	"setupman/internal/watch"
)

// Event is sent to listeners when something changes.
type Event struct {
	Type string         `json:"type"`           // "game-updated", "games-changed" or "live"
	Game string         `json:"game,omitempty"` // for "game-updated"
	Live game.LiveState `json:"live,omitempty"` // for "live"
	// LiveGame is the short name of the game that runs (e.g. "ACC"), for "live".
	LiveGame string `json:"liveGame,omitempty"`
}

// Library is the in-memory state of all games.
type Library struct {
	games []game.Game
	store *settings.Store
	db    *store.Store // version history (spec D-41); may be nil

	// Debounce: rescan this long after the last change in a folder.
	Debounce time.Duration
	// LiveEvery: how often to check whether the player is driving.
	LiveEvery time.Duration

	// Backups makes and restores ACC setups folder backups (Phase 3); set by main.
	Backups *folderbackup.Store

	// folderMu: only one thing at a time reads or changes a game folder (a scan never
	// sees a half-finished staging change).
	folderMu sync.Mutex

	mu       sync.RWMutex
	settings settings.Settings
	states   map[string]*state
	live     game.LiveState
	liveGame string                    // short name of the running game, "" when none
	gameLive map[string]game.LiveState // per game, for the rig-test log (D-140)

	subsMu sync.Mutex
	subs   map[chan Event]struct{}

	// recording laps (Phase 4): one recorder per game
	recMu sync.Mutex
	recs  map[string]*recorder
	// resumeTried: per game, whether the first session after SetupMan started was checked for a
	// session to continue (A-5)
	resumeTried map[string]bool

	// files SetupMan put in a game folder, with the last-opened time it left on them (D-147): until
	// that time changes, the game has not opened the file
	touchMu  sync.Mutex
	ownTouch map[string]time.Time

	// TelemetryDir is where SetupMan keeps its extra telemetry logs (%APPDATA%\SetupMan\telemetry,
	// D-162); set by main. "" = no extra log.
	TelemetryDir string
	teleOnce     sync.Once
	teleD        *teleData
}

type state struct {
	det       game.Detection
	cfg       settings.Game
	folder    string // folder in use ("" = none)
	visible   bool
	scan      *game.Scan
	err       string // last scan error
	scanning  bool
	pending   bool    // a change arrived while driving
	newSetups []int64 // setups that just appeared (for optional auto-renaming)
	timer     *time.Timer
	stopWatch func()
}

// New creates a library for the given games. db is the version-history database
// (nil = no history, used in some tests).
func New(games []game.Game, st *settings.Store, db *store.Store) *Library {
	l := &Library{
		games:     games,
		store:     st,
		db:        db,
		Debounce:  2 * time.Second,
		LiveEvery: 2 * time.Second,
		states:    map[string]*state{},
		live:      game.LiveUnknown,
		gameLive:  map[string]game.LiveState{},
		subs:      map[chan Event]struct{}{},
		recs:      map[string]*recorder{},
	}
	for _, g := range games {
		l.states[g.ID()] = &state{}
	}
	return l
}

// Start applies the settings (detects games, scans, starts watching) and starts
// the driving check. Call once.
func (l *Library) Start() {
	l.purgeBin()              // setups deleted more than 30 days ago (D-102)
	l.migrateManagedFolders() // settings from before D-115
	l.Apply()
	go l.liveLoop()
}

// Settings returns the settings currently in use.
func (l *Library) Settings() settings.Settings {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings
}

// Apply re-reads the settings and detection results. Games whose folder or
// visibility changed are rescanned; watchers are (re)started as needed.
func (l *Library) Apply() {
	st := l.store.Load()
	var rescan []string
	var stops []func() // called after unlocking: a watcher's callback takes l.mu, and stopping waits for it

	l.mu.Lock()
	l.settings = st
	for _, g := range l.games {
		s := l.states[g.ID()]
		s.det = g.Detect()
		s.cfg = st.GameOrDefault(g.ID())
		folder := s.cfg.Folder
		if folder == "" {
			folder = s.det.Folder
		}
		visible := s.cfg.Visibility == settings.VisShow || (s.cfg.Visibility == settings.VisAuto && s.det.Installed)
		if !visible {
			folder = "" // hidden games are not scanned or watched
		}
		if folder == s.folder && visible == s.visible && (s.scan != nil || s.err != "" || folder == "") {
			continue
		}
		if s.stopWatch != nil {
			stops = append(stops, s.stopWatch)
			s.stopWatch = nil
		}
		s.folder, s.visible, s.scan, s.err = folder, visible, nil, ""
		if folder != "" {
			id := g.ID()
			if stop, err := watch.Watch(folder, func() { l.changed(id) }); err == nil {
				s.stopWatch = stop
			}
			rescan = append(rescan, id)
		}
	}
	l.mu.Unlock()
	for _, stop := range stops {
		stop()
	}

	for _, id := range rescan {
		l.Rescan(id)
	}
	// Settings may have changed the endurance limit: re-guess sessions not set by hand (D-69).
	if l.db != nil {
		for _, g := range l.games {
			if ok, _ := l.db.Imported(g.ID()); ok {
				if err := history.RefreshSessions(l.db, g.ID(), l.historyConfig(g.ID())); err != nil {
					log.Printf("sessions for %s: %v", g.ID(), err)
				}
			}
		}
	}
	l.applyTelemetry() // telemetry folder watchers (D-160)
	l.publish(Event{Type: "games-changed"})
}

// Stop ends all watchers, then waits until a scan or folder change that is running has finished,
// so SetupMan never quits halfway through changing a game folder (D-72, D-138). Nothing touches a
// game folder after Stop: call it only when SetupMan quits.
func (l *Library) Stop() {
	var stops []func()
	l.mu.Lock()
	for _, s := range l.states {
		if s.stopWatch != nil {
			stops = append(stops, s.stopWatch)
			s.stopWatch = nil
		}
		if s.timer != nil {
			s.timer.Stop()
		}
	}
	l.mu.Unlock()
	for _, stop := range stops { // outside the lock: a watcher's callback takes l.mu (see Apply)
		stop()
	}
	l.stopTelemetry() // closes the extra telemetry log properly
	l.folderMu.Lock() // held on purpose: no new folder change starts while SetupMan quits
}

// Rescan scans a game's folder now.
func (l *Library) Rescan(id string) {
	g := l.game(id)
	if g == nil {
		return
	}
	l.mu.Lock()
	s := l.states[id]
	folder := s.folder
	s.scanning, s.pending = true, false
	l.mu.Unlock()
	if folder == "" {
		l.mu.Lock()
		s.scanning = false
		l.mu.Unlock()
		return
	}

	l.folderMu.Lock()
	res, err := g.Scan(folder) // slow part, outside the state lock
	if err == nil && l.db != nil {
		// Record versions, branches, renames and removals in the library (never in the game folder).
		cfg := l.historyConfig(id)
		hr, herr := history.Sync(l.db, id, res, g.ParseSetup, cfg)
		if herr == nil {
			logScan(g, hr, cfg.GameRunning)
		}
		if herr != nil {
			log.Printf("history for %s: %v", id, herr)
		} else if len(hr.NewSetupIDs) > 0 && cfg.GameRunning {
			// Only files that appear while the game runs are saves from the game and may be
			// renamed automatically; files copied in while it is closed keep their names (D-114).
			l.mu.Lock()
			s.newSetups = append(s.newSetups, hr.NewSetupIDs...)
			l.mu.Unlock()
		}
	}
	l.folderMu.Unlock()

	l.mu.Lock()
	s.scanning = false
	if s.folder == folder { // ignore results for a folder that was changed meanwhile
		if err != nil {
			s.scan, s.err = nil, err.Error()
		} else {
			s.scan, s.err = res, ""
		}
	}
	l.mu.Unlock()
	l.publish(Event{Type: "game-updated", Game: id})
	freeMemory()
}

var freeing atomic.Bool

// freeMemory gives the memory a scan no longer needs back to Windows, so SetupMan's memory in Task
// Manager stays close to what it really uses (a scan of ~900 setups holds about 30 MB, but briefly
// needs several times that, D-138). Scans are rare (after a folder change, never while driving),
// so the extra clean-up costs nothing noticeable.
func freeMemory() {
	if freeing.CompareAndSwap(false, true) {
		go func() {
			defer freeing.Store(false)
			debug.FreeOSMemory()
		}()
	}
}

// changed is called by a watcher; it rescans after a quiet period, unless driving.
func (l *Library) changed(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.states[id]
	if s.timer != nil {
		s.timer.Reset(l.Debounce)
		return
	}
	s.timer = time.AfterFunc(l.Debounce, func() {
		defer guard("rescan after a folder change")
		l.mu.Lock()
		s.timer = nil
		driving := l.live == game.LiveDriving
		if driving {
			if !s.pending {
				log.Printf("folder %s changed while driving: it is read when you stop driving", id)
			}
			s.pending = true
		}
		l.mu.Unlock()
		if !driving {
			l.Rescan(id)
			l.autoRename(id) // optional: tidy names for new in-game saves
		}
	})
}

// liveLoop checks every few seconds whether the player is driving in any visible game.
// When driving stops, changes that arrived meanwhile are scanned.
func (l *Library) liveLoop() {
	prev := game.LiveUnknown
	for {
		prev = l.liveStep(prev)
		time.Sleep(l.LiveEvery)
	}
}

// liveStep is one round of liveLoop. An unexpected error is logged and the loop goes on, so lap
// recording never stops in the middle of a race.
func (l *Library) liveStep(prev game.LiveState) (now game.LiveState) {
	now = prev
	defer guard("live check")
	now = l.checkLive()
	l.recordTick() // record laps per setup version (D-44, D-48)
	if now != game.LiveDriving {
		for _, id := range l.pendingGames() {
			l.Rescan(id)
			l.autoRename(id)
		}
		l.runPendingOps() // staging changes queued while driving (D-79)
	}
	l.telemetryTick(now) // telemetry files that arrived while driving (D-160)
	if prev != game.LiveUnknown && now == game.LiveUnknown {
		l.tidyOnGameClose() // the game was closed (D-80, optional)
	}
	return now
}

// guard logs an unexpected error (a bug) in a background task instead of stopping SetupMan.
// Use as: defer guard("what was being done").
func guard(what string) {
	if r := recover(); r != nil {
		log.Printf("%s: unexpected error: %v\n%s", what, r, debug.Stack())
	}
}

func (l *Library) checkLive() game.LiveState {
	state, name := game.LiveUnknown, ""
	for _, g := range l.games {
		lr, ok := g.(game.LiveReader)
		if !ok || !l.isVisible(g.ID()) {
			continue
		}
		st := lr.Live()
		l.mu.Lock()
		prev, seen := l.gameLive[g.ID()]
		l.gameLive[g.ID()] = st
		l.mu.Unlock()
		if seen && prev != st || !seen && st != game.LiveUnknown {
			logLiveChange(g, prev, st)
		}
		switch st {
		case game.LiveDriving:
			state, name = game.LiveDriving, game.Short(g)
		case game.LiveNotDriving:
			if state == game.LiveUnknown {
				state, name = game.LiveNotDriving, game.Short(g)
			}
		}
	}
	l.mu.Lock()
	changed := state != l.live || name != l.liveGame
	l.live, l.liveGame = state, name
	l.mu.Unlock()
	if changed {
		l.publish(Event{Type: "live", Live: state, LiveGame: name})
	}
	return state
}

func (l *Library) pendingGames() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []string
	for id, s := range l.states {
		if s.pending {
			s.pending = false
			ids = append(ids, id)
		}
	}
	return ids
}

func (l *Library) isVisible(id string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.states[id].visible
}

func (l *Library) game(id string) game.Game {
	for _, g := range l.games {
		if g.ID() == id {
			return g
		}
	}
	return nil
}

// Live returns whether the player is driving right now.
// LiveEvent is the current game state as an event (sent to a page when it connects).
func (l *Library) LiveEvent() Event {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Event{Type: "live", Live: l.live, LiveGame: l.liveGame}
}

func (l *Library) Live() game.LiveState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.live
}

// Notify sends an event to all listeners (e.g. "autostart" when Start with Windows changed).
func (l *Library) Notify(e Event) { l.publish(e) }

// Subscribe returns a channel of events; call cancel when done.
func (l *Library) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	l.subsMu.Lock()
	l.subs[ch] = struct{}{}
	l.subsMu.Unlock()
	return ch, func() {
		l.subsMu.Lock()
		delete(l.subs, ch)
		l.subsMu.Unlock()
	}
}

func (l *Library) publish(e Event) {
	l.subsMu.Lock()
	defer l.subsMu.Unlock()
	for ch := range l.subs {
		select {
		case ch <- e:
		default: // a slow listener misses an event; it refreshes on the next one
		}
	}
}
