package ac

import (
	"time"

	"setupman/internal/game"
)

// Patches are Assetto Corsa's PC releases (spec D-103), oldest first, as provided by the user on
// 2026-11-19. The 1.0.0 release candidate (15 Oct 2014) is left out because its name is not a
// plain version number: setups saved before 1.0 count as 0.22.
var patches = []game.Patch{
	{Version: "0.3", Date: day(2013, 12, 6)},
	{Version: "0.4", Date: day(2013, 12, 20)},
	{Version: "0.5", Date: day(2014, 1, 17)},
	{Version: "0.6", Date: day(2014, 1, 31)},
	{Version: "0.6.5", Date: day(2014, 2, 14)},
	{Version: "0.6.6", Date: day(2014, 2, 15)},
	{Version: "0.7", Date: day(2014, 2, 28)},
	{Version: "0.7.5", Date: day(2014, 3, 14)},
	{Version: "0.8", Date: day(2014, 3, 28)},
	{Version: "0.8.5", Date: day(2014, 4, 11)},
	{Version: "0.9", Date: day(2014, 5, 9)},
	{Version: "0.10", Date: day(2014, 5, 23)},
	{Version: "0.15", Date: day(2014, 6, 6)},
	{Version: "0.20", Date: day(2014, 6, 20)},
	{Version: "0.21", Date: day(2014, 7, 18)},
	{Version: "0.21.6", Date: day(2014, 7, 24)},
	{Version: "0.22", Date: day(2014, 8, 29)},
	{Version: "1.0", Date: day(2014, 12, 19)},
	{Version: "1.0.1", Date: day(2014, 12, 24)},
	{Version: "1.0.2", Date: day(2015, 1, 19)},
	{Version: "1.0.3", Date: day(2015, 2, 2)},
	{Version: "1.0.4", Date: day(2015, 2, 5)},
	{Version: "1.1", Date: day(2015, 3, 11)},
	{Version: "1.1.6", Date: day(2015, 4, 27)},
	{Version: "1.2", Date: day(2015, 7, 31)},
	{Version: "1.3", Date: day(2015, 10, 8)},
	{Version: "1.3.1", Date: day(2015, 10, 9)},
	{Version: "1.3.2", Date: day(2015, 10, 12)},
	{Version: "1.3.3", Date: day(2015, 10, 17)},
	{Version: "1.3.4", Date: day(2015, 10, 22)},
	{Version: "1.3.5", Date: day(2015, 11, 25)},
	{Version: "1.3.6", Date: day(2015, 11, 26)},
	{Version: "1.4", Date: day(2015, 12, 15)},
	{Version: "1.4.2", Date: day(2015, 12, 22)},
	{Version: "1.4.3", Date: day(2015, 12, 23)},
	{Version: "1.5", Date: day(2016, 3, 31)},
	{Version: "1.5.5", Date: day(2016, 4, 6)},
	{Version: "1.6", Date: day(2016, 5, 18)},
	{Version: "1.7", Date: day(2016, 7, 14)},
	{Version: "1.8", Date: day(2016, 8, 26)},
	{Version: "1.9", Date: day(2016, 10, 25)},
	{Version: "1.10", Date: day(2016, 11, 22)},
	{Version: "1.11", Date: day(2016, 12, 20)},
	{Version: "1.12", Date: day(2017, 2, 14)},
	{Version: "1.13", Date: day(2017, 3, 22)},
	{Version: "1.14", Date: day(2017, 5, 18)},
	{Version: "1.16", Date: day(2017, 12, 20)},
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Patches returns Assetto Corsa's known PC patches.
func (g *Game) Patches() []game.Patch { return append([]game.Patch(nil), patches...) }
