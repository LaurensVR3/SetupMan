package library

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"setupman/internal/game"
	"setupman/internal/games/acc"
	"setupman/internal/settings"
	"setupman/internal/setup"
	histstore "setupman/internal/store"
)

// fakeGame wraps the real ACC scanner but lets tests control detection and live state.
type fakeGame struct {
	mu        sync.Mutex
	installed bool
	folder    string
	live      game.LiveState
	scans     int
}

func (f *fakeGame) ID() string   { return "acc" }
func (f *fakeGame) Name() string { return "Assetto Corsa Competizione" }
func (f *fakeGame) Detect() game.Detection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return game.Detection{Installed: f.installed, Folder: f.folder}
}
func (f *fakeGame) Scan(folder string) (*game.Scan, error) {
	f.mu.Lock()
	f.scans++
	f.mu.Unlock()
	return acc.ScanFolder(folder)
}
func (f *fakeGame) ParseSetup(content []byte) ([]setup.Field, error) {
	return acc.New().ParseSetup(content)
}
func (f *fakeGame) EditSetup(content []byte, edits map[string]string) ([]byte, error) {
	return acc.Edit(content, edits)
}
func (f *fakeGame) MatchSetup(t game.Telemetry, candidates [][]byte) []int {
	return acc.New().MatchSetup(t, candidates)
}
func (f *fakeGame) Live() game.LiveState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}
func (f *fakeGame) scanCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.scans }

func setupFolder(t *testing.T) string {
	t.Helper()
	base, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Setups")
	track := filepath.Join(dir, "ferrari_296_gt3", "Spa")
	os.MkdirAll(track, 0o755)
	os.WriteFile(filepath.Join(track, "a.json"), base, 0o644)
	os.WriteFile(filepath.Join(track, "b.json"), base, 0o644)
	os.MkdirAll(filepath.Join(dir, "bmw_m4_gt4", "Monza"), 0o755)
	os.WriteFile(filepath.Join(dir, "bmw_m4_gt4", "Monza", "m.json"), base, 0o644)
	return dir
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newLib(t *testing.T, g *fakeGame) (*Library, *settings.Store) {
	t.Helper()
	store := settings.NewStore(filepath.Join(t.TempDir(), "settings.json"))
	db, err := histstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l := New([]game.Game{g}, store, db)
	l.Debounce = 50 * time.Millisecond
	l.LiveEvery = 20 * time.Millisecond
	t.Cleanup(l.Stop)
	return l, store
}

func TestVisibilityAndViews(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{folder: folder} // not installed
	l, store := newLib(t, g)
	l.Apply()
	if s := l.Games()[0]; s.Visible || s.Status != "hidden" {
		t.Fatalf("not installed + auto should be hidden: %+v", s)
	}

	// Override in settings (spec D-37).
	st := store.Load()
	st.Games["acc"] = settings.Game{Visibility: settings.VisShow}
	store.Save(st)
	l.Apply()
	s := l.Games()[0]
	if !s.Visible || s.Status != "ok" || s.Cars != 2 || s.Tracks != 2 || s.Setups != 3 || s.Duplicates != 1 {
		t.Fatalf("summary wrong: %+v", s)
	}

	d, ok := l.Game("acc")
	if !ok || len(d.Classes) != 2 || d.Classes[0].Class != "GT3" || d.Classes[1].Class != "GT4" {
		t.Fatalf("classes wrong: %+v", d.Classes)
	}
	if d.Classes[0].Cars[0].Name != "Ferrari 296 GT3" || d.Classes[0].Cars[0].Duplicates != 1 {
		t.Errorf("car summary wrong: %+v", d.Classes[0].Cars[0])
	}
	c, ok := l.Car("acc", "ferrari_296_gt3")
	if !ok || len(c.Tracks) != 1 || c.Tracks[0].Name != "Spa-Francorchamps" {
		t.Fatalf("car detail wrong: %+v", c)
	}
	tr, ok := l.Track("acc", "ferrari_296_gt3", "spa")
	if !ok || len(tr.Setups) != 2 || len(tr.Clusters.Identical) != 1 || tr.Threshold != 95 {
		t.Fatalf("track detail wrong: %+v", tr)
	}
	if _, ok := l.Track("acc", "ferrari_296_gt3", "nowhere"); ok {
		t.Error("unknown track found")
	}
}

func TestAutoRescanPausedWhileDriving(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveNotDriving}
	l, _ := newLib(t, g)
	events, cancel := l.Subscribe()
	defer cancel()
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	start := g.scanCount()

	// A new file while not driving: rescanned after the quiet period.
	os.WriteFile(filepath.Join(folder, "ferrari_296_gt3", "Spa", "c.json"), []byte(`{"carName":"x"}`), 0o644)
	waitFor(t, "rescan", func() bool { return g.scanCount() > start })

	// While driving: changes wait until driving stops.
	g.mu.Lock()
	g.live = game.LiveDriving
	g.mu.Unlock()
	waitFor(t, "live=driving", func() bool { return l.Live() == game.LiveDriving })
	n := g.scanCount()
	os.WriteFile(filepath.Join(folder, "ferrari_296_gt3", "Spa", "d.json"), []byte(`{"carName":"x"}`), 0o644)
	time.Sleep(300 * time.Millisecond)
	if g.scanCount() != n {
		t.Fatal("scanned while driving")
	}
	g.mu.Lock()
	g.live = game.LiveNotDriving
	g.mu.Unlock()
	waitFor(t, "pending rescan after driving", func() bool { return g.scanCount() > n })

	sawLive, sawUpdate := false, false
	for len(events) > 0 {
		e := <-events
		sawLive = sawLive || e.Type == "live"
		sawUpdate = sawUpdate || e.Type == "game-updated"
	}
	if !sawLive || !sawUpdate {
		t.Errorf("events missing: live=%v update=%v", sawLive, sawUpdate)
	}
}

// setLive sets the fake game's live state and waits until the library has seen it.
func setLive(t *testing.T, l *Library, g *fakeGame, state game.LiveState) {
	t.Helper()
	g.mu.Lock()
	g.live = state
	g.mu.Unlock()
	waitFor(t, "live state "+string(state), func() bool { return l.Live() == state })
}
