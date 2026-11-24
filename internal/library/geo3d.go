package library

// 3D telemetry (spec D-190): a track's scene (where the game's track lies on the real world, the
// terrain, the track's typical line) and each lap's path in it.

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"sort"
	"strings"
	"sync"

	"setupman/internal/history"
	"setupman/internal/maps3d"
	"setupman/internal/store"
	"setupman/internal/telemetry"
)

// ErrNo3D: SetupMan has no real-world data for this track.
var ErrNo3D = errors.New("no 3D map data for this track yet")

const scene3DVersion = 2

// Path3D is a line in the scene: metres east, north and up, with the distance along the lap.
type Path3D struct {
	E    telemetry.Floats `json:"e"`
	N    telemetry.Floats `json:"n"`
	H    telemetry.Floats `json:"h"`
	Dist telemetry.Floats `json:"dist"`
}

// Scene3D is what the 3D view needs of a track.
type Scene3D struct {
	Track     string          `json:"track"`
	Lat       float64         `json:"lat"` // origin of E, N
	Lon       float64         `json:"lon"`
	Source    string          `json:"source"` // how the track was placed: "position" (the game's car position) or "calculated"
	FitRMS    float64         `json:"fitRms"` // metres
	Line      Path3D          `json:"line"`   // the track's typical line (a lap of the fit)
	Terrain   *maps3d.Terrain `json:"terrain,omitempty"`
	NoTerrain string          `json:"noTerrain,omitempty"` // why there is no terrain (offline)
	Photos    maps3d.Photos   `json:"photos"`
	Credits   []string        `json:"credits"`

	fit     telemetry.GeoFit
	hOffset float64 // game height + hOffset = metres above sea level (position source)
}

// kept3D is the fit stored in the library (KV), so it is worked out once per track.
type kept3D struct {
	V      int              `json:"v"`
	Source string           `json:"source"`
	Fit    telemetry.GeoFit `json:"fit"`
	FileID int64            `json:"fileId"`
	Lap    int              `json:"lap"`
	// the lap's line: E, N (fitted), the game's height (position source) and the distance
	E, N, Y, Dist []float32
}

type scene3DCache struct {
	mu      sync.Mutex
	scenes  map[string]*Scene3D
	checked map[string]bool // a calculated fit: looked for an extra-log lap this run
	busy    sync.Mutex      // one fit at a time (a fit takes a few seconds)
}

// Track3D returns the scene of a track, fitting the track and downloading the terrain the first time.
func (l *Library) Track3D(gameID, track string) (*Scene3D, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	track = strings.ToLower(track)
	g, ok := telemetry.Geo(track)
	if !ok {
		return nil, ErrNo3D
	}
	c := &l.scene3D
	key := gameID + "/" + track
	c.mu.Lock()
	if c.scenes == nil {
		c.scenes, c.checked = map[string]*Scene3D{}, map[string]bool{}
	}
	s := c.scenes[key]
	c.mu.Unlock()
	if s == nil || (s.Terrain == nil && s.NoTerrain != "") {
		var err error
		if s, err = l.buildScene3D(gameID, track, g); err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.scenes[key] = s
		c.mu.Unlock()
	}
	out := *s
	if l.Maps != nil {
		out.Photos = l.Maps.PhotoStatus(track, g) // the view adds the photo's credit while it shows the photo
	}
	return &out, nil
}

func (l *Library) buildScene3D(gameID, track string, g telemetry.GeoTrack) (*Scene3D, error) {
	c := &l.scene3D
	c.busy.Lock()
	defer c.busy.Unlock()
	kv := "tele-3d/" + gameID + "/" + track
	var k kept3D
	if v, _ := l.db.GetKV(kv); v != "" && json.Unmarshal([]byte(v), &k) == nil && k.V == scene3DVersion {
		c.mu.Lock()
		look := k.Source != "position" && !c.checked[kv]
		c.checked[kv] = true
		c.mu.Unlock()
		if look {
			if nk, ok := l.fitTrack(gameID, track, g, true); ok {
				k = nk
				l.saveKept3D(kv, k)
			}
		}
	} else {
		var ok bool
		if k, ok = l.fitTrack(gameID, track, g, false); !ok {
			return nil, errors.New("SetupMan could not place this track on the map: no lap with a usable line")
		}
		l.saveKept3D(kv, k)
	}
	s := &Scene3D{Track: track, Lat: g.Lat, Lon: g.Lon, Source: k.Source, FitRMS: math.Round(k.Fit.RMS*10) / 10, fit: k.Fit,
		Credits: []string{"Track lines © OpenStreetMap contributors (ODbL)"}}
	if l.Maps != nil {
		t, err := l.Maps.Terrain(track, g)
		if err != nil {
			log.Printf("3D terrain %s: %v", track, err)
			s.NoTerrain = "The terrain could not be downloaded (no internet connection?). It is kept once downloaded."
		} else {
			s.Terrain = t
			s.Credits = append(s.Credits, maps3d.TerrainCredit)
		}
	}
	// heights of the line
	s.Line = Path3D{E: k.E, N: k.N, Dist: k.Dist, H: make(telemetry.Floats, len(k.E))}
	if k.Source == "position" {
		var d []float64
		for i := range k.E {
			if s.Terrain != nil {
				d = append(d, s.Terrain.At(float64(k.E[i]), float64(k.N[i]))-float64(k.Y[i]))
			} else {
				d = append(d, -float64(k.Y[i]))
			}
		}
		s.hOffset = median(d)
		for i := range k.Y {
			s.Line.H[i] = float32(float64(k.Y[i]) + s.hOffset)
		}
	} else if s.Terrain != nil {
		for i := range k.E {
			s.Line.H[i] = float32(s.Terrain.At(float64(k.E[i]), float64(k.N[i])))
		}
	}
	if s.Terrain != nil && k.Source == "position" {
		t := *s.Terrain
		t.H = flattenAlong(s.Terrain, s.Line)
		s.Terrain = &t
	}
	return s, nil
}

