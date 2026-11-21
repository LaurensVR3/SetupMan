//go:build !windows

package gamekit

import "time"

// The games only run on Windows; elsewhere nothing is found.

func SteamLibraries() []string               { return nil }
func Documents() string                      { return "" }
func SavedGames() string                     { return "" }
func LocalAppData() string                   { return "" }
func processNames() (map[string]bool, error) { return map[string]bool{}, nil }

func StartedAt(exes ...string) (time.Time, bool) { return time.Time{}, false }
