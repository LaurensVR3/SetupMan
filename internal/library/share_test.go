package library

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"setupman/internal/game"
)

func TestShare(t *testing.T) {
	// Sender.
	g := &fakeGame{installed: true, folder: setupFolder(t), live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	id := td.SetupIDs[0]
	s, _ := l.db.Setup(id)
	s.Note = "Great in the wet. PRIVATE"
	l.db.UpdateSetup(s)

	data, name, err := l.Share(id, false, true)
	if err != nil || name != "a.setupman" {
		t.Fatalf("share: %q %v", name, err)
	}
	var sh Shared
	json.Unmarshal(data, &sh)
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	if !bytes.Equal(sh.Setup, base) || sh.Car != "ferrari_296_gt3" || sh.Note != "" {
		t.Errorf("shared content wrong (note should be left out): %+v", sh.Note)
	}
	withNote, _, _ := l.Share(id, true, false)

	// Receiver: another SetupMan with an empty folder.
	g2 := &fakeGame{installed: true, folder: t.TempDir(), live: game.LiveUnknown}
	l2, _ := newLib(t, g2)
	l2.Start()
	waitFor(t, "receiver scan", func() bool { return l2.Games()[0].Status != "" })

	it, err := l2.ImportShared(withNote, "", "")
	if err != nil || it.Result != "imported" {
		t.Fatalf("import: %+v %v", it, err)
	}
	got, _ := l2.db.Setup(it.SetupID)
	if got.Name != "a" || got.Car != "ferrari_296_gt3" || got.Track != "spa" || got.RelPath != "" ||
		!strings.Contains(got.Note, "Their notes: Great in the wet.") {
		t.Errorf("imported: %+v", got)
	}
	v, _ := l2.db.LatestVersion(got.ID)
	if !bytes.Equal(v.Content, base) {
		t.Error("setup bytes changed on the way")
	}
	// Twice → already there; on a track page for another track → skipped; junk → error.
	if it, _ := l2.ImportShared(withNote, "", ""); it.Result != "skipped" || !strings.Contains(it.Reason, "already") {
		t.Errorf("second import: %+v", it)
	}
	res, _ := l2.Import("acc", "ferrari_296_gt3", "monza", []ImportFile{{Name: "a.setupman", Data: data}})
	if res.Imported != 0 || res.Skipped != 1 {
		t.Errorf("other track: %+v", res)
	}
	if _, err := l2.ImportShared([]byte(`{"kind":"other"}`), "", ""); err == nil {
		t.Error("accepted a non-shared file")
	}
}
