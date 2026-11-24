package maps3d

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/telemetry"
)

func TestTerrainFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "terrain.bin")
	in := &Terrain{E0: -500, N0: -400, Step: 10, Cols: 3, Rows: 2, H: telemetry.Floats{1, 2, 3, 4, 5, 6}}
	if err := writeTerrain(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := readTerrain(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Cols != 3 || out.Rows != 2 || out.H[5] != 6 || out.E0 != -500 {
		t.Errorf("got %+v", out)
	}
	if h := out.At(-495, -400); math.Abs(h-1.5) > 1e-6 { // half way between the first two
		t.Errorf("At = %v, want 1.5", h)
	}
}

func TestPhotoTilesAndAddresses(t *testing.T) {
	g, ok := telemetry.Geo("zolder")
	if !ok {
		t.Fatal("no zolder data")
	}
	s, ok := SourceFor("zolder")
	if !ok || s.kind != "wms" {
		t.Fatalf("zolder source %+v", s)
	}
	tiles := tilesFor(g, s.Zoom)
	if len(tiles) < 50 || len(tiles) > 600 {
		t.Errorf("%d tiles for Zolder", len(tiles))
	}
	u := s.tileURL(tiles[0])
	if !strings.Contains(u, "REQUEST=GetMap") || !strings.Contains(u, "CRS=EPSG:3857") || !strings.Contains(u, "LAYERS=omwrgb25vl") {
		t.Errorf("wms address %s", u)
	}
	es := sources["es"]
	if u := es.tileURL(Tile{Z: 18, X: 10, Y: 5}); !strings.HasSuffix(u, "/18/10/262138.jpeg") { // counted from the south
		t.Errorf("tms address %s", u)
	}
	if _, ok := SourceFor("imola"); ok {
		t.Error("imola has no offline source with an open licence")
	}
}

func TestTrackNameChecked(t *testing.T) {
	m := New(t.TempDir())
	for _, bad := range []string{"../x", "a/b", "", "Spa"} {
		if _, err := m.trackDir(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if p := m.PhotoTile("../etc", Tile{18, 1, 1}); p != "" {
		t.Errorf("bad track gave %q", p)
	}
}
