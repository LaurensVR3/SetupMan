package acc

import (
	"time"

	"setupman/internal/game"
)

// Patches are ACC's numbered PC releases (spec D-103), oldest first, as verified by the user on
// 2026-11-19 (Q-U17). Unnumbered hotfixes and livery/sponsor updates are left out: setups saved
// after them keep the version before. 1.9 and 1.10.4 are not in the user's list; their dates
// are Claude's earlier estimates and are marked below.
var patches = []game.Patch{
	{Version: "1.7.12", Date: day(2021, 6, 29)},
	{Version: "1.7.13", Date: day(2021, 8, 23)},
	{Version: "1.7.15", Date: day(2021, 10, 7)},
	{Version: "1.8", Date: day(2021, 11, 24)},
	{Version: "1.8.2", Date: day(2021, 11, 26)},
	{Version: "1.8.5", Date: day(2021, 12, 2)},
	{Version: "1.8.6", Date: day(2021, 12, 3)},
	{Version: "1.8.7", Date: day(2021, 12, 9)},
	{Version: "1.8.8", Date: day(2021, 12, 15)},
	{Version: "1.8.9", Date: day(2022, 1, 7)},
	{Version: "1.8.10", Date: day(2022, 1, 18)},
	{Version: "1.8.11", Date: day(2022, 2, 7)},
	{Version: "1.9", Date: day(2023, 4, 13)}, // estimate (2023 GT World Challenge pack); not in the user's list
	{Version: "1.9.1", Date: day(2023, 4, 25)},
	{Version: "1.9.2", Date: day(2023, 5, 4)},
	{Version: "1.9.3", Date: day(2023, 5, 25)},
	{Version: "1.9.4", Date: day(2023, 6, 7)},
	{Version: "1.9.5", Date: day(2023, 7, 21)},
	{Version: "1.9.6", Date: day(2024, 1, 24)},
	{Version: "1.9.7", Date: day(2024, 1, 26)},
	{Version: "1.9.8", Date: day(2024, 2, 1)},
	{Version: "1.10", Date: day(2024, 4, 1)},
	{Version: "1.10.3", Date: day(2024, 8, 28)},
	{Version: "1.10.4", Date: day(2025, 3, 1)}, // estimate (2025 season update); not in the user's list
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Patches returns ACC's known PC patches.
func (g *Game) Patches() []game.Patch { return append([]game.Patch(nil), patches...) }
