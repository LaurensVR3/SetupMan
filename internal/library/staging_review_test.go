package library

// Tests for the review fixes of 2026-11-20 (D-138).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setupman/internal/folderbackup"
	"setupman/internal/game"
	"setupman/internal/settings"
	"setupman/internal/store"
)

// managedLib returns a library with folder management on, and the ids of Spa a and b. debounce
// (optional) is the watcher's quiet period, set before the library starts so no rescan armed
// with the default one can fire later.
func managedLib(t *testing.T, debounce ...time.Duration) (*Library, string, int64, int64) {
	t.Helper()
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	if len(debounce) > 0 {
		l.Debounce = debounce[0]
	}
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	if mr, err := l.SetManaged("acc", true); err != nil || !mr.Managed {
		t.Fatalf("manage: %+v %v", mr, err)
	}
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	ids := map[string]int64{}
	for i, s := range td.Setups {
		ids[s.Name] = td.SetupIDs[i]
	}
	return l, folder, ids["a"], ids["b"]
}

// A save from the game that SetupMan hasn't read yet is stored, never overwritten unasked.
func TestStageKeepsUnreadGameSave(t *testing.T) {
	l, folder, idA, _ := managedLib(t, time.Hour) // the watcher must not read the save before we stage
	file := filepath.Join(folder, "ferrari_296_gt3", "Spa", "a.json")
	raw, _ := os.ReadFile(file)
	saved := []byte(strings.Replace(string(raw), `"tC1": 4`, `"tC1": 7`, 1))
	os.WriteFile(file, saved, 0o644) // "saved in ACC" a moment ago

	if _, err := l.Stage(idA, 0); err == nil || !strings.Contains(err.Error(), "just saved") {
		t.Fatalf("stage over an unread save: %v", err)
	}
	if now, _ := os.ReadFile(file); string(now) != string(saved) {
		t.Fatal("the game's save was overwritten")
	}
	v, _ := l.db.LatestVersion(idA)
	if v == nil || string(v.Content) != string(saved) {
		t.Fatal("the game's save is not stored in the library")
	}
	// Now that it is stored, staging works (writes the same, newest version).
	if _, err := l.Stage(idA, 0); err != nil {
		t.Fatalf("second stage: %v", err)
	}
}

// Renaming to the same name with other capitals keeps the name, without " (2)".
func TestRenameCaseOnly(t *testing.T) {
	l, folder, idA, _ := managedLib(t)
	got, err := l.RenameSetup(idA, "A")
	if err != nil || got != "A" {
		t.Fatalf("rename: %q %v", got, err)
	}
	names := filesIn(t, filepath.Join(folder, "ferrari_296_gt3", "Spa"))
	found := false
	for _, n := range names {
		if n == "A.json" {
			found = true
		}
		if strings.HasPrefix(n, ".setupman") || strings.Contains(n, "(2)") {
			t.Errorf("unexpected file %s", n)
		}
	}
	if !found {
		t.Errorf("files: %v", names)
	}
}

// A queued change that fails later is reported in Recent changes.
func TestFailedQueuedChangeIsReported(t *testing.T) {
	l, _, idA, _ := managedLib(t)
	if err := l.db.AddPendingOp(store.PendingOp{Game: "acc", Kind: "no-such-change", Payload: map[string]any{"setupId": float64(idA)}}); err != nil {
		t.Fatal(err)
	}
	l.runPendingOps()
	evs, _ := l.db.Events(false, 20)
	for _, e := range evs {
		if e.Kind == "queued-failed" && e.SetupID == idA && e.Details["error"] != "" {
			return
		}
	}
	t.Fatalf("no queued-failed event: %+v", evs)
}

// Several setups shown / hidden in one go.
func TestStageUnstageMany(t *testing.T) {
	l, folder, idA, idB := managedLib(t)
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	_, list, err := l.UnstageMany([]int64{idA, idB, 999999})
	if err != nil || len(list) != 3 {
		t.Fatalf("unstage many: %v %+v", err, list)
	}
	errs := 0
	for _, r := range list {
		if r.Error != "" {
			errs++
		}
	}
	if errs != 1 || len(filesIn(t, spa)) != 0 {
		t.Fatalf("after hiding: %+v, files %v", list, filesIn(t, spa))
	}
	if _, list, err = l.StageMany([]int64{idA, idB}); err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.Error != "" {
			t.Errorf("stage %s: %s", r.Name, r.Error)
		}
	}
	if got := filesIn(t, spa); len(got) != 2 {
		t.Errorf("after showing: %v", got)
	}
}

// With "apply the naming standard to new saves" on, the game keeps saving under the name it knows.
// Such a save becomes a new version of the renamed setup, not a separate "(2)" setup (D-138).
func TestGameSaveAfterAutoRename(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveNotDriving} // the game runs
	l, _ := newLib(t, g)
	l.Backups = &folderbackup.Store{Dir: filepath.Join(t.TempDir(), "backups")}
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	if mr, err := l.SetManaged("acc", true); err != nil || !mr.Managed {
		t.Fatalf("manage: %+v %v", mr, err)
	}
	l.saveGameSetting("acc", func(gs *settings.Game) { gs.AutoRename = true })
	spa := filepath.Join(folder, "ferrari_296_gt3", "Spa")
	base, _ := os.ReadFile(filepath.Join(spa, "a.json"))
	save := func(tc string) {
		os.WriteFile(filepath.Join(spa, "2123R.json"), []byte(strings.Replace(string(base), `"tC1": 4`, `"tC1": `+tc, 1)), 0o644)
	}
	save("7")
	var renamed string
	waitFor(t, "auto-rename", func() bool {
		for _, n := range filesIn(t, spa) {
			if n != "a.json" && n != "b.json" && n != "2123R.json" {
				renamed = n
				return true
			}
		}
		return false
	})
	save("2") // saved again in the game, under the name the game knows
	waitFor(t, "merge", func() bool {
		for _, n := range filesIn(t, spa) {
			if n == "2123R.json" {
				return false
			}
		}
		return true
	})
	got := filesIn(t, spa)
	if len(got) != 3 {
		t.Fatalf("files: %v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(spa, renamed))
	if !strings.Contains(string(raw), `"tC1": 2`) {
		t.Error("the renamed file doesn't hold the new save")
	}
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	for i, s := range td.Setups {
		if s.Name+".json" == renamed {
			if vs, _ := l.db.Versions(td.SetupIDs[i]); len(vs) != 2 {
				t.Errorf("versions of %s: %d", s.Name, len(vs))
			}
			return
		}
	}
	t.Errorf("renamed setup not found among %d setups", len(td.Setups))
}
