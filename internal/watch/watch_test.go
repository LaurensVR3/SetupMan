package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWatchReportsChangesInSubfolders(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("watching is Windows-only")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "car", "track")
	os.MkdirAll(sub, 0o755)

	events := make(chan struct{}, 100)
	stop, err := Watch(dir, func() { events <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(sub, "new.json"), []byte("{}"), 0o644)
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("no change reported for a new file in a subfolder")
	}

	stop() // must return promptly
	for len(events) > 0 {
		<-events
	}
	os.WriteFile(filepath.Join(sub, "after.json"), []byte("{}"), 0o644)
	select {
	case <-events:
		t.Error("change reported after stop")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatchMissingFolder(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("watching is Windows-only")
	}
	if _, err := Watch(filepath.Join(t.TempDir(), "nope"), func() {}); err == nil {
		t.Error("expected an error")
	}
}
