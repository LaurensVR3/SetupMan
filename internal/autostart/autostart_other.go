//go:build !windows

package autostart

import "errors"

// Supported reports whether autostart works on this system.
const Supported = false

// Enabled reports whether SetupMan starts with the system (never, outside Windows).
func Enabled() bool { return false }

// Set is not available outside Windows.
func Set(on bool) error { return errors.New("start with Windows is only available on Windows") }

// Refresh does nothing outside Windows.
func Refresh() error { return nil }