func (l *Library) saveKept3D(kv string, k kept3D) {
	if b, err := json.Marshal(k); err == nil {
		l.db.SetKV(kv, string(b))
	}
}

// fitTrack places the track on the real world: from the game's car position in a lap with the extra
// log when there is one (fastest first), else from the fastest lap's calculated line. onlyPosition:
// only try the car position.
func (l *Library) fitTrack(gameID, track string, g telemetry.GeoTrack, onlyPosition bool) (kept3D, bool) {
	rows, err := l.db.TelemetryTrackLaps(gameID, "", track)
	if err != nil || len(rows) == 0 {
		return kept3D{}, false
	}
	for _, r := range rows[:min(len(rows), 40)] {
		p := l.lapPositions(r.FileID, r.N)
		if p == nil {
			continue
		}
		xs, zs := make([]float64, len(p.x)), make([]float64, len(p.x))
		for i := range p.x {
			xs[i], zs[i] = float64(p.x[i]), float64(p.z[i])
		}
		f, ok := telemetry.FitToGeo(g, xs, zs, false)
		log.Printf("3D: %s placed from the car position of a lap (file %d lap %d): %.1f m, ok %v", track, r.FileID, r.N, f.RMS, ok)
		if !ok {
			continue
		}
		k := kept3D{V: scene3DVersion, Source: "position", Fit: f, FileID: r.FileID, Lap: r.N, Y: p.y, Dist: p.dist}
		for i := range p.x {
			e, n := f.Apply(xs[i], zs[i])
			k.E, k.N = append(k.E, float32(e)), append(k.N, float32(n))
		}
		return k, true
	}
	if onlyPosition {
		return kept3D{}, false
	}
	// the calculated line: the size is right (speed integrated), so only turned and shifted; a lap
	// whose line doesn't cover the whole lap (ACC kept no data for part of it) is skipped
	tried := 0
	for _, r := range rows {
		if tried == 5 {
			break
		}
		if r.DistM < 1000 {
			continue
		}
		m := l.lapMap(r.FileID, r.N)
		if m == nil || len(m.X) < 50 || m.Dist[len(m.Dist)-1] < float32(r.DistM*0.95) {
			continue
		}
		tried++
		xs, zs := make([]float64, len(m.X)), make([]float64, len(m.X))
		length := 0.0
		for i := range m.X {
			xs[i], zs[i] = float64(m.X[i]), float64(m.Y[i])
			if i > 0 {
				length += math.Hypot(xs[i]-xs[i-1], zs[i]-zs[i-1])
			}
		}
		if length < r.DistM*0.9 {
			continue
		}
		f, _ := telemetry.FitToGeo(g, xs, zs, false)
		log.Printf("3D: %s placed from the calculated line of a lap (file %d lap %d, %.0f m): %.1f m", track, r.FileID, r.N, length, f.RMS)
		if f.RMS > 15 {
			continue
		}
		k := kept3D{V: scene3DVersion, Source: "calculated", Fit: f, FileID: r.FileID, Lap: r.N, Dist: m.Dist, Y: make([]float32, len(m.X))}
		for i := range m.X {
			e, n := f.Apply(xs[i], zs[i])
			k.E, k.N = append(k.E, float32(e)), append(k.N, float32(n))
		}
		return k, true
	}
	return kept3D{}, false
}

// lapPos is a lap's car positions from the extra log, in the game's coordinates, with the distance
// along the lap (metres, from the MoTeC speed: the same distance the graphs use).
type lapPos struct {
	x, y, z, dist []float32
}

