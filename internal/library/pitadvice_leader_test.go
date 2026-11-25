package library

import (
	"math"
	"testing"

	"setupman/internal/game"
)

// Timed race end from the leader's position (D-198). Lap time 100 s throughout.
func TestTimedLapsToGoLeader(t *testing.T) {
	race := func(left float64) game.Telemetry {
		return game.Telemetry{Session: "race", TimeLeftMs: left, TimeLeftKnown: true}
	}
	cases := []struct {
		name string
		t    game.Telemetry
		done float64
		want float64
		end  string
	}{
		// The rig race: the leader crossed with 5 s left and starts one more lap; 10 s behind,
		// the driver crosses after zero and still has to drive that lap too.
		{"leader started a last lap", func() game.Telemetry {
			x := race(5000)
			x.LeaderKnown, x.LeaderProgress, x.LeaderLapMs = true, 0, 100000
			return x
		}(), 0.9, 1.1, "leader"},
		{"no leader known: as before", race(5000), 0.9, 0.1, "unknown"},
		{"driver leads", func() game.Telemetry {
			x := race(5000)
			x.LeaderKnown, x.LeaderIsPlayer = true, true
			return x
		}(), 0.9, 0.1, "you"},
		{"leader has finished", func() game.Telemetry {
			x := race(-20000)
			x.LeaderKnown, x.LeaderFinished, x.LeaderProgress = true, true, 0.2
			return x
		}(), 0.6, 0.4, "leader"},
		// Clock at zero, leader halfway round their last lap, driver only 20 % round: the leader
		// finishes first, so this lap is the driver's last.
		{"leader finishes before the driver's crossing", func() game.Telemetry {
			x := race(-5000)
			x.LeaderKnown, x.LeaderProgress, x.LeaderLapMs = true, 0.5, 100000
			return x
		}(), 0.2, 0.8, "leader"},
		// 12.5 minutes left; the leader (about to lap the driver) crosses the line 15 s after the
		// driver each lap: driver at 0:45 left and -0:55, leader at 0:30 and finishing at -1:10,
		// so the driver gets one lap more than the own clock says (8.05).
		{"mid race, leader just behind on the road", func() game.Telemetry {
			x := race(750000)
			x.LeaderKnown, x.LeaderProgress, x.LeaderLapMs = true, 0.8, 100000
			return x
		}(), 0.95, 9.05, "leader"},
		{"qualifying: own crossing", game.Telemetry{Session: "qualifying", TimeLeftMs: 5000, TimeLeftKnown: true}, 0.9, 0.1, ""},
	}
	for _, c := range cases {
		got, end := timedLapsToGo(c.t, c.done, 100000)
		if math.Abs(got-c.want) > 1e-9 || end != c.end {
			t.Errorf("%s: got %.3f %q, want %.3f %q", c.name, got, end, c.want, c.end)
		}
	}
}
