package library

import (
	"testing"

	"setupman/internal/store"
)

// One file for a server weekend (D-188): laps go to their linked lap's session; a lap without a link
// goes with the lap before it; a file of one session isn't split.
func TestLapSessions(t *testing.T) {
	laps := []store.TeleLap{
		{N: 0}, {N: 1, SessionID: 19}, {N: 2, SessionID: 19}, {N: 3}, // out-lap, practice, a gap lap
		{N: 4, SessionID: 20}, {N: 5}, {N: 6, SessionID: 21}, {N: 7, SessionID: 21},
	}
	order, of := lapSessions(laps)
	if len(order) != 3 || order[0] != 19 || order[2] != 21 {
		t.Fatalf("order %v", order)
	}
	want := []int64{19, 19, 19, 19, 20, 20, 21, 21}
	for i := range want {
		if of[i] != want[i] {
			t.Fatalf("lap %d → %d, want %d (%v)", i, of[i], want[i], of)
		}
	}
	if got := partLaps(laps, 20); len(got) != 2 || got[0].N != 4 || got[1].N != 5 {
		t.Errorf("qualifying laps: %+v", got)
	}
	one := []store.TeleLap{{N: 1, SessionID: 5}, {N: 2}}
	if _, of := lapSessions(one); of != nil {
		t.Errorf("a file of one session was split")
	}
	if got := partLaps(one, 5); len(got) != 2 {
		t.Errorf("unsplit file: %d laps", len(got))
	}
}
