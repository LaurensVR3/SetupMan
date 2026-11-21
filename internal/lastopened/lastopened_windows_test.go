package lastopened

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A normal read moves the last-opened time; ReadFile does not, and leaves the content and the
// modified time alone. Skipped when Windows doesn't record last-opened times on this PC.
func TestReadFileLeavesNoTrace(t *testing.T) {
	if s := Check(); !s.Known || !s.Enabled {
		t.Skip("last-opened times are not recorded on this PC")
	}
	p := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(p, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	b, err := ReadFile(p)
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
	fi, _ := os.Stat(p)
	if at, ok := Time(fi); !ok || !at.Equal(old) {
		t.Fatalf("last opened after a quiet read = %v, want %v", at, old)
	}
	if !fi.ModTime().Equal(old) {
		t.Fatalf("modified time changed: %v", fi.ModTime())
	}
	if _, err := os.ReadFile(p); err != nil { // a normal read, like the game's
		t.Fatal(err)
	}
	fi, _ = os.Stat(p)
	if at, _ := Time(fi); time.Since(at) > time.Minute {
		t.Fatalf("a normal read did not move the last-opened time: %v", at)
	}
}
