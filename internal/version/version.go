// Package version tells which build of SetupMan is running.
package version

import (
	"os"
	"runtime/debug"
	"time"
)

// Number is SetupMan's version. Raise it with every build that goes to the sim rig or to users.
const Number = "0.25.0"

// Info describes the running build.
type Info struct {
	Number string `json:"number"` // e.g. "0.9.1"
	Commit string `json:"commit"` // short git commit, e.g. "219cb9e" (empty if unknown)
	Built  string `json:"built"`  // when the program was built, e.g. "2026-11-18 14:02"
	Dirty  bool   `json:"dirty"`  // built with uncommitted changes
}

// Get reads the commit details Go stores in the program when it is built inside the git folder.
// Built is the time of the program file (when it was built); the commit time only when that is
// unknown, since a build with uncommitted changes can be days newer than its commit.
func Get() Info {
	i := Info{Number: Number}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = &debug.BuildInfo{}
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
	if exe, err := os.Executable(); err == nil {
		if st, err := os.Stat(exe); err == nil {
			i.Built = st.ModTime().Local().Format("2006-01-02 15:04")
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
