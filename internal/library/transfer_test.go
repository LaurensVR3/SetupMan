package library

import (
	"archive/zip"
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"setupman/internal/game"
)

func TestExportImport(t *testing.T) {
	folder := setupFolder(t)
	g := &fakeGame{installed: true, folder: folder, live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	before := snapshot(t, folder)

	// Export: the game's own layout, exact bytes.
	var buf bytes.Buffer
	n, err := l.Export(td.SetupIDs, &buf)
	if err != nil || n != 2 {
		t.Fatalf("export: %d %v", n, err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	var got []string
	for _, f := range zr.File {
		got = append(got, f.Name)
		rc, _ := f.Open()
		var b bytes.Buffer
		b.ReadFrom(rc)
		rc.Close()
		if !bytes.Equal(b.Bytes(), base) {
			t.Errorf("%s: bytes differ", f.Name)
		}
	}
	if strings.Join(got, ",") != "ferrari_296_gt3/Spa/a.json,ferrari_296_gt3/Spa/b.json" {
		t.Errorf("zip entries: %v", got)
	}

	// Import: identical setups are skipped, a changed one is added to the library only.
	changed := strings.Replace(string(base), `"rearWing": 6`, `"rearWing": 1`, 1)
	otherCar := []byte(strings.Replace(changed, `"test_car_gt3"`, `"bmw_m4_gt4"`, 1))
	changed = strings.Replace(changed, `"test_car_gt3"`, `"ferrari_296_gt3"`, 1)
	res, err := l.Import("acc", "ferrari_296_gt3", "spa", []ImportFile{
		{Name: "export.zip", Data: buf.Bytes()},
		{Name: "a.json", Data: []byte(changed)},
		{Name: "bmw.json", Data: otherCar},
		{Name: "notes.txt", Data: []byte("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Skipped != 4 {
		t.Fatalf("imported %d, skipped %d: %+v", res.Imported, res.Skipped, res.Items)
	}
	var newID int64
	for _, it := range res.Items {
		if it.Result == "imported" {
			if it.Name != "a (2)" {
				t.Errorf("name clash not numbered: %q", it.Name)
			}
			newID = it.SetupID
		}
	}
	s, _ := l.db.Setup(newID)
	if s == nil || s.RelPath != "" {
		t.Errorf("imported setup should be library-only: %+v", s)
	}
	// Importing never touches the game folder.
	if after := snapshot(t, folder); !reflect.DeepEqual(after, before) {
		t.Error("the game folder changed during export/import")
	}
}
