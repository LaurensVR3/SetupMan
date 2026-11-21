package gamekit

import (
	"strings"
	"sync"
	"time"
)

// A process snapshot costs several milliseconds, so all games share one, taken at most every
// few seconds (D-13: SetupMan must not cost CPU while driving).
const processCacheFor = 5 * time.Second

var procCache struct {
	sync.Mutex
	at    time.Time
	names map[string]bool // lower-case program file names
	ok    bool
}

// Check reports whether a program with one of these file names runs (case-insensitive).
// known is false when the process list could not be read.
func Check(exes ...string) (running, known bool) {
	procCache.Lock()
	if procCache.at.IsZero() || time.Since(procCache.at) >= processCacheFor {
		names, err := processNames()
		procCache.names, procCache.ok, procCache.at = names, err == nil, time.Now()
	}
	names, ok := procCache.names, procCache.ok
	procCache.Unlock()
	if !ok {
		return false, false
	}
	for _, x := range exes {
		if names[strings.ToLower(x)] {
			return true, true
		}
	}
	return false, true
}

// Running reports whether a program with one of these file names runs (false when unknown).
func Running(exes ...string) bool {
	r, _ := Check(exes...)
	return r
}
