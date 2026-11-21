package library

// Recognising the setup being driven from the moment its file was last opened (spec D-147).
// When a game loads a setup it reads the file, and Windows records that moment (the file's "last
// access" time). The setup the game opened most recently, since the game started, is the one being
// driven. SetupMan's own reads leave no such trace (internal/lastopened), and files SetupMan put in
// the folder itself are remembered until the game opens them. Nothing here writes to a game folder.

import (
	"os"
	"path/filepath"
	"time"

	"setupman/internal/game"
	"setupman/internal/lastopened"
)

// openedTie: files opened within this time of the newest one count as opened together (a game
// that reads several setups at once, e.g. to list them). The values then decide among them.
const openedTie = 2 * time.Second

// noteOwnTouch remembers the last-opened time SetupMan left on a file it wrote into a game folder.
func (l *Library) noteOwnTouch(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	at, ok := lastopened.Time(fi)
	if !ok {
		return
	}
	l.touchMu.Lock()
	if l.ownTouch == nil {
		l.ownTouch = map[string]time.Time{}
	}
	l.ownTouch[filepath.Clean(path)] = at
	l.touchMu.Unlock()
}

// openedSince returns, per file, when the game opened it, or the zero time when it was not opened
// since the game started (or SetupMan can't tell: last-opened times are off, or the file is gone).
func (l *Library) openedSince(start time.Time, paths []string) []time.Time {
	out := make([]time.Time, len(paths))
	l.touchMu.Lock()
	defer l.touchMu.Unlock()
	for i, p := range paths {
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		at, ok := lastopened.Time(fi)
		if !ok || (!start.IsZero() && !at.After(start)) {
			continue
		}
		key := filepath.Clean(p)
		if own, ok := l.ownTouch[key]; ok {
			if own.Equal(at) {
				continue // SetupMan put it there and the game hasn't opened it since
			}
			delete(l.ownTouch, key)
		}
		out[i] = at
	}
	return out
}

// newestOpened returns the indexes of the file opened last, plus any opened together with it
// (within openedTie). Empty when none was opened.
func newestOpened(times []time.Time) []int {
	var newest time.Time
	for _, t := range times {
		if t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return nil
	}
	var out []int
	for i, t := range times {
		if !t.IsZero() && newest.Sub(t) <= openedTie {
			out = append(out, i)
		}
	}
	return out
}

// gameStart is when the game's program started, or the zero time when unknown.
func gameStart(g game.Game) time.Time {
	if st, ok := g.(game.StartTimer); ok {
		if t, ok := st.StartedAt(); ok {
			return t
		}
	}
	return time.Time{}
}
