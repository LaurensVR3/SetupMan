//go:build !windows

package acc

import (
	"errors"

	"setupman/internal/game"
)

// OpenExtra: ACC's shared memory exists only on Windows.
func (g *Game) OpenExtra() (game.ExtraReader, error) { return nil, errors.New("not supported") }
