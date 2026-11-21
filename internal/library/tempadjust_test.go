package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/game"
)

func TestProposeTemperature(t *testing.T) {
	folder := setupFolder(t)
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	// The user's own two variants: air 20 / track 28 and air 28 / track 32 (D-145).
	cold := string(base)
	warm := strings.Replace(cold, `"tyrePressure": [ 50, 50, 50, 50 ]`, `"tyrePressure": [ 46, 46, 46, 46 ]`, 1)
	warm = strings.Replace(warm, `"brakeDuct": [ 3, 3 ]`, `"brakeDuct": [ 4, 4 ]`, 1)
	os.WriteFile(filepath.Join(spa, "ABC R 2028.json"), []byte(cold), 0o644)
	os.WriteFile(filepath.Join(spa, "ABC R 2832.json"), []byte(warm), 0o644)

	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	var coldID, plainID int64
	for i, s := range td.Setups {
		switch s.Name {
		case "ABC R 2028":
			coldID = td.SetupIDs[i]
		case "a":
			plainID = td.SetupIDs[i]
		}
	}

	// Track 28 → 36 °C: +8 °C. Learned per °C of track: pressure −4 clicks per 4 °C, ducts +1 per 4 °C.
	p, err := l.ProposeTemperature(coldID, 26, 36)
	if err != nil {
		t.Fatal(err)
	}
	if p.Pairs != 1 || len(p.Rules) != 6 {
		t.Fatalf("learned %d pairs, %d rules: %+v", p.Pairs, len(p.Rules), p.Rules)
	}
	want := map[string]string{
		"basicSetup.tyres.tyrePressure[0]":                        "42",
		"advancedSetup.aeroBalance.brakeDuct[0]":                  "5",
		"basicSetup.strategy.pitStrategy#1.tyres.tyrePressure[0]": "37",
	}
	for k, v := range want {
		if p.Edits[k] != v {
			t.Errorf("%s = %q, want %s (all: %v)", k, p.Edits[k], v, p.Edits)
		}
	}
	if p.NewName != "ABC R 2636" {
		t.Errorf("new name %q", p.NewName)
	}
	// The proposal can be saved as a new setup through the normal edit path.
	if _, err := l.EditSetup(coldID, p.Edits, p.NewName); err != nil {
		t.Errorf("saving the proposal: %v", err)
	}

	// No temperature in the name: explain instead of guessing.
	if _, err := l.ProposeTemperature(plainID, 26, 36); err == nil {
		t.Error("proposed for a setup without a temperature")
	}
	if _, err := l.ProposeTemperature(coldID, 26, 0); err == nil {
		t.Error("accepted a missing track temperature")
	}
}
