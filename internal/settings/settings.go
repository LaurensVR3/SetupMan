// Package settings stores SetupMan's own settings in %APPDATA%\SetupMan\settings.json
// (never inside a setup folder). See spec D-33, D-36, D-37.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"setupman/internal/naming"
)

// Visibility of a game on the overview (spec D-31, D-37).
const (
	VisAuto = "auto" // show when installed
	VisShow = "show" // always show
	VisHide = "hide" // never show
)

// Game holds the per-game settings.
type Game struct {
	Visibility string `json:"visibility"`
	Folder     string `json:"folder"` // "" = use the detected folder
	// EnduranceMinutes: a stint longer than this counts as Endurance (spec D-67, D-69). 0 = default (45).
	EnduranceMinutes int `json:"enduranceMinutes"`
	// Target hot tyre pressures for pit stop advice, psi (D-124). 0 = default (27.0 dry, 30.0 wet).
	TyreTargetDry float64 `json:"tyreTargetDry,omitempty"`
	TyreTargetWet float64 `json:"tyreTargetWet,omitempty"`
	// Managed: SetupMan may write to this game's setups folder (staging, D-73). Only switched on
	// through the "manage" action, which makes a backup first (D-74).
	Managed bool `json:"managed"`
	// ManagedFolder: the folder management was switched on for (and backed up). If the setups
	// folder changes, management is off until the user switches it on again (D-115).
	ManagedFolder string `json:"managedFolder,omitempty"`
	// TidyOnGameClose: move new in-game saves to the library when the game closes (D-80, optional).
	TidyOnGameClose bool `json:"tidyOnGameClose"`
	// AutoRename: give new in-game saves a tidy name (opt-in, original feature list; managed only).
	AutoRename bool `json:"autoRename"`
}

// ManagedFor tells whether SetupMan may write to folder: management is on, and was switched
// on for this very folder (D-115).
func (g Game) ManagedFor(folder string) bool {
	return g.Managed && folder != "" && g.ManagedFolder != "" &&
		strings.EqualFold(filepath.Clean(g.ManagedFolder), filepath.Clean(folder))
}

// Settings are all user settings.
type Settings struct {
	Theme         string          `json:"theme"`     // "system", "light" or "dark"
	Threshold     float64         `json:"threshold"` // near-identical threshold in %
	DownloadLogos bool            `json:"downloadLogos"`
	Games         map[string]Game `json:"games"`
	// History (spec D-53, D-61): how similar setups must be to link them in the history.
	HistoryThreshold float64 `json:"historyThreshold"`
	NameThreshold    float64 `json:"nameThreshold"`
	// Network (D-100): other computers on the local network may open SetupMan with a PIN.
	Network Network `json:"network"`
	// LFMID is the driver's Low Fuel Motorsport ID (digits), for their next race in Race prep (D-142).
	// "" = not set. Sent only to LFM.
	LFMID string `json:"lfmId,omitempty"`
	// Initials are the driver's own initials (e.g. "ABC"), put in front of the names SetupMan gives
	// new in-game saves (naming standard, D-144). "" = none.
	Initials string `json:"initials,omitempty"`
	// TempRules: how each author writes temperatures in setup names (D-146), by initials in upper
	// case; "*" = everyone else. Empty = the defaults (two numbers air then track, one number air).
	TempRules naming.TempRules `json:"tempRules,omitempty"`
}

// Network is access from other computers on the local network (spec D-100).
type Network struct {
	Allow bool   `json:"allow"`
	Port  int    `json:"port,omitempty"` // 0 = 8765; the --port option overrides it
	PIN   string `json:"pin,omitempty"`  // 6 digits, shown only on this PC
	// NoPIN: other devices on the network may use SetupMan without the PIN (opt-out, D-139). Off by
	// default: the PIN is required unless the user switches it off in Settings → Network.
	NoPIN bool   `json:"noPin,omitempty"`
	Token string `json:"token,omitempty"` // proves a browser entered the PIN; changes with the PIN
	// HostName is a name such as setupman.lan that a reverse proxy (e.g. Nginx Proxy Manager)
	// forwards to SetupMan (D-129). Empty = only IP addresses are accepted from other devices.
	HostName string `json:"hostName,omitempty"`
}

// Defaults returns the settings used before the user changes anything.
func Defaults() Settings {
	return Settings{Theme: "system", Threshold: 95, DownloadLogos: true, Games: map[string]Game{},
		HistoryThreshold: 80, NameThreshold: 65}
}

// GameOrDefault returns the settings for one game.
func (s Settings) GameOrDefault(id string) Game {
	if g, ok := s.Games[id]; ok {
		return g
	}
	return Game{Visibility: VisAuto}
}

// Validate checks values coming from the UI.
func (s Settings) Validate() error {
	switch s.Theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("unknown theme %q", s.Theme)
	}
	if s.Threshold < 50 || s.Threshold > 100 {
		return errors.New("threshold must be between 50 and 100")
	}
	if s.HistoryThreshold < 50 || s.HistoryThreshold > 100 || s.NameThreshold < 40 || s.NameThreshold > 100 {
		return errors.New("history thresholds must be between 50 and 100 (names: 40 and 100)")
	}
	for id, g := range s.Games {
		switch g.Visibility {
		case VisAuto, VisShow, VisHide:
		default:
			return fmt.Errorf("game %s: unknown visibility %q", id, g.Visibility)
		}
		if g.EnduranceMinutes < 0 || g.EnduranceMinutes > 1440 {
			return fmt.Errorf("game %s: endurance stint length must be between 0 and 1440 minutes", id)
		}
		for _, v := range []float64{g.TyreTargetDry, g.TyreTargetWet} {
			if v != 0 && (v < 15 || v > 40) {
				return fmt.Errorf("game %s: target tyre pressures must be between 15 and 40 psi", id)
			}
		}
	}
	return nil
}

// Store reads and writes the settings file.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore uses the given file path.
func NewStore(path string) *Store { return &Store{path: path} }

// DefaultPath returns %APPDATA%\SetupMan\settings.json (or the OS equivalent).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "SetupMan", "settings.json"), nil
}

// Load returns the saved settings, filling in defaults. A missing or damaged file gives defaults.
func (s *Store) Load() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Defaults()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return st
	}
	var raw struct {
		Settings
		Folder string `json:"folder"` // Phase 1 setting: the last scanned (ACC) folder
	}
	raw.Settings = st
	if json.Unmarshal(b, &raw) != nil {
		return Defaults()
	}
	st = raw.Settings
	if st.Games == nil {
		st.Games = map[string]Game{}
	}
	// Migrate Phase 1: a scanned folder becomes ACC's folder, shown on the overview.
	if _, ok := st.Games["acc"]; !ok && raw.Folder != "" {
		st.Games["acc"] = Game{Visibility: VisShow, Folder: raw.Folder}
	}
	if st.Validate() != nil {
		return Defaults()
	}
	return st
}

// Save validates and writes the settings.
func (s *Store) Save(st Settings) error {
	if err := st.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file first so a crash never leaves a half-written settings file.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
