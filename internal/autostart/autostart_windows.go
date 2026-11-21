// Package autostart turns "Start with Windows" on and off for the current Windows user
// (spec D-71). It uses the per-user Run key, so no administrator rights are needed.
package autostart

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// KeyPath and ValueName can be changed in tests.
var (
	KeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	ValueName = "SetupMan"
)

// Supported reports whether autostart works on this system.
const Supported = true

// Command is what Windows runs at sign-in: this exe, quietly in the tray.
func Command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return `"` + exe + `" --no-browser`, nil
}

// Enabled reports whether SetupMan starts with Windows.
func Enabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, KeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(ValueName)
	return err == nil && v != ""
}

// Set turns starting with Windows on or off.
func Set(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, KeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(ValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	cmd, err := Command()
	if err != nil {
		return err
	}
	return k.SetStringValue(ValueName, cmd)
}

// Refresh updates the stored command when autostart is on but SetupMan.exe was moved.
func Refresh() error {
	if !Enabled() {
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, KeyPath, registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	v, _, _ := k.GetStringValue(ValueName)
	k.Close()
	if cmd, err := Command(); err == nil && v != cmd {
		return Set(true)
	}
	return nil
}
