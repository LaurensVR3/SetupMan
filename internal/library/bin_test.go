package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
)

func TestBin(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	a := td.SetupIDs[0]
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")

	// Not managed: a setup in ACC's folder can't be deleted (SetupMan may not remove the file).
	res, _ := l.DeleteSetups([]int64{a})
	if len(res) != 1 || res[0].Error == "" {
		t.Fatalf("deleted a file in an unmanaged folder: %+v", res)
	}
	if _, err := l.SetManaged("acc", true); err != nil {
		t.Fatal(err)
	}
	res, err := l.DeleteSetups([]int64{a})
	if err != nil || res[0].Error != "" {
		t.Fatalf("delete: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(spa, "a.json")); !os.IsNotExist(err) {
		t.Error("file still in ACC's folder")
	}
	all, _ := l.db.SetupsForGame("acc")
	if len(all) != 2 {
		t.Errorf("deleted setup still listed: %d", len(all))
	}
	bin, _ := l.Bin()
	if len(bin) != 1 || bin[0].ID != a || bin[0].GoneAt.Sub(bin[0].DeletedAt) != BinDays*24*time.Hour {
		t.Errorf("bin: %+v", bin)
	}
	// Restore: back in the lists, hidden from ACC.
	if _, err := l.RestoreDeleted(a); err != nil {
		t.Fatal(err)
	}
	s, _ := l.db.Setup(a)
	if !s.DeletedAt.IsZero() || s.RelPath != "" {
		t.Errorf("restored: %+v", s)
	}
	// After 30 days it is gone for good, with its versions.
	l.DeleteSetups([]int64{a})
	l.db.SetDeleted(a, time.Now().AddDate(0, 0, -31))
	l.purgeBin()
	if s, _ := l.db.Setup(a); s != nil {
		t.Error("not purged after 30 days")
	}
}

// Several setups on different cars are removed in one go, with one rescan (large libraries).
func TestDeleteMany(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	if _, err := l.SetManaged("acc", true); err != nil {
		t.Fatal(err)
	}
	spa, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	monza, _ := l.Track("acc", "bmw_m4_gt4", "monza")
	scans := func() int { g.mu.Lock(); defer g.mu.Unlock(); return g.scans }
	before := scans()
	res, err := l.DeleteSetups([]int64{spa.SetupIDs[1], monza.SetupIDs[0]})
	if err != nil || len(res) != 2 || res[0].Error != "" || res[1].Error != "" {
		t.Fatalf("delete: %+v %v", res, err)
	}
	for _, p := range []string{"ferrari_296_gt3/Spa/b.json", "bmw_m4_gt4/Monza/m.json"} {
		if _, err := os.Stat(filepath.Join(folder, p)); !os.IsNotExist(err) {
			t.Errorf("%s still in the folder", p)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "ferrari_296_gt3/Spa/a.json")); err != nil {
		t.Error("the setup to keep was removed")
	}
	if n := scans() - before; n > 1 {
		t.Errorf("%d rescans for one batch, want 1", n)
	}
	if bin, _ := l.Bin(); len(bin) != 2 {
		t.Errorf("bin holds %d", len(bin))
	}
}