func (l *Library) lapPositions(fileID int64, n int) *lapPos {
	f, err := l.db.TelemetryFile(fileID)
	if err != nil || f == nil {
		return nil
	}
	lap := l.teleLapOf(fileID, n)
	if lap == nil || !lap.Complete {
		return nil
	}
	extra := l.extraLog(f.Game, f.SessionID)
	if extra == nil {
		return nil
	}
	xlap, ok := extra.FindLap(lap.LapMs)
	if !ok {
		return nil
	}
	xi, yi, zi, ti := extra.Index(telemetry.ExtraPosX), extra.Index("POS_Y"), extra.Index(telemetry.ExtraPosZ), extra.Index(telemetry.ExtraLapTime)
	if xi < 0 || yi < 0 || zi < 0 || ti < 0 {
		return nil
	}
	lf, err := l.teleOpen(f)
	if err != nil {
		return nil
	}
	d, err := telemetry.ReadLap(lf, *lap, nil, nil, nil)
	lf.Close()
	if err != nil || len(d.Dist) < 2 {
		return nil
	}
	dist := func(t float64) float32 {
		i := (t - d.DistT0) * float64(d.DistHz)
		j := min(max(int(i), 0), len(d.Dist)-1)
		return d.Dist[j]
	}
	p := &lapPos{}
	for r := xlap.From; r < xlap.To; r++ {
		x, z := extra.Data[xi][r], extra.Data[zi][r]
		if x == 0 && z == 0 {
			continue
		}
		p.x, p.y, p.z = append(p.x, x), append(p.y, extra.Data[yi][r]), append(p.z, z)
		p.dist = append(p.dist, dist(float64(extra.Data[ti][r])/1000))
	}
	if len(p.x) < 50 {
		return nil
	}
	return p
}

func (l *Library) teleLapOf(fileID int64, n int) *store.TeleLap {
	laps, err := l.db.TelemetryLaps(fileID)
	if err != nil {
		return nil
	}
	for i := range laps {
		if laps[i].N == n {
			return &laps[i]
		}
	}
	return nil
}

// LapPath3D is one lap's line in its track's scene. Exact: from the car's recorded position; else
// the track's typical line stretched to the lap's length ("approximate position", Q-TL19).
type LapPath3D struct {
	Path3D
	Exact bool `json:"exact"`
}

// TelemetryLap3D returns a lap's path in its track's scene.
func (l *Library) TelemetryLap3D(fileID int64, n int) (*LapPath3D, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	f, err := l.db.TelemetryFile(fileID)
	if err != nil {
		return nil, err
	}
	lap := l.teleLapOf(fileID, n)
	if f == nil || lap == nil {
		return nil, history.ErrNotFound
	}
	s, err := l.Track3D(f.Game, f.Track)
	if err != nil {
		return nil, err
	}
	if s.Source == "position" {
		if p := l.lapPositions(fileID, n); p != nil {
			out := &LapPath3D{Exact: true, Path3D: Path3D{Dist: p.dist}}
			for i := range p.x {
				e, nn := s.fit.Apply(float64(p.x[i]), float64(p.z[i]))
				out.E, out.N = append(out.E, float32(e)), append(out.N, float32(nn))
				out.H = append(out.H, float32(float64(p.y[i])+s.hOffset))
			}
			return out, nil
		}
	}
	out := &LapPath3D{Path3D: Path3D{E: s.Line.E, N: s.Line.N, H: s.Line.H, Dist: make(telemetry.Floats, len(s.Line.Dist))}}
	k := 1.0
	if total := s.Line.Dist[len(s.Line.Dist)-1]; total > 0 && lap.DistM > 100 {
		k = lap.DistM / float64(total)
	}
	for i, d := range s.Line.Dist {
		out.Dist[i] = float32(float64(d) * k)
	}
	return out, nil
}

// flattenAlong pulls the terrain to the track's height near its line (the open terrain data is
// coarse; the game's track height is exact): fully within 12 m, fading out by 40 m.
func flattenAlong(t *maps3d.Terrain, line Path3D) telemetry.Floats {
	out := append(telemetry.Floats{}, t.H...)
	// line points by cell for a quick nearest search
	const cell = 20.0
	grid := map[[2]int][]int{}
	for i := range line.E {
		k := [2]int{int(math.Floor(float64(line.E[i]) / cell)), int(math.Floor(float64(line.N[i]) / cell))}
		grid[k] = append(grid[k], i)
	}
	for r := 0; r < t.Rows; r++ {
		for c := 0; c < t.Cols; c++ {
			e, n := t.E0+float64(c)*t.Step, t.N0+float64(r)*t.Step
			k := [2]int{int(math.Floor(e / cell)), int(math.Floor(n / cell))}
			best, bh := 1e9, 0.0
			for a := k[0] - 2; a <= k[0]+2; a++ {
				for b := k[1] - 2; b <= k[1]+2; b++ {
					for _, i := range grid[[2]int{a, b}] {
						if d := math.Hypot(float64(line.E[i])-e, float64(line.N[i])-n); d < best {
							best, bh = d, float64(line.H[i])
						}
					}
				}
			}
			if best >= 40 {
				continue
			}
			w := math.Min(1, (40-best)/28)
			h := float64(out[r*t.Cols+c])
			out[r*t.Cols+c] = float32(h + (bh-0.4-h)*w) // 0.4 m below the line: the line is drawn on top
		}
	}
	return out
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64{}, v...)
	sort.Float64s(s)
	return s[len(s)/2]
}
