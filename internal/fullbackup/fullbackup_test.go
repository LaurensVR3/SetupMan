package fullbackup

import (
	"os"
	"path/filepath"
	"testing"

	"setupman/internal/store"
)

func TestCreateRestore(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "library.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s := &store.Setup{Game: "acc", Car: "c", Track: "t", Name: "before"}
	if err := db.InsertSetup(s); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte(`{"theme":"dark"}`), 0o644)
	folderZip := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(folderZip, []byte("folder backup bytes"), 0o644)

	fb := &Store{DataDir: dataDir}
	sum, err := fb.Create(ReasonManual, db, map[FolderRef]string{{Game: "acc", ID: "2026-11-18_140000"}: folderZip})
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Games) != 1 || sum.Reason != ReasonManual {
		t.Errorf("summary: %+v", sum)
	}

	// Change things after the backup.
	s.Name = "after"
	db.UpdateSetup(s)
	os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte(`{"theme":"light"}`), 0o644)

	if err := fb.ScheduleRestore(sum.ID); err != nil {
		t.Fatal(err)
	}
	if !fb.Pending() {
		t.Fatal("restore not pending")
	}
	db.Close() // SetupMan restarts: the library is closed before the restore

	folderDir := filepath.Join(dataDir, "backups")
	safetyRan := false
	applied, err := fb.ApplyPending(func() error {
		sdb, err := store.Open(dbPath)
		if err != nil {
			return err
		}
		defer sdb.Close()
		_, err = fb.Create(ReasonBeforeRestore, sdb, nil)
		safetyRan = err == nil
		return err
	}, folderDir)
	if err != nil || !applied {
		t.Fatalf("apply: %v %v", applied, err)
	}
	if !safetyRan || fb.Pending() {
		t.Error("no safety backup, or still pending")
	}
	db2, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if got, _ := db2.Setup(s.ID); got == nil || got.Name != "before" {
		t.Errorf("library not restored: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dataDir, "settings.json")); string(b) != `{"theme":"dark"}` {
		t.Errorf("settings not restored: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(folderDir, "acc", "2026-11-18_140000.zip")); string(b) != "folder backup bytes" {
		t.Error("folder backup not added to the ACC setups folder backups")
	}
	list, _ := fb.List()
	if len(list) != 2 || list[0].Reason != ReasonBeforeRestore {
		t.Errorf("list: %+v", list)
	}

	// A damaged or foreign file is refused.
	if _, err := fb.AddFile([]byte("not a zip")); err == nil {
		t.Error("accepted a non-zip")
	}
	good, _ := os.ReadFile(fb.Path(sum.ID))
	if added, err := fb.AddFile(good); err != nil || added.ID == sum.ID {
		t.Errorf("add file: %+v %v", added, err)
	}
	if fb.Path("../evil") != "" {
		t.Error("path traversal id accepted")
	}
}
