// Package game defines what every supported sim title must provide.
// Each title lives in its own package under internal/games (ACC is the first).
package game

import (
	"time"

	"setupman/internal/compare"
	"setupman/internal/setup"
)

// Game is one supported sim title.
type Game interface {
	ID() string   // short, stable, used in URLs and settings, e.g. "acc"
	Name() string // e.g. "Assetto Corsa Competizione"
	// Detect reports whether the game is installed and where its setups normally live.
	Detect() Detection
	// Scan reads the setup folder (read-only, spec D-07).
	Scan(folder string) (*Scan, error)
	// ParseSetup turns stored file content back into comparable fields (for version history).
	ParseSetup(content []byte) ([]setup.Field, error)
}

// Editor is implemented by games whose setup files SetupMan can change (spec D-85).
// edits maps a field path to its new whole-number value; the result is the new file content.
type Editor interface {
	EditSetup(content []byte, edits map[string]string) ([]byte, error)
}

// LiveReader is implemented by games that expose live data (e.g. shared memory).
type LiveReader interface {
	Live() LiveState
}

// Telemetry is one snapshot of live data, used to record laps per setup (spec D-44, D-48).
// Fields a game can't provide (or that look implausible) are marked unknown.
type Telemetry struct {
	Live          bool    // a session is running (not menu, pause or replay)
	Off           bool    // no session at all: the game's main menu, e.g. after leaving a server (D-177)
	Session       string  // "practice", "qualifying", "race", "hotlap", … or ""
	Car, Track    string  // as the game names them (e.g. "ferrari_296_gt3", "spa")
	CompletedLaps int     // laps completed in this session
	LastLapMs     int     // last completed lap time
	InPit         bool    // in the pit box
	InPitLane     bool    // in the pit lane (only meaningful when PitLaneKnown)
	PitLaneKnown  bool    //
	ValidLap      bool    // current lap is still valid (only meaningful when ValidKnown)
	ValidKnown    bool    //
	Fuel          float64 // litres in the tank
	AirTemp       float64 // °C (only meaningful when TempsKnown)
	RoadTemp      float64 // °C
	TempsKnown    bool    //
	TC, ABS, Map  int     // electronics as shown in the car (-1 = unknown)

	// For pit stop advice (D-124). Each group is only meaningful when its …Known flag is set.
	Pressures      [4]float64 // hot tyre pressures now, psi, FL FR RL RR
	PressuresKnown bool
	WetTyres       bool // rain tyres fitted
	WetKnown       bool
	TimeLeftMs     float64 // session time left (≤ 0 when the clock has run out)
	TimeLeftKnown  bool
	SessionLaps    int     // laps in a lap-count session (0 = timed session or unknown)
	LapProgress    float64 // how far round the current lap, 0 … 1
	ProgressKnown  bool
	StopsLeft      int // mandatory pit stops still to make
	StopsKnown     bool
	PitFuel        float64    // fuel to add as now set in the pit menu, litres
	PitPressures   [4]float64 // tyre pressures as now set in the pit menu, psi
	PitMenuKnown   bool
	BrakeBias      float64 // as the game reports it (ACC: physics brakeBias); logged only, D-149
	BiasKnown      bool
	StintLeftMs    int // time the driver may still drive in this stint, set by the server (D-125)
	StintKnown     bool
	TankSize       float64 // litres the car's tank holds
	TankKnown      bool
}

// TelemetrySource is implemented by games that can report live data for recording laps.
type TelemetrySource interface {
	Telemetry() (Telemetry, bool) // false = the game isn't running
}

// SetupMatcher guesses which of the candidate setups (file contents) is being driven, from a
// telemetry snapshot taken when the car leaves the pits. It returns the indexes that fit best:
// one = sure, several = equally likely (the user picks among them, D-149), none = nothing fits.
type SetupMatcher interface {
	MatchSetup(t Telemetry, candidates [][]byte) []int
}

// SetupTempMatcher also takes the air temperature each setup was made for (from its name, 0 =
// unknown): tyre pressures adjusted for today's air temperature still fit the setup (D-181).
type SetupTempMatcher interface {
	MatchSetupTemps(t Telemetry, candidates [][]byte, airs []float64) []int
}

// SetupCacher is implemented by games that read all setup files when they list them and keep them
// in memory, so a file's last-opened time doesn't say which setup is loaded (ACC, rig log
// 2026-11-21, D-149).
type SetupCacher interface {
	CachesSetups() bool
}

