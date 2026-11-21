package library

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
	"setupman/internal/settings"
)

// snapshot records every file (name with case, content hash, date) under root.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		info, _ := d.Info()
		rel, _ := filepath.Rel(root, p)
		sum := sha256.Sum256(b)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:]) + "@" + info.ModTime().UTC().Format(time.RFC3339Nano)
		return nil
	})
	return out
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestStagingEndToEnd(t *testing.T) {
	folder := setupFolder(t) // ferrari_296_gt3/Spa/{a,b}.json, bmw_m4_gt4/Monza/m.json
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, store := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	start := snapshot(t, folder)
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	idA, idB := td.SetupIDs[0], td.SetupIDs[1]

	// Not managed yet: nothing may be written (D-73).
	if _, err := l.Unstage(idA); err != ErrNotManaged {
		t.Fatalf("expected ErrNotManaged, got %v", err)
	}

	// Switching on: backup + byte-for-byte check (D-74).
	mr, err := l.SetManaged("acc", true)
	if err != nil || !mr.Managed || mr.BackupID == "" || mr.Checked != 3 || len(mr.Problems) != 0 {
		t.Fatalf("manage: %+v %v", mr, err)
	}
	if !store.Load().Games["acc"].Managed {
		t.Error("managed not saved in settings")
	}
	if again, _ := l.SetManaged("acc", true); !again.Managed || again.BackupID != "" {
		t.Errorf("switching on twice made another backup: %+v", again)
	}

	// Keep only a: b goes to the library, the other track is untouched (D-75).
	if _, err := l.KeepOnly("acc", "ferrari_296_gt3", "spa", []int64{idA}); err != nil {
		t.Fatal(err)
	}
	if got := filesIn(t, spa); !reflect.DeepEqual(got, []string{"a.json"}) {
		t.Fatalf("spa folder after keep-only: %v", got)
	}
	if _, err := os.Stat(filepath.Join(folder, "bmw_m4_gt4", "Monza", "m.json")); err != nil {
		t.Error("other track was touched")
	}
	sb, _ := l.db.Setup(idB)
	if sb.RelPath != "" {
		t.Errorf("b should be library only: %+v", sb)
	}
	if vs, _ := l.db.Versions(idB); len(vs) != 1 {
		t.Error("b's history lost")
	}

	// Put b back: same name, same bytes.
	orig := start["ferrari_296_gt3/Spa/b.json"]
	if _, err := l.Stage(idB, 0); err != nil {
		t.Fatal(err)
	}
	if got := filesIn(t, spa); !reflect.DeepEqual(got, []string{"a.json", "b.json"}) {
		t.Fatalf("after staging b: %v", got)
	}
	if strings.Split(snapshot(t, folder)["ferrari_296_gt3/Spa/b.json"], "@")[0] != strings.Split(orig, "@")[0] {
		t.Error("staged b differs from the original bytes")
	}
	if got := snapshot(t, folder)["ferrari_296_gt3/Spa/b.json"]; got[:84] != orig[:84] { // hash @ date to the second
		t.Errorf("staged b should keep its bytes and original date: %s, want %s", got, orig)
	}

	// a is changed "in the game", then version 1 is put back (restored).
	aPath := filepath.Join(spa, "a.json")
	b1, _ := os.ReadFile(aPath)
	os.WriteFile(aPath, []byte(strings.Replace(string(b1), `"brakeBias": 40`, `"brakeBias": 44`, 1)), 0o644)
	l.Rescan("acc")
	vs, _ := l.db.Versions(idA)
	if len(vs) != 2 {
		t.Fatalf("expected 2 versions of a, got %d", len(vs))
	}
	if _, err := l.Stage(idA, vs[0].ID); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(aPath)
	if string(now) != string(b1) {
		t.Error("version 1 was not put back exactly")
	}
	vs, _ = l.db.Versions(idA)
	if len(vs) != 3 || vs[2].Source != "restored" {
		t.Errorf("restored version not recorded: %+v", vs)
	}

	// A file copied in while the game is closed is not a save from the game (D-114).
	os.WriteFile(filepath.Join(spa, "copied in.json"), b1, 0o644)
	l.Rescan("acc")
	td, _ = l.Track("acc", "ferrari_296_gt3", "spa")
	for i, s := range td.Setups {
		if s.Name == "copied in" {
			if c, _ := l.db.Setup(td.SetupIDs[i]); c == nil || c.Fresh {
				t.Errorf("copied-in file marked as a save from the game: %+v", c)
			}
		}
	}

	// A new save from the game (game running) in this managed folder is marked; Keep clears it (D-80).
	setLive(t, l, g, game.LiveNotDriving)
	os.WriteFile(filepath.Join(spa, "new save.json"), b1, 0o644)
	l.Rescan("acc")
	td, _ = l.Track("acc", "ferrari_296_gt3", "spa")
	var fresh int64
	for i, s := range td.Setups {
		if s.Name == "new save" {
			fresh = td.SetupIDs[i]
		}
	}
	if s, _ := l.db.Setup(fresh); s == nil || !s.Fresh {
		t.Fatalf("new save not marked: %+v", s)
	}
	if err := l.Keep(fresh); err != nil {
		t.Fatal(err)
	}
	if s, _ := l.db.Setup(fresh); s.Fresh {
		t.Error("keep did not clear the mark")
	}

	// While driving, a change is queued and runs once driving stops (D-79).
	g.mu.Lock()
	g.live = game.LiveDriving
	g.mu.Unlock()
	waitFor(t, "driving", func() bool { return l.Live() == game.LiveDriving })
	r, err := l.Unstage(idB)
	if err != nil || !r.Queued || l.QueuedCount() != 1 {
		t.Fatalf("expected queued: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(spa, "b.json")); err != nil {
		t.Fatal("file changed while driving")
	}
	g.mu.Lock()
	g.live = game.LiveNotDriving
	g.mu.Unlock()
	waitFor(t, "queued change to run", func() bool {
		_, err := os.Stat(filepath.Join(spa, "b.json"))
		return os.IsNotExist(err) && l.QueuedCount() == 0
	})

	// Tidy when the game closes (optional, D-80): an unreviewed save goes to the library.
	st := store.Load()
	gs := st.Games["acc"]
	gs.TidyOnGameClose = true
	st.Games["acc"] = gs
	store.Save(st)
	l.Apply()
	os.WriteFile(filepath.Join(spa, "quick save.json"), b1, 0o644)
	l.Rescan("acc")
	g.mu.Lock()
	g.live = game.LiveUnknown // the game closed
	g.mu.Unlock()
	waitFor(t, "tidy on game close", func() bool {
		_, err := os.Stat(filepath.Join(spa, "quick save.json"))
		return os.IsNotExist(err)
	})
	if _, err := os.Stat(filepath.Join(spa, "new save.json")); err != nil {
		t.Error("a kept save was tidied away")
	}

	// Restore the first backup: the folder is exactly as at the start, no duplicate setups (D-81).
	before, _ := l.db.SetupsForGame("acc")
	if _, err := l.RestoreBackup("acc", mr.BackupID); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, folder); !reflect.DeepEqual(got, start) {
		t.Fatalf("folder not identical after restore:\n got  %v\n want %v", got, start)
	}
	after, _ := l.db.SetupsForGame("acc")
	if len(after) != len(before) {
		t.Errorf("restore created duplicates: %d → %d setups", len(before), len(after))
	}
	if b, _ := l.db.Setup(idB); b.RelPath == "" {
		t.Error("b should be back in the folder (re-attached) after the restore")
	}
	backups, _ := l.Backups.List("acc")
	if len(backups) != 2 { // the first one + the automatic one before restoring
		t.Errorf("expected 2 backups, got %d", len(backups))
	}
	_ = settings.VisShow
}

