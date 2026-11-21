// Package lastopened reads setup files without leaving a trace, and tells when a file was last
// opened by anyone else (spec D-147). Windows records the moment a file was last read (the "last
// access" time). When a game loads a setup, that file gets the newest time in its folder, so
// SetupMan can see which setup is being driven. SetupMan's own reads must not change that time,
// so every read of a game's setups folder goes through ReadFile or Open.
package lastopened

import (
	"io"
	"os"
	"time"
)

// ReadFile reads a whole file without changing its last-opened time (where Windows allows that).
func ReadFile(path string) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Time returns when the file was last opened, from its directory information (looking does not
// change it). False when the system doesn't say.
func Time(fi os.FileInfo) (time.Time, bool) { return accessTime(fi) }

// Status says whether Windows records last-opened times.
type Status struct {
	Known   bool   `json:"known"`   // the setting could be read
	Enabled bool   `json:"enabled"` // last-opened times are recorded
	Command string `json:"command"` // the command that switches it on (administrator)
}

// EnableCommand switches recording on for good ("user managed, enabled"). On current Windows it takes
// effect immediately (rig, 2026-11-21: "no reboot required"); older versions may ask for a restart.
const EnableCommand = "fsutil behavior set disablelastaccess 0"

// Check reads the Windows setting.
func Check() Status {
	enabled, known := enabledSetting()
	return Status{Known: known, Enabled: enabled, Command: EnableCommand}
}
