package gamekit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The cheap per-process lookup must find the same programs as a full snapshot.
func TestProcessNames(t *testing.T) {
	full, err := snapshotNames()
	if err != nil {
		t.Fatal(err)
	}
	quick, err := processNames()
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	me := strings.ToLower(filepath.Base(self))
	if !quick[me] || !full[me] {
		t.Fatalf("own program %q not found (quick %v, full %v)", me, quick[me], full[me])
	}
	missing := 0
	for n := range full {
		if !quick[n] {
			missing++ // a program that ended between the two lists
		}
	}
	if missing > 3 {
		t.Errorf("%d programs missing from the quick list", missing)
	}
	start := time.Now()
	for i := 0; i < 50; i++ {
		processNames()
	}
	t.Logf("quick check: %v (full snapshot was used %v ago)", time.Since(start)/50, time.Since(procNames.snapshot))
}
