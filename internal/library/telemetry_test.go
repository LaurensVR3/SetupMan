package library

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"setupman/internal/game"
	"setupman/internal/settings"
	"setupman/internal/telemetry"
)

// extraGame is a fake game with an extra telemetry log (D-162).
type extraGame struct {
	fakeGame
	mu     sync.Mutex
	opened int
	closed int
	reads  int
}

func (g *extraGame) ExtraChannels() []game.ExtraChannel {
	return []game.ExtraChannel{{Name: "LAP"}, {Name: "LAP_TIME", Unit: "ms"}, {Name: "LAST_LAP", Unit: "ms"}, {Name: "FUEL", Unit: "l"}}
}

func (g *extraGame) OpenExtra() (game.ExtraReader, error) {
	g.mu.Lock()
	g.opened++
	g.mu.Unlock()
	return &extraFake{g: g}, nil
}

type extraFake struct {
	g *extraGame
	n int
}

func (r *extraFake) Read(dst []float32) (bool, bool) {
	r.n++
	r.g.mu.Lock()
	r.g.reads++
	r.g.mu.Unlock()
	dst[0], dst[1], dst[2], dst[3] = float32(r.n/5), float32(r.n%5)*100, 90000, 50-float32(r.n)*0.01
	return r.n%7 != 0, true // now and then a reading in the pause menu (not written)
}

func (r *extraFake) Close() {
	r.g.mu.Lock()
	r.g.closed++
	r.g.mu.Unlock()
}

func TestExtraLog(t *testing.T) {
	g := &extraGame{}
	l, _ := newLib(t, &g.fakeGame)
	l.games = []game.Game{g}
	l.TelemetryDir = t.TempDir()
	l.startExtra(g, 42)
	p := telemetry.ExtraPath(l.TelemetryDir, "acc", 42)
	// wait for readings rather than a fixed time: GitHub's machines can be slow (the test once
	// stopped the log before a single row was written)
	waitFor(t, "readings", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.reads >= 6 })
	l.stopExtra("acc")
	waitFor(t, "the reader to close", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.closed == 1 })
	var x *telemetry.ExtraLog
	waitFor(t, "the closed log", func() bool { // the writer closes right after the reader
		var err error
		x, err = telemetry.ReadExtra(p)
		return err == nil && x.Complete && len(x.T) >= 3
	})
	if len(x.T) < 3 || x.Header.Session != 42 || x.Index("FUEL") != 3 {
		t.Fatalf("%d rows, header %+v", len(x.T), x.Header)
	}
	if x.T[1] <= x.T[0] {
		t.Errorf("times should go up: %v", x.T[:2])
	}
}

func TestExtraLogOff(t *testing.T) {
	g := &extraGame{}
	l, st := newLib(t, &g.fakeGame)
	l.games = []game.Game{g}
	l.TelemetryDir = t.TempDir()
	s := st.Load()
	cfg := s.GameOrDefault("acc")
	cfg.ExtraLogOff = true
	if s.Games == nil {
		s.Games = map[string]settings.Game{}
	}
	s.Games["acc"] = cfg
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	l.Apply()
	l.startExtra(g, 7)
	time.Sleep(250 * time.Millisecond)
	if m, _ := filepath.Glob(filepath.Join(l.TelemetryDir, "*")); len(m) != 0 || g.opened != 0 {
		t.Fatalf("no extra log expected when it is disabled: %v", m)
	}
}
