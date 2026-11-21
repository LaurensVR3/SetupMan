package ace

import (
	"time"

	"setupman/internal/game"
)

// Patches are Assetto Corsa EVO's PC releases (spec D-103), oldest first, as provided by the
// user on 2026-11-19.
var patches = []game.Patch{
	{Version: "0.1.1", Date: day(2025, 1, 16)},
	{Version: "0.1.2", Date: day(2025, 1, 17)},
	{Version: "0.1.3", Date: day(2025, 1, 20)},
	{Version: "0.1.4", Date: day(2025, 1, 29)},
	{Version: "0.1.5", Date: day(2025, 2, 5)},
	{Version: "0.1.5.1", Date: day(2025, 2, 12)},
	{Version: "0.1.6", Date: day(2025, 2, 19)},
	{Version: "0.2", Date: day(2025, 5, 7)},
	{Version: "0.2.1", Date: day(2025, 5, 15)},
	{Version: "0.2.2", Date: day(2025, 5, 20)},
	{Version: "0.3", Date: day(2025, 9, 25)},
	{Version: "0.4", Date: day(2025, 12, 4)},
	{Version: "0.4.1", Date: day(2025, 12, 16)},
	{Version: "0.5", Date: day(2026, 2, 12)},
	{Version: "0.5.2", Date: day(2026, 2, 26)}, // 0.5.1 came out the same day
	{Version: "0.5.3", Date: day(2026, 3, 5)},
	{Version: "0.5.4", Date: day(2026, 3, 16)},
	{Version: "0.6", Date: day(2026, 4, 15)},
	{Version: "0.6.1", Date: day(2026, 4, 16)},
	{Version: "0.6.2", Date: day(2026, 4, 21)},
	{Version: "0.6.3", Date: day(2026, 5, 6)},
	{Version: "0.7", Date: day(2026, 6, 3)},
	{Version: "0.7.1", Date: day(2026, 6, 5)},
	{Version: "0.8", Date: day(2026, 7, 9)},
	{Version: "0.8.1", Date: day(2026, 7, 22)},
	{Version: "0.9", Date: day(2026, 8, 26)},
	{Version: "0.9.1", Date: day(2026, 9, 1)},
	{Version: "0.10", Date: day(2026, 12, 9)}, // announced; applies from that date
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Patches returns Assetto Corsa EVO's known PC patches.
func (g *Game) Patches() []game.Patch { return append([]game.Patch(nil), patches...) }