func TestFreeName(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "car", "Spa"), 0o755)
	if got := freeName(root, "car", "Spa", "quali", ".json"); got != "car/Spa/quali.json" {
		t.Errorf("got %q", got)
	}
	os.WriteFile(filepath.Join(root, "car", "Spa", "quali.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(root, "car", "Spa", "quali (2).json"), []byte("{}"), 0o644)
	if got := freeName(root, "car", "Spa", "quali", ".json"); got != "car/Spa/quali (3).json" {
		t.Errorf("got %q", got)
	}
	if isInside(root, filepath.Join(root, "..", "evil.json")) || !isInside(root, filepath.Join(root, "car", "x.json")) {
		t.Error("isInside wrong")
	}
}

// Management belongs to the folder it was switched on for (D-115).
func TestManagedFollowsFolder(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, store := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	if _, err := l.SetManaged("acc", true); err != nil || !l.managed("acc") {
		t.Fatalf("manage: %v", err)
	}
	if l.Games()[0].Managed != true {
		t.Error("summary does not show managed")
	}

	// Another folder: not managed, nothing may be written there.
	other := setupFolder(t)
	st := store.Load()
	gs := st.Games["acc"]
	gs.Folder = other
	st.Games["acc"] = gs
	store.Save(st)
	l.Apply()
	waitFor(t, "scan of the other folder", func() bool { return l.Games()[0].Folder == other && l.Games()[0].Status == "ok" })
	if l.managed("acc") || l.Games()[0].Managed {
		t.Fatal("still managed after the folder changed")
	}
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	if _, err := l.Unstage(td.SetupIDs[0]); err != ErrNotManaged {
		t.Errorf("wrote to a folder that was never backed up: %v", err)
	}

	// Back to the first folder: managed again (its backup and check are still valid).
	gs.Folder = folder
	st.Games["acc"] = gs
	store.Save(st)
	l.Apply()
	waitFor(t, "back to the first folder", func() bool { return l.managed("acc") })
}

// Settings from before D-115 get their folder from the "before managing" backup.
func TestMigrateManagedFolders(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder}
	l, store := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	if _, err := l.Backups.Create("acc", folder, folderbackup.ReasonBeforeManaging); err != nil {
		t.Fatal(err)
	}
	st := store.Load()
	st.Games["acc"] = settings.Game{Visibility: settings.VisShow, Managed: true}   // old settings
	st.Games["other"] = settings.Game{Visibility: settings.VisShow, Managed: true} // no backup at all
	store.Save(st)
	l.migrateManagedFolders()
	got := store.Load()
	if a := got.Games["acc"]; !a.Managed || a.ManagedFolder != folder {
		t.Errorf("acc: %+v", a)
	}
	if o := got.Games["other"]; o.Managed {
		t.Errorf("managed without a backup: %+v", o)
	}
}
