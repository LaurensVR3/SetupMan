package naming

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		want Meta
	}{
		{"ABC R 1.10.4 2327 TC25", Meta{Initials: "ABC", Session: SessionR, GameVersion: "1.10.4", Air: 23, Track: 27, Codes: []string{"TC25"}}},
		{"LA 296 HUN Q", Meta{Initials: "LA", Session: SessionQ, Codes: []string{}}},
		{"SCG F296 HUN  1.42.9   Q 21 25", Meta{Initials: "SCG", Session: SessionQ, LapTime: "1:42.9", Air: 21, Track: 25, Codes: []string{"F296"}}},
		{"FRI3_296_LAG_Q2_20_25_v1.9.2", Meta{Session: SessionQ, GameVersion: "1.9.2", Air: 20, Track: 25, Codes: []string{"FRI3"}}},
		{"45m 1 stp 2026", Meta{Session: SessionR, RaceMinutes: 45, Stops: 1, Air: 20, Track: 26, Codes: []string{}}},
		{"45m 2128 1stop", Meta{Session: SessionR, RaceMinutes: 45, Stops: 1, Air: 21, Track: 28, Codes: []string{}}},
		{"hungo25m 1718", Meta{Session: SessionR, RaceMinutes: 25, Air: 17, Track: 18, Codes: []string{}}},
		{"-abesports_HUN_2026", Meta{Initials: "abesports", Air: 20, Track: 26, Codes: []string{}}},
		{"GO_Q_23-27C", Meta{Initials: "GO", Session: SessionQ, TempMin: 23, TempMax: 27, Codes: []string{}}},    // a dash: a range
		{"R 1.10.3 3040", Meta{Session: SessionR, GameVersion: "1.10.3", Air: 30, Track: 40, Codes: []string{}}}, // D-145
		{"test 2014", Meta{Codes: []string{}}}, // a year, not air 20 / track 14
		{"yt (17)", Meta{Codes: []string{}}},
		{"HYMO 2h endurance", Meta{Initials: "HYMO", Session: SessionEndu, RaceMinutes: 120, Codes: []string{}}},
		{"BGK 60m race 1622", Meta{Initials: "BGK", Session: SessionR, RaceMinutes: 60, Air: 16, Track: 22, Codes: []string{}}},
		{"quali (1)", Meta{Session: SessionQ, Codes: []string{}}},
	}
	for _, c := range cases {
		if got := Parse(c.name); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q)\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestGuessSession(t *testing.T) {
	cases := []struct {
		name string
		endu int
		want string
	}{
		{"45m 1 stp 2026", 45, SessionR},        // 22.5 min stints
		{"BGK 60m race 1622", 45, SessionEndu},  // one 60 min stint
		{"BGK 60m race 1622", 60, SessionR},     // user's limit is 60
		{"2h 3 stops", 45, SessionR},            // 30 min stints
		{"ABC Q 2h", 45, SessionQ},              // Q stays Q
		{"HYMO 25m endurance", 45, SessionEndu}, // explicit endurance stays
		{"race", 45, SessionR},
		{"tst", 45, ""},
		{"60m", 0, SessionEndu}, // 0 = default 45
	}
	for _, c := range cases {
		if got := GuessSession(Parse(c.name), c.endu); got != c.want {
			t.Errorf("GuessSession(%q, %d) = %q, want %q", c.name, c.endu, got, c.want)
		}
	}
}

func TestCore(t *testing.T) {
	same := [][2]string{
		{"LA 296 HUN Q", "LA 296 HUN R"},
		{"hungo25m 1718", "hungo q 3037"},
		{"-abesports_HUN_2026", "-abesports_HUN_23_27"},
		{"ABC R 1.10.4 2327 TC25", "ABC Q 1.10.5 2026 TC25"},
		{"quali", "race (1)"},
	}
	for _, p := range same {
		if Core(p[0]) != Core(p[1]) {
			t.Errorf("Core(%q)=%q != Core(%q)=%q", p[0], Core(p[0]), p[1], Core(p[1]))
		}
	}
	if Core("LA 296 HUN Q") == Core("LA 296 KYA Q") {
		t.Error("different tracks should not match")
	}
	if !SameName("LA 296 HUN Q", "LA 296 HUN R") || SameName("quali", "race") || SameName("q", "25m Race 1825") {
		t.Error("SameName wrong")
	}
	if Parse("HOTLAP").Initials != "" {
		t.Error("a single word is not initials")
	}
}

// TestDumpNames prints what is read from every name in the local dump (not in git).
func TestDumpNames(t *testing.T) {
	root := filepath.Join("..", "..", "SampleSetups", "Assetto Corsa Competizione")
	if _, err := os.Stat(root); err != nil {
		t.Skip("dump not present")
	}
	var lines []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
			n := strings.TrimSuffix(d.Name(), ".json")
			m := Parse(n)
			lines = append(lines, fmt.Sprintf("%-40q ini=%-10s ses=%-9s ver=%-7s temp=%d-%d lap=%-7s codes=%v core=%q",
				n, m.Initials, m.Session, m.GameVersion, m.TempMin, m.TempMax, m.LapTime, m.Codes, Core(n)))
		}
		return nil
	})
	if len(lines) < 100 { // the dump is a live folder; the exact count can change
		t.Errorf("got %d names, expected the dump's 140-odd", len(lines))
	}
	if testing.Verbose() {
		t.Log("\n" + strings.Join(lines, "\n"))
	}
}

func TestRigNames(t *testing.T) {
	cases := []struct {
		name       string
		session    string
		air, track int
	}{
		{"19c 21c CDA M LFM Q DRY 01", SessionQ, 19, 21},
		{"19c 21c CDA4 RS DRY 02", SessionR, 19, 21},
		{"19c 21c CDA4 QF DRY 02", SessionQ, 19, 21},
		{"2123R", SessionR, 21, 23},
		{"24°C 27°C SRO R", SessionR, 24, 27},
		{"R1516", SessionR, 15, 16},
		{"HUN W2429", "", 24, 29},
		{"GO2 296 GT3 SIL E R01 23C", SessionR, 23, 23},
		{"FRI3_296_BAR_RS_20_25_v1.9.5", SessionR, 20, 25},
		{"ABC 1313 R 1.10.4 tc36", SessionR, 13, 13},
	}
	for _, c := range cases {
		m := Parse(c.name)
		air, track := m.Air, m.Track
		if air == 0 { // one temperature ("23C"): kind not given
			air, track = m.TempMin, m.TempMax
		}
		if m.Session != c.session || air != c.air || track != c.track {
			t.Errorf("%q: session %q air %d track %d", c.name, m.Session, air, track)
		}
	}
}
