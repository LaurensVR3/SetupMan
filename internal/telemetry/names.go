// Package telemetry indexes the game's own telemetry files (ACC's MoTeC logs, spec D-160), links
// them to recorded sessions, laps and setups, reads lap data for the Telemetry pages, and writes and
// reads SetupMan's small extra log (D-162). Game telemetry files are only ever read.
package telemetry

import (
	"regexp"
	"strings"
	"time"
)

// fileName is ACC's MoTeC file name: <track>-<car>-<n>-<yyyy.mm.dd>-<hh.mm.ss>.ld
// (e.g. "donington-ferrari_296_gt3-59-2026.11.17-22.06.38.ld"). Track and car ids have no dashes.
var fileName = regexp.MustCompile(`(?i)^([^-]+)-([^-]+)-(\d+)-(\d{4}\.\d{2}\.\d{2})-(\d{2}\.\d{2}\.\d{2})\.ld$`)

// ParseName reads track, car and the moment ACC saved the file (local time) from a MoTeC file name.
// The number before the date is the laps completed so far; ACC saves a session again and again.
func ParseName(name string) (track, car string, saved time.Time, ok bool) {
	m := fileName.FindStringSubmatch(name)
	if m == nil {
		return "", "", time.Time{}, false
	}
	t, err := time.ParseInLocation("2006.01.02 15.04.05", m[4]+" "+m[5], time.Local)
	if err != nil {
		return "", "", time.Time{}, false
	}
	return strings.ToLower(m[1]), m[2], t, true
}
