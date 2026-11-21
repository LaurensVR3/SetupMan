package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
)

func TestRename(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, store := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idA := td.SetupIDs[0]
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")

	if _, err := l.RenameSetup(idA, "ABC R 2327"); err == nil {
		t.Fatal("renamed a file in an unmanaged folder")
	}
	if _, err := l.SetManaged("acc", true); err != nil {
		t.Fatal(err)
	}
	got, err := l.RenameSetup(idA, "ABC R 2327")
	if err != nil || got != "ABC R 2327" {
		t.Fatalf("rename: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(spa, "ABC R 2327.json")); err != nil {
		t.Error("file not renamed")
	}
	if _, err := os.Stat(filepath.Join(spa, "a.json")); !os.IsNotExist(err) {
		t.Error("old file still there")
	}
	all, _ := l.db.SetupsForGame("acc")
	if len(all) != 3 {
		t.Errorf("rename created extra setups: %d", len(all))
	}
	if _, err := l.RenameSetup(idA, `bad:name`); err == nil {
		t.Error("invalid name accepted")
	}
	// A taken name gets a number.
	if got, _ := l.RenameSetup(td.SetupIDs[1], "ABC R 2327"); got != "ABC R 2327 (2)" {
		t.Errorf("taken name: %q", got)
	}

	// Automatic renaming of a new in-game save (opt-in).
	st := store.Load()
	gs := st.Games["acc"]
	gs.AutoRename = true
	st.Games["acc"] = gs
	store.Save(st)
	l.Apply()
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	// Copied in while the game is closed: keeps its name (D-114).
	copied := filepath.Join(spa, "copied 2026 q.json")
	os.WriteFile(copied, []byte(strings.Replace(string(base), `"rearWing": 6`, `"rearWing": 4`, 1)), 0o644)
	waitFor(t, "copied file scanned", func() bool {
		td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
		return len(td.Setups) == 3
	})
	time.Sleep(3 * l.Debounce)
	if _, err := os.Stat(copied); err != nil {
		t.Error("file copied in while the game is closed was renamed")
	}
	setLive(t, l, g, game.LiveNotDriving) // in-game saves happen while the game runs
	os.WriteFile(filepath.Join(spa, "ABC test 2026 q.json"), []byte(strings.Replace(string(base), `"rearWing": 6`, `"rearWing": 2`, 1)), 0o644)
	// The new name also gets the game version current on the file date (D-103), e.g. "ABC Q 2026 1.10.4 test".
	renamedTo := func() string {
		m, _ := filepath.Glob(filepath.Join(spa, "ABC Q 2026 *test.json"))
		if len(m) == 1 {
			return strings.TrimSuffix(filepath.Base(m[0]), ".json")
		}
		return ""
	}
	// The note is saved just after the file is renamed, so wait for both.
	noteOf := func() string {
		name := renamedTo()
		all, _ := l.db.SetupsForGame("acc")
		for _, s := range all {
			if name != "" && s.Name == name {
				return s.Note
			}
		}
		return ""
	}
	waitFor(t, "auto rename with note", func() bool {
		return strings.Contains(noteOf(), `Saved in the game as "ABC test 2026 q"`)
	})
}

// The whole-game naming standard: preview first, then one bulk rename with a single rescan.
func TestTidyWholeGame(t *testing.T) {
	folder := setupFolder(t)
	os.WriteFile(filepath.Join(folder, "ferrari_296_gt3", "Spa", "ABC 2327 q.json"), mustRead(t, "../games/acc/testdata/base.json"), 0o644)
	os.WriteFile(filepath.Join(folder, "bmw_m4_gt4", "Monza", "race 45m.json"), mustRead(t, "../games/acc/testdata/base.json"), 0o644)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	if _, err := l.SetManaged("acc", true); err != nil {
		t.Fatal(err)
	}
	plan, err := l.TidyPreview("acc")
	if err != nil || len(plan) < 2 {
		t.Fatalf("preview: %+v %v", plan, err)
	}
	ids := []int64{}
	for _, p := range plan {
		if p.CarName == "" || p.TrackName == "" || p.To == "" || p.To == p.From {
			t.Errorf("bad plan entry: %+v", p)
		}
		ids = append(ids, p.ID)
	}
	scans := func() int { g.mu.Lock(); defer g.mu.Unlock(); return g.scans }
	before := scans()
	res := l.RenameTidy(ids)
	if len(res) != len(plan) {
		t.Fatalf("renamed %d of %d", len(res), len(plan))
	}
	for i, r := range res {
		if r.Error != "" || r.To != plan[i].To {
			t.Errorf("%+v, planned %q", r, plan[i].To)
		}
	}
	if n := scans() - before; n != 1 {
		t.Errorf("%d rescans for one bulk rename, want 1", n)
	}
	if again, _ := l.TidyPreview("acc"); len(again) != 0 {
		t.Errorf("still to rename after applying: %+v", again)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
