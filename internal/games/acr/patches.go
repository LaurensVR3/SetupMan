package acr

import (
	"time"

	"setupman/internal/game"
)

// Patches are Assetto Corsa Rally's PC releases (spec D-103), oldest first, as provided by the
// user on 2026-11-19. The unnumbered launch hotfix (14 Nov 2025) is left out.
var patches = []game.Patch{
	{Version: "0.2", Date: day(2025, 12, 18)},
	{Version: "0.3", Date: day(2026, 2, 26)},
	{Version: "0.3.1", Date: day(2026, 3, 4)},
	{Version: "0.4", Date: day(2026, 4, 30)},
	{Version: "0.4.1", Date: day(2026, 5, 7)},
	{Version: "0.5", Date: day(2026, 6, 29)},
	{Version: "0.5.1", Date: day(2026, 7, 16)},
	{Version: "0.6", Date: day(2026, 9, 10)},
	{Version: "0.7", Date: day(2026, 11, 19)},
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Patches returns Assetto Corsa Rally's known PC patches.
func (g *Game) Patches() []game.Patch { return append([]game.Patch(nil), patches...) }
