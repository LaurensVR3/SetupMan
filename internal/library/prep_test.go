package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setupman/internal/game"
	"setupman/internal/store"
)

// Race prep ranks by session, then the temperature in the name, then your laps at that temperature.
func TestPrepRanking(t *testing.T) {
	base, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Setups")
	spa := filepath.Join(dir, "ferrari_296_gt3", "Spa")
	os.MkdirAll(spa, 0o755)
	write := func(name, tc string) {
		os.WriteFile(filepath.Join(spa, name+".json"), []byte(strings.Replace(string(base), `"tC1": 4`, `"tC1": `+tc, 1)), 0o644)
	}
	write("Q 2230", "1")     // qualifying, air 22 °C, track 30 °C (D-145)
	write("R 2836", "2")     // race, too warm
	write("R 2230", "3")     // race, right temperature
	write("R 2230 old", "5") // race, right temperature, slower laps
	write("tst", "6")        // nothing known
	g := &fakeGame{installed: true, folder: dir, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })

	ids := map[string]int64{}
	all, _ := l.db.SetupsForTrack("acc", "ferrari_296_gt3", "spa")
	for _, s := range all {
		ids[s.Name] = s.ID
	}
	// Laps at 22 °C air: "R 2230" averages 1:45, "R 2230 old" 1:46.
	sess := &store.Session{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Kind: "race", StartedAt: time.Now()}
	if err := l.db.InsertSession(sess); err != nil {
		t.Fatal(err)
	}
	for name, ms := range map[string]int{"R 2230": 105000, "R 2230 old": 106000} {
		for i := 0; i < 3; i++ {
			l.db.InsertLap(&store.Lap{SessionID: sess.ID, SetupID: ids[name], LapMs: ms + i*100, Valid: true, Air: 22, Road: 30, HasTemps: true})
		}
	}

	got, err := l.Prep(PrepInput{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Session: "R", Temp: 22, HasTemp: true, TempKind: "air"})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, p := range got {
		order = append(order, p.Name)
	}
	want := []string{"R 2230", "R 2230 old", "R 2836", "tst", "Q 2230"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Errorf("order = %v, want %v", order, want)
	}
	if !got[0].Top || got[0].Near.Clean != 3 {
		t.Errorf("top: %+v", got[0])
	}
	if !strings.Contains(strings.Join(got[0].Reasons, " / "), "Made for air 22 °C, track 30 °C") {
		t.Errorf("reasons: %v", got[0].Reasons)
	}
	// Measured against track temperature, no lap is within 3 °C of 22, so the laps don't decide.
	got, _ = l.Prep(PrepInput{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Session: "R", Temp: 22, HasTemp: true, TempKind: "track"})
	if got[0].Near.Clean != 0 {
		t.Errorf("laps at 30 °C track counted for 22 °C: %+v", got[0].Near)
	}
	// Track 30 °C expected: the names' track temperature (30) is compared, not their air (22).
	got, _ = l.Prep(PrepInput{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Session: "R", Temp: 30, HasTemp: true, TempKind: "track"})
	if !strings.Contains(strings.Join(got[0].Reasons, " / "), "Made for track 30 °C, air 22 °C") {
		t.Errorf("track reasons: %v", got[0].Reasons)
	}
}

// Within the same temperature group, the closer temperature comes first when no laps decide.
func TestPrepCloserTemperatureFirst(t *testing.T) {
	base, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Setups")
	spa := filepath.Join(dir, "ferrari_296_gt3", "Spa")
	os.MkdirAll(spa, 0o755)
	for i, n := range []string{"R 14C", "R 2226"} { // 3 °C and 5 °C away from 17 °C
		p := filepath.Join(spa, n+".json")
		os.WriteFile(p, []byte(strings.Replace(string(base), `"tC1": 4`, `"tC1": `+string(rune('1'+i)), 1)), 0o644)
		when := time.Now().Add(time.Duration(i) * time.Hour) // "R 2226" is newer
		os.Chtimes(p, when, when)
	}
	g := &fakeGame{installed: true, folder: dir, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	got, err := l.Prep(PrepInput{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Session: "R", Temp: 17, HasTemp: true, TempKind: "air"})
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Name != "R 14C" {
		t.Errorf("first = %s, want R 14C (closer to 17 °C)", got[0].Name)
	}
}