// StartTimer tells when the game's program started (D-147): setups it loaded were opened after
// that moment. False when the game doesn't run or the time can't be read.
type StartTimer interface {
	StartedAt() (time.Time, bool)
}

// LiveState says whether the player is driving right now (spec D-38).
type LiveState string

const (
	LiveUnknown    LiveState = "unknown"     // game not running, or live data unreadable
	LiveNotDriving LiveState = "not-driving" // menus, pit box, pause, replay
	LiveDriving    LiveState = "driving"
)

// Detection is the result of looking for a game on this PC.
type Detection struct {
	Installed bool   `json:"installed"`
	Folder    string `json:"folder"` // default setup folder; "" when unknown
}

// Scan is everything found in one setup folder.
type Scan struct {
	Root      string    `json:"root"`
	ScannedAt time.Time `json:"scannedAt"`
	Cars      []Car     `json:"cars"`
	Ignored   []Issue   `json:"ignored"`  // files not used
	Warnings  []Issue   `json:"warnings"` // files used, but something looks odd
	Hint      string    `json:"hint,omitempty"`
}

// Car is one car with setups, with readable names (spec D-27).
type Car struct {
	ID     string  `json:"id"`    // folder name as used by the game
	Name   string  `json:"name"`  // e.g. "Ferrari 296 GT3"
	Brand  string  `json:"brand"` // e.g. "Ferrari"
	Class  string  `json:"class"` // e.g. "GT3"
	Year   int     `json:"year,omitempty"`
	Tracks []Track `json:"tracks"`
}

// Track holds all setups for one car on one track.
type Track struct {
	ID     string         `json:"id"`     // lower-case folder name, used in URLs
	Name   string         `json:"name"`   // e.g. "Spa-Francorchamps"
	Folder string         `json:"folder"` // folder name on disk (most common spelling)
	Setups []setup.Setup  `json:"setups"`
	Pairs  []compare.Pair `json:"pairs"` // every pair of setups compared
}

// Issue is a file that was skipped, or a remark about a file that was used.
type Issue struct {
	RelPath string `json:"relPath"`
	Reason  string `json:"reason"`
}

// SetupCount returns the number of setups for a car.
func (c *Car) SetupCount() int {
	n := 0
	for _, t := range c.Tracks {
		n += len(t.Setups)
	}
	return n
}

// Patch is one release of a game (spec D-103): setups saved on or after Date (and before the
// next patch) were probably made on this version.
type Patch struct {
	Version string    `json:"version"`
	Date    time.Time `json:"date"`
}

// WetChecker is implemented by games whose setup files say whether rain tyres are fitted, for
// the naming standard's WET part (D-89).
type WetChecker interface {
	IsWet(fields []setup.Field) bool
}

// PatchLister is implemented by games that know their patch history.
type PatchLister interface {
	Patches() []Patch
}

// VersionAt returns the patch version current on a date ("" before the first known patch).
func VersionAt(list []Patch, t time.Time) string {
	v, at := "", time.Time{}
	for _, p := range list {
		if !t.Before(p.Date) && !p.Date.Before(at) {
			v, at = p.Version, p.Date
		}
	}
	return v
}

// TelemetryFolder is implemented by games that write their own telemetry files (ACC: MoTeC logs in
// Documents\Assetto Corsa Competizione\MoTeC, D-160). SetupMan only ever reads that folder.
type TelemetryFolder interface {
	TelemetryFolder() string // "" when not found
}

// ExtraChannel is one value an ExtraLogger reads.
type ExtraChannel struct {
	Name string // e.g. "FUEL", "SLIP_ANGLE_FL"; LAP, LAP_TIME and LAST_LAP are required to line laps up
	Unit string
}

// ExtraLogger is implemented by games whose live data has values their own telemetry files lack.
// SetupMan logs them about 10 times per second while a session is live (D-162).
type ExtraLogger interface {
	ExtraChannels() []ExtraChannel
	OpenExtra() (ExtraReader, error)
}

// ExtraReader reads the extra channels; keep it open while logging (opening is the costly part).
type ExtraReader interface {
	// Read fills dst (one value per ExtraChannels entry) and says whether a session is live
	// (not menus, pause or replay). ok = false: the game's live data can't be read any more.
	Read(dst []float32) (live, ok bool)
	Close()
}

// Namer is implemented by games that know readable car and track names without a setup folder
// (telemetry may exist for cars and tracks that have no setups).
type Namer interface {
	CarInfo(id string) (name, brand, class string)
	TrackName(id string) string
}
