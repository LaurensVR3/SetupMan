package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
)

func TestEditSetup(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idA := td.SetupIDs[0]
	aPath := filepath.Join(folder, "ferrari_296_gt3", "Spa", "a.json")
	before, _ := os.ReadFile(aPath)

	// Not managed: the change goes to the library only; the file is untouched.
	res, err := l.EditSetup(idA, map[string]string{"advancedSetup.mechanicalBalance.brakeBias": "42"}, "")
	if err != nil || res.InGameFolder || !strings.Contains(res.Changes, "Brake bias +2") {
		t.Fatalf("edit unmanaged: %+v %v", res, err)
	}
	if now, _ := os.ReadFile(aPath); string(now) != string(before) {
		t.Fatal("file changed although the folder is not managed")
	}
	vs, _ := l.db.Versions(idA)
	if len(vs) != 2 || vs[1].Source != "edited" {
		t.Fatalf("versions: %+v", vs)
	}
	if _, err := l.EditSetup(idA, map[string]string{"advancedSetup.mechanicalBalance.brakeBias": "42"}, ""); err == nil {
		t.Error("an edit that changes nothing was accepted")
	}
	// A rescan must not undo the library-only edit by recording the older file as newest.
	l.Rescan("acc")
	if vs, _ := l.db.Versions(idA); len(vs) != 2 || vs[1].Source != "edited" {
		t.Fatalf("rescan undid the edit: %+v", vs)
	}
	if s, _ := l.db.Setup(idA); s.DiskVersionID != vs[0].ID {
		t.Errorf("the folder should still hold version 1: disk=%d", s.DiskVersionID)
	}

	// Managed: the change is written to the game folder.
	if mr, err := l.SetManaged("acc", true); err != nil || !mr.Managed {
		t.Fatalf("manage: %+v %v", mr, err)
	}
	res, err = l.EditSetup(idA, map[string]string{"advancedSetup.aeroBalance.rearWing": "8"}, "")
	if err != nil || !res.InGameFolder {
		t.Fatalf("edit managed: %+v %v", res, err)
	}
	now, _ := os.ReadFile(aPath)
	if !strings.Contains(string(now), `"rearWing": 8`) || !strings.Contains(string(now), `"brakeBias": 42`) {
		t.Errorf("file does not have the edited values:\n%s", now)
	}

	// Save as a new setup: a new file next to it, linked to where it came from.
	res, err = l.EditSetup(idA, map[string]string{"basicSetup.strategy.fuel": "60"}, "ABC R 60m")
	if err != nil || res.SetupID == idA {
		t.Fatalf("save as new: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "ferrari_296_gt3", "Spa", "ABC R 60m.json")); err != nil {
		t.Error("new setup not in the game folder")
	}
	d, err := l.SetupDetail(res.SetupID)
	if err != nil || d.Parent == nil || d.Parent.SetupID != idA || d.Setup.Session != "Endurance" {
		t.Errorf("new setup detail: parent=%+v session=%q err=%v", d.Parent, d.Setup.Session, err)
	}
	if _, err := l.EditSetup(idA, nil, `bad/name`); err == nil {
		t.Error("invalid name accepted")
	}
}
