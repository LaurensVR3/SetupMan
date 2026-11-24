// Package maps3d downloads and keeps the real-world data of the 3D telemetry view (spec D-190):
// terrain heights and, where a government offers them under an open licence, aerial photos.
// Everything lives in %APPDATA%\SetupMan\maps\<track>. MapTiler's photos are never stored here:
// the browser loads them itself, as MapTiler's terms require.
package maps3d

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"setupman/internal/telemetry"
)

// Margin is how far the 3D view's surroundings reach beyond the track's lines (Q-TL18).
const Margin = 400.0

// userAgent: a generic client name (no personal details, as for logo downloads).
const userAgent = "SetupMan/1 (3D telemetry maps)"

// Maps downloads and keeps the data per track.
type Maps struct {
	Dir    string
	Client *http.Client

	mu   sync.Mutex
	jobs map[string]*Job
}

// New makes a Maps keeping its files in dir.
func New(dir string) *Maps {
	return &Maps{Dir: dir, Client: &http.Client{Timeout: 30 * time.Second}, jobs: map[string]*Job{}}
}

var safeTrack = regexp.MustCompile(`^[a-z0-9_]+$`)

func (m *Maps) trackDir(track string) (string, error) {
	if !safeTrack.MatchString(track) {
		return "", errors.New("bad track name")
	}
	return filepath.Join(m.Dir, track), nil
}

func (m *Maps) get(url string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := m.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.Header.Get("Content-Type"), err
}

// ---- terrain ----

// Terrain is a grid of ground heights in metres above sea level, every Step metres from E0, N0
// (south-west corner), row by row from south to north.
type Terrain struct {
	E0   float64          `json:"e0"`
	N0   float64          `json:"n0"`
	Step float64          `json:"step"`
	Cols int              `json:"cols"`
	Rows int              `json:"rows"`
	H    telemetry.Floats `json:"h"`
}

// At is the height at a point (bilinear; the nearest edge outside the grid).
func (t *Terrain) At(e, n float64) float64 {
	x := math.Max(0, math.Min(float64(t.Cols-1)-1e-9, (e-t.E0)/t.Step))
	y := math.Max(0, math.Min(float64(t.Rows-1)-1e-9, (n-t.N0)/t.Step))
	i, j := int(x), int(y)
	fx, fy := x-float64(i), y-float64(j)
	h := func(a, b int) float64 { return float64(t.H[b*t.Cols+a]) }
	return (h(i, j)*(1-fx)+h(i+1, j)*fx)*(1-fy) + (h(i, j+1)*(1-fx)+h(i+1, j+1)*fx)*fy
}

// terrainZoom: open terrain tiles at zoom 14 (about 7 m a pixel; the data itself is 30 m or finer).
const (
	terrainZoom = 14
	terrainURL  = "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/%d/%d/%d.png"
	terrainStep = 10.0
	// TerrainCredit names the sources of the open terrain tiles.
	TerrainCredit = "Terrain: Mapzen terrain tiles (SRTM, EU-DEM, USGS 3DEP and others)"
)

// Terrain returns the track's ground heights, downloading them once (an error when offline and not
// kept yet).
func (m *Maps) Terrain(track string, g telemetry.GeoTrack) (*Terrain, error) {
	dir, err := m.trackDir(track)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "terrain.bin")
	if t, err := readTerrain(p); err == nil {
		return t, nil
	}
	e0, n0, e1, n1 := g.Bounds(Margin)
	t := &Terrain{E0: e0, N0: n0, Step: terrainStep, Cols: int((e1-e0)/terrainStep) + 1, Rows: int((n1-n0)/terrainStep) + 1}
	tiles := map[[2]int]image.Image{}
	t.H = make(telemetry.Floats, t.Cols*t.Rows)
	for r := 0; r < t.Rows; r++ {
		for c := 0; c < t.Cols; c++ {
			lat, lon := g.LatLon(e0+float64(c)*terrainStep, n0+float64(r)*terrainStep)
			px, py := mercPixel(lat, lon, terrainZoom)
			h, err := m.terrainAt(tiles, px, py)
			if err != nil {
				return nil, err
			}
			t.H[r*t.Cols+c] = float32(h)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		writeTerrain(p, t)
	}
	return t, nil
}

// terrainAt reads the height at a pixel position of the terrain tiles (bilinear).
func (m *Maps) terrainAt(tiles map[[2]int]image.Image, px, py float64) (float64, error) {
	px, py = px-0.5, py-0.5
	x0, y0 := math.Floor(px), math.Floor(py)
	fx, fy := px-x0, py-y0
	var v [4]float64
	for k := 0; k < 4; k++ {
		x, y := int(x0)+k%2, int(y0)+k/2
		tk := [2]int{x >> 8, y >> 8}
		img, ok := tiles[tk]
		if !ok {
			b, _, err := m.get(fmt.Sprintf(terrainURL, terrainZoom, tk[0], tk[1]))
			if err != nil {
				return 0, err
			}
			img, err = png.Decode(bytes.NewReader(b))
			if err != nil {
				return 0, err
			}
			tiles[tk] = img
		}
		r, g, bl, _ := img.At(img.Bounds().Min.X+x&255, img.Bounds().Min.Y+y&255).RGBA()
		v[k] = float64(r>>8)*256 + float64(g>>8) + float64(bl>>8)/256 - 32768 // Terrarium encoding
	}
	return (v[0]*(1-fx)+v[1]*fx)*(1-fy) + (v[2]*(1-fx)+v[3]*fx)*fy, nil
}

func readTerrain(p string) (*Terrain, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var t Terrain
	if len(b) < 4 {
		return nil, errors.New("short terrain file")
	}
	hl := int(binary.LittleEndian.Uint32(b))
	if 4+hl > len(b) || json.Unmarshal(b[4:4+hl], &t) != nil {
		return nil, errors.New("bad terrain file")
	}
	rest := b[4+hl:]
	if t.Cols <= 1 || t.Rows <= 1 || len(rest) != 4*t.Cols*t.Rows {
		return nil, errors.New("bad terrain file")
	}
	t.H = make(telemetry.Floats, t.Cols*t.Rows)
	for i := range t.H {
		t.H[i] = math.Float32frombits(binary.LittleEndian.Uint32(rest[4*i:]))
	}
	return &t, nil
}

func writeTerrain(p string, t *Terrain) error {
	h := *t
	h.H = nil
	hj, _ := json.Marshal(h)
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(hj)))
	b = append(b, hj...)
	for _, v := range t.H {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// mercPixel is the web-map pixel position (256 px tiles) of a point at zoom z.
func mercPixel(lat, lon float64, z int) (x, y float64) {
	n := math.Exp2(float64(z)) * 256
	x = (lon + 180) / 360 * n
	s := math.Sin(lat * math.Pi / 180)
	y = (0.5 - math.Log((1+s)/(1-s))/(4*math.Pi)) * n
	return
}
