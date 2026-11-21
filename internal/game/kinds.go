package game

import "strings"

// Optional properties of a game. Games that don't implement them get the defaults below
// (the ACC behaviour).

// Shorter gives the short name used in buttons and messages, e.g. "AC EVO" ("Show in AC EVO").
type Shorter interface{ Short() string }

// SetupFiles is implemented by games whose setup files are not .json, e.g. ".ini" (AC).
type SetupFiles interface{ SetupExt() string }

// ReadOnly is implemented by games whose setups SetupMan can only read, never write, e.g. because
// the game keeps all setups in one save file (AC Rally). The reason is shown to the user.
type ReadOnly interface{ ReadOnlyReason() string }

// Short returns the game's short name (default: its ID in capitals, e.g. "ACC").
func Short(g Game) string {
	if s, ok := g.(Shorter); ok {
		return s.Short()
	}
	return strings.ToUpper(g.ID())
}

// Ext returns the file extension of the game's setup files, with the dot (default ".json").
func Ext(g Game) string {
	if f, ok := g.(SetupFiles); ok {
		return f.SetupExt()
	}
	return ".json"
}

// ReadOnlyReason returns why SetupMan never writes this game's setups ("" = it may, when allowed).
func ReadOnlyReason(g Game) string {
	if r, ok := g.(ReadOnly); ok {
		return r.ReadOnlyReason()
	}
	return ""
}
