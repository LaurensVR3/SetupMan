// Package ac supports Assetto Corsa (2014): setups are INI files in
// Documents\Assetto Corsa\setups\<car>\<track>\<name>.ini.
package ac

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"setupman/internal/game"
	"setupman/internal/games/acc"
	"setupman/internal/games/gamekit"
	"setupman/internal/lastopened"
	"setupman/internal/setup"
)

// SteamAppID is Assetto Corsa's Steam application id.
const SteamAppID = "244210"

// Game is the AC implementation of game.Game.
type Game struct {
	live *acc.ACLive

	once    sync.Once
	content string // <install>/content, for readable car and track names ("" = not found)

	carRanges carRanges // each car's setup.ini, for real values (D-141)
}

// New creates the AC game.
// Program names given by the user (Q-G2): acs.exe, and acs_x86.exe for the old 32-bit version.
// Still to be confirmed in Task Manager while AC runs (R-14).
func New() *Game { return &Game{live: acc.NewACLive("acs.exe", "acs_x86.exe")} }

func (g *Game) ID() string       { return "ac" }
func (g *Game) Name() string     { return "Assetto Corsa" }
func (g *Game) Short() string    { return "AC" }
func (g *Game) SetupExt() string { return ".ini" }

// Live reports whether the player is driving (AC's shared memory, D-137).
func (g *Game) Live() game.LiveState { return g.live.Live() }

// Telemetry returns the latest live-data snapshot, for recording laps (false when AC isn't running).
func (g *Game) Telemetry() (game.Telemetry, bool) { return g.live.Telemetry() }

// StartedAt returns when AC's program started (D-147).
func (g *Game) StartedAt() (time.Time, bool) { return g.live.StartedAt() }

// MatchSetup returns the candidate setups whose fuel fits the start fuel (one = sure, several = the
// user picks, D-149). AC's live data has no electronics positions in clicks, so only the start fuel
// can tell setups apart (D-137).
func (g *Game) MatchSetup(t game.Telemetry, candidates [][]byte) []int {
	if len(candidates) == 1 {
		return []int{0}
	}
	var out []int
	for i, c := range candidates {
		fields, err := Parse(c)
		if err != nil {
			continue
		}
		for _, f := range fields {
			if f.Path == "FUEL" && f.IsNumber() && t.Fuel > 0 && math.Abs(f.Num-t.Fuel) <= 1.5 {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// EditSetup changes values in an AC setup file (see Edit).
func (g *Game) EditSetup(content []byte, edits map[string]string) ([]byte, error) {
	return Edit(content, edits)
}

// Detect looks for AC via Steam and for its setups folder in Documents.
func (g *Game) Detect() game.Detection {
	var d game.Detection
	d.Installed, _ = gamekit.SteamGame(SteamAppID)
	if doc := gamekit.Documents(); doc != "" {
		if f := filepath.Join(doc, "Assetto Corsa", "setups"); gamekit.IsDir(f) {
			d.Folder, d.Installed = f, true
		}
	}
	return d
}

// ParseSetup turns stored file content back into comparable fields.
// Values get the car's real values when the game is installed on this PC (D-141).
func (g *Game) ParseSetup(content []byte) ([]setup.Field, error) {
	fields, err := Parse(content)
	if err == nil {
		applyRanges(fields, g.ranges(carModel(fields)))
	}
	return fields, err
}

// Scan reads an AC setups folder (read-only).
func (g *Game) Scan(folder string) (*game.Scan, error) {
	lay := gamekit.Layout{
		Ext:   ".ini",
		Parse: g.ParseSetup, // with the car's real values (D-141)
		Car:   g.car,
		Track: g.track,
		Skip:  map[string]string{".sp": "AC's pit-stop presets for the setup with the same name (not a setup)"},
		Hint:  `No setups found. Choose the folder that contains the car folders (normally Documents\Assetto Corsa\setups).`,
	}
	return lay.Scan(folder)
}

func (g *Game) contentDir() string {
	g.once.Do(func() {
		if _, dir := gamekit.SteamGame(SteamAppID); dir != "" && gamekit.IsDir(filepath.Join(dir, "content")) {
			g.content = filepath.Join(dir, "content")
		}
	})
	return g.content
}

// The game's ui_car.json / ui_track.json are often not valid JSON (stray tabs and line breaks),
// so the few values needed are picked out one by one.
var uiValue = func(key string) *regexp.Regexp { return regexp.MustCompile(`"` + key + `"\s*:\s*"([^"]*)"`) }
var (
	reName  = uiValue("name")
	reBrand = uiValue("brand")
	reClass = uiValue("class")
)

func uiFile(path string, re *regexp.Regexp) string {
	b, err := lastopened.ReadFile(path)
	if err != nil || len(b) > 1<<20 {
		return ""
	}
	if m := re.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// car gives the readable name: from the installed game, or made from the folder name.
func (g *Game) car(folder string) game.Car {
	c := game.Car{Name: gamekit.Words(folder)}
	if dir := g.contentDir(); dir != "" {
		ui := filepath.Join(dir, "cars", folder, "ui", "ui_car.json")
		if n := uiFile(ui, reName); n != "" {
			c.Name = n
		}
		c.Brand = uiFile(ui, reBrand)
		switch strings.ToLower(uiFile(ui, reClass)) {
		case "race":
			c.Class = "Race cars"
		case "street":
			c.Class = "Road cars"
		}
	}
	if c.Brand == "" {
		c.Brand = strings.Fields(c.Name + " ?")[0]
	}
	if c.Class == "" {
		c.Class = "Cars"
	}
	return c
}

// track gives the readable name. "generic" holds setups for any track.
func (g *Game) track(folder string) string {
	if strings.EqualFold(folder, "generic") {
		return "Any track"
	}
	if dir := g.contentDir(); dir != "" {
		ui := filepath.Join(dir, "tracks", folder, "ui")
		if n := uiFile(filepath.Join(ui, "ui_track.json"), reName); n != "" {
			return n
		}
		// Tracks with layouts have ui/<layout>/ui_track.json, named e.g. "Lime Rock Park - South Chicane".
		// Setups are per track, not per layout, so the layout part is left off.
		if entries, err := os.ReadDir(ui); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					if n := uiFile(filepath.Join(ui, e.Name(), "ui_track.json"), reName); n != "" {
						return trackOnly(n)
					}
				}
			}
		}
	}
	return gamekit.Words(folder)
}

// trackOnly drops the layout from a layout name: "Bikernieki - Big Ring" → "Bikernieki".
func trackOnly(name string) string {
	if i := strings.Index(name, " - "); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	return name
}
