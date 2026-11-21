package library

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Hiding a setup whose file can't be removed (held open by another program, like the game or a
// virus scanner) leaves the library unchanged, so the next scan doesn't add it again (D-138).
func TestUnstageWhenFileCantBeRemoved(t *testing.T) {
	l, folder, idA, _ := managedLib(t)
	file := filepath.Join(folder, "ferrari_296_gt3", "Spa", "a.json")
	p, _ := windows.UTF16PtrFromString(file)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)

	if _, err := l.Unstage(idA); err == nil {
		t.Fatal("no error although the file could not be removed")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("file is gone")
	}
	s, _ := l.db.Setup(idA)
	if s.RelPath == "" {
		t.Error("library says hidden although the file is still in the folder")
	}
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	if len(td.Setups) != 2 || len(td.Removed) != 0 {
		t.Errorf("after rescan: %d in folder, %d library only", len(td.Setups), len(td.Removed))
	}
}
