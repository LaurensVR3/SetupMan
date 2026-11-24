package maps3d

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A tile the photo service doesn't have (404, the edge of its country) is skipped, not an error;
// other answers still stop the download.
func TestFetchTileSkipsMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/18/1/1.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("jpeg"))
		case "/18/2/2.jpg":
			http.NotFound(w, r)
		default:
			http.Error(w, "broken", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	m := New(t.TempDir())
	s := Source{ID: "test", Zoom: 18, kind: "xyz", url: srv.URL + "/{z}/{x}/{y}.jpg"}
	dir := t.TempDir()
	if n, err := m.fetchTile(s, dir, Tile{18, 1, 1}); err != nil || n != 4 {
		t.Fatalf("kept tile: %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "18", "1", "1.jpg")); err != nil {
		t.Errorf("tile not written: %v", err)
	}
	if n, err := m.fetchTile(s, dir, Tile{18, 2, 2}); err != nil || n != 0 {
		t.Errorf("missing tile: %d, %v (want skipped)", n, err)
	}
	if _, _, err := m.getTile(srv.URL + "/18/3/3.jpg"); err == nil {
		t.Errorf("a server error was not reported")
	}
}
