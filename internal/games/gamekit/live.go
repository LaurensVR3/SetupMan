package gamekit

import (
	"sync"
	"sync/atomic"
	"time"

	"setupman/internal/game"
)

// ProcessLive tells whether a game runs by looking for its program every few seconds. It can't
// tell driving from the menus, so it only ever reports "running, not driving" or "not running".
// Used for games whose live data SetupMan doesn't read (yet).
type ProcessLive struct {
	Exes  []string // program file names, e.g. "acs.exe"
	once  sync.Once
	state atomic.Value // game.LiveState
}

const processCheckEvery = 5 * time.Second

// Live returns the latest state; the background check starts on first use.
func (p *ProcessLive) Live() game.LiveState {
	p.once.Do(func() {
		p.state.Store(p.check())
		go func() {
			for {
				time.Sleep(processCheckEvery)
				p.state.Store(p.check())
			}
		}()
	})
	return p.state.Load().(game.LiveState)
}

func (p *ProcessLive) check() game.LiveState {
	if Running(p.Exes...) {
		return game.LiveNotDriving
	}
	return game.LiveUnknown
}
