package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
)

// Telemetry laps to 99 (D-176): only with folder management, a backup first, only that value
// changes, the file date stays and the setup gets no new version.
func TestSetTelemetryLaps(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	id := td.SetupIDs[0]
	s, _ := l.db.Setup(id)
	file := filepath.Join(folder, filepath.FromSlash(s.RelPath))
	before, _ := os.ReadFile(file)
	infoBefore, _ := os.Stat(file)
	versionsBefore, _ := l.db.Versions(id)

	plan, err := l.TelemetryLapsPreview("acc")
	if err != nil || len(plan) != 3 || plan[0].Laps != "0" {
		t.Fatalf("preview: %+v %v", plan, err)
	}
	if _, err := l.SetTelemetryLaps("acc", []int64{id}); err != ErrNotManaged {
		t.Fatalf("expected ErrNotManaged, got %v", err)
	}
	if _, err := l.SetManaged("acc", true); err != nil {
		t.Fatal(err)
	}
	backups, _ := l.Backups.List("acc")
	res, err := l.SetTelemetryLaps("acc", []int64{id})
	if err != nil || len(res) != 1 || res[0].Error != "" {
		t.Fatalf("set: %+v %v", res, err)
	}
	after, _ := os.ReadFile(file)
	if !strings.Contains(string(after), `"telemetryLaps": 99`) {
		t.Errorf("telemetry laps not set:\n%s", after)
	}
	if strings.Replace(string(before), `"telemetryLaps": 0`, `"telemetryLaps": 99`, 1) != string(after) {
		t.Error("more than the telemetry laps value changed")
	}
	if info, _ := os.Stat(file); !info.ModTime().Equal(infoBefore.ModTime()) {
		t.Errorf("file date changed: %v, was %v", info.ModTime(), infoBefore.ModTime())
	}
	if now, _ := l.Backups.List("acc"); len(now) != len(backups)+1 {
		t.Errorf("no backup before the change: %d backups, was %d", len(now), len(backups))
	}
	l.Rescan("acc")
	if vs, _ := l.db.Versions(id); len(vs) != len(versionsBefore) {
		t.Errorf("telemetry laps made a new version: %d, was %d", len(vs), len(versionsBefore))
	}
	if plan, _ := l.TelemetryLapsPreview("acc"); len(plan) != 2 {
		t.Errorf("after the change 2 setups should be left: %+v", plan)
	}

	g.mu.Lock()
	g.live = game.LiveDriving
	g.mu.Unlock()
	waitFor(t, "driving", func() bool { return l.Live() == game.LiveDriving })
	if _, err := l.SetTelemetryLaps("acc", []int64{plan[0].ID}); err == nil {
		t.Error("changed setups while driving")
	}
}
