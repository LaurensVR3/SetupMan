//go:build !windows

package lastopened

import (
	"os"
	"time"
)

// The games only run on Windows; elsewhere files are read normally and no time is known.

func Open(path string) (*os.File, error)       { return os.Open(path) }
func accessTime(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
func enabledSetting() (enabled, known bool)    { return false, false }
