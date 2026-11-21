// Package version tells which build of SetupMan is running.
package version

import (
	"runtime/debug"
	"time"
)

// Number is SetupMan's version. Raise it with every build that goes to the sim rig or to users.
const Number = "0.23.1"

// Info describes the running build.
type Info struct {
	Number string `json:"number"` // e.g. "0.9.1"
	Commit string `json:"commit"` // short git commit, e.g. "219cb9e" (empty if unknown)
	Built  string `json:"built"`  // time of that commit, e.g. "2026-11-18 14:02"
	Dirty  bool   `json:"dirty"`  // built with uncommitted changes
}

// Get reads the commit details Go stores in the program when it is built inside the git folder.
func Get() Info {
	i := Info{Number: Number}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return i
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				i.Commit = s.Value[:7]
			}
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				i.Built = t.Local().Format("2006-01-02 15:04")
			}
		case "vcs.modified":
			i.Dirty = s.Value == "true"
		}
	}
	return i
}

// String is the short form shown in the tray menu and the log, e.g. "0.9.1 (219cb9e)".
func (i Info) String() string {
	s := i.Number
	if i.Commit != "" {
		s += " (" + i.Commit
		if i.Dirty {
			s += "+"
		}
		s += ")"
	}
	return s
}
