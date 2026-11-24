package maps3d

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"setupman/internal/telemetry"
)

// Source is a government aerial photo service whose licence allows keeping the photos (Q-TL20).
// Each was checked on 2026-11-24: tiles at the circuit, licence on the provider's metadata.
type Source struct {
	ID      string `json:"id"`
	Credit  string `json:"credit"` // shown on the view, as the licence asks
	Licence string `json:"licence"`
	Zoom    int    `json:"zoom"` // web-map zoom of the photos (18 ≈ 0.4–0.6 m a pixel at these tracks)
	kind    string // "xyz" (tiles), "tms" (tiles counted from the south) or "wms" (an area per request)
	url     string // tiles: {z} {x} {y}; wms: the service address
	layer   string // wms only
}

var sources = map[string]Source{
	"nl": {ID: "nl", Credit: "Aerial photo: Beeldmateriaal Nederland / PDOK", Licence: "CC BY 4.0", Zoom: 18, kind: "xyz",
		url: "https://service.pdok.nl/hwh/luchtfotorgb/wmts/v1_0/Actueel_orthoHR/EPSG:3857/{z}/{x}/{y}.jpeg"},
	"be-vl": {ID: "be-vl", Credit: "Aerial photo: Digitaal Vlaanderen", Licence: "Modellicentie Gratis Hergebruik", Zoom: 18, kind: "wms",
		url: "https://geo.api.vlaanderen.be/omw/wms", layer: "omwrgb25vl"},
	"be-wa": {ID: "be-wa", Credit: "Aerial photo 2021: SPW – Service public de Wallonie", Licence: "CC BY 4.0", Zoom: 18, kind: "wms",
		url: "https://geoservices.wallonie.be/geoserver/inspire_oi/ows", layer: "OI.OrthoimageCoverage.2021"},
	"de-rp": {ID: "de-rp", Credit: "Aerial photo: ©GeoBasis-DE / LVermGeoRP", Licence: "dl-de/by-2-0", Zoom: 18, kind: "wms",
		url: "https://geo4.service24.rlp.de/wms/rp_dop20.fcgi", layer: "rp_dop20"},
	"at": {ID: "at", Credit: "Aerial photo: basemap.at", Licence: "CC BY 4.0", Zoom: 18, kind: "xyz",
		url: "https://mapsneu.wien.gv.at/basemap/bmaporthofoto30cm/normal/google3857/{z}/{y}/{x}.jpeg"},
	"fr": {ID: "fr", Credit: "Aerial photo: IGN – Géoplateforme", Licence: "Licence Ouverte Etalab 2.0", Zoom: 18, kind: "xyz",
		url: "https://data.geopf.fr/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0&LAYER=ORTHOIMAGERY.ORTHOPHOTOS&STYLE=normal&TILEMATRIXSET=PM&FORMAT=image/jpeg&TILEMATRIX={z}&TILEROW={y}&TILECOL={x}"},
	"es": {ID: "es", Credit: "Aerial photo: PNOA cedido por © Instituto Geográfico Nacional", Licence: "CC BY 4.0", Zoom: 18, kind: "tms",
		url: "https://tms-pnoa-ma.idee.es/1.0.0/pnoa-ma/{z}/{x}/{y}.jpeg"},
	"jp": {ID: "jp", Credit: "Aerial photo: GSI Japan (地理院タイル)", Licence: "GSI terms of use (credit required)", Zoom: 18, kind: "xyz",
		url: "https://cyberjapandata.gsi.go.jp/xyz/seamlessphoto/{z}/{x}/{y}.jpg"},
	"us": {ID: "us", Credit: "Aerial photo: USGS The National Map", Licence: "Public domain", Zoom: 16, kind: "xyz",
		url: "https://basemap.nationalmap.gov/arcgis/rest/services/USGSImageryOnly/MapServer/tile/{z}/{y}/{x}"},
}

var trackSource = map[string]string{
	"zandvoort": "nl", "zolder": "be-vl", "spa": "be-wa", "nurburgring": "de-rp", "nurburgring_24h": "de-rp",
	"red_bull_ring": "at", "paul_ricard": "fr", "barcelona": "es", "valencia": "es", "suzuka": "jp",
	"cota": "us", "laguna_seca": "us", "watkins_glen": "us", "indianapolis": "us",
}

// SourceFor returns the offline photo source of a track (ok false: none with an open licence).
func SourceFor(track string) (Source, bool) {
	s, ok := sources[trackSource[track]]
	return s, ok
}

// photoBand: photos are kept for this far around the track's lines.
const photoBand = 300.0

// Tile is one web-map tile.
type Tile struct{ Z, X, Y int }

// tilesFor lists the photo tiles a track needs.
func tilesFor(g telemetry.GeoTrack, z int) []Tile {
	e0, n0, e1, n1 := g.Bounds(Margin)
	la0, lo0 := g.LatLon(e0, n0)
	la1, lo1 := g.LatLon(e1, n1)
	x0, y1 := mercPixel(la0, lo0, z)
	x1, y0 := mercPixel(la1, lo1, z)
	var out []Tile
	for ty := int(y0 / 256); ty <= int(y1/256); ty++ {
		for tx := int(x0 / 256); tx <= int(x1/256); tx++ {
			lat, lon := tileLatLon(z, float64(tx)+0.5, float64(ty)+0.5)
			e, n := g.EN(lat, lon)
			half := 156543.03 * math.Cos(lat*math.Pi/180) / math.Exp2(float64(z)) * 128 * math.Sqrt2
			if g.Near(e, n, photoBand+half) {
				out = append(out, Tile{z, tx, ty})
			}
		}
	}
	return out
}

// tileLatLon is the latitude and longitude of a position in tile units.
func tileLatLon(z int, tx, ty float64) (lat, lon float64) {
	n := math.Exp2(float64(z))
	lon = tx/n*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*ty/n))) * 180 / math.Pi
	return
}

func (s Source) tileURL(t Tile) string {
	switch s.kind {
	case "wms":
		const r = 20037508.342789244
		size := 2 * r / math.Exp2(float64(t.Z))
		x0, y1 := -r+float64(t.X)*size, r-float64(t.Y)*size
		return fmt.Sprintf("%s?SERVICE=WMS&REQUEST=GetMap&VERSION=1.3.0&LAYERS=%s&STYLES=&CRS=EPSG:3857&BBOX=%.3f,%.3f,%.3f,%.3f&WIDTH=256&HEIGHT=256&FORMAT=image/jpeg",
			s.url, s.layer, x0, y1-size, x0+size, y1)
	case "tms":
		t.Y = (1 << t.Z) - 1 - t.Y
	}
	u := strings.ReplaceAll(s.url, "{z}", fmt.Sprint(t.Z))
	u = strings.ReplaceAll(u, "{x}", fmt.Sprint(t.X))
	return strings.ReplaceAll(u, "{y}", fmt.Sprint(t.Y))
}

// Photos is what is kept of a track's offline photos.
type Photos struct {
	Source   *Source `json:"source"`             // nil: no offline source for this track
	Tiles    int     `json:"tiles"`              // tiles the track needs
	Have     int     `json:"have"`               // tiles kept
	Bytes    int64   `json:"bytes"`              // size kept
	Estimate int64   `json:"estimate,omitempty"` // expected size of the whole set
	Busy     bool    `json:"busy"`               // downloading now
	Error    string  `json:"error,omitempty"`    // the last download's problem
	When     string  `json:"when,omitempty"`     // when the set was completed
}

// Job is a running photo download.
type Job struct {
	done, total int
	err         string
}

type manifest struct {
	Source string `json:"source"`
	Tiles  int    `json:"tiles"`
	Bytes  int64  `json:"bytes"`
	When   string `json:"when"`
}

// PhotoStatus tells what is kept of a track's offline photos.
func (m *Maps) PhotoStatus(track string, g telemetry.GeoTrack) Photos {
	s, ok := SourceFor(track)
	if !ok {
		return Photos{}
	}
	p := Photos{Source: &s, Tiles: len(tilesFor(g, s.Zoom))}
	p.Estimate = int64(p.Tiles) * 22 << 10
	dir, _ := m.trackDir(track)
	var mf manifest
	if b, err := os.ReadFile(filepath.Join(dir, "photos.json")); err == nil && json.Unmarshal(b, &mf) == nil && mf.Source == s.ID {
		p.Have, p.Bytes, p.When = mf.Tiles, mf.Bytes, mf.When
	}
	m.mu.Lock()
	if j := m.jobs[track]; j != nil {
		p.Busy = j.total > 0 && j.done < j.total && j.err == ""
		if p.Busy {
			p.Have = j.done
		}
		p.Error = j.err
	}
	m.mu.Unlock()
	return p
}

// DownloadPhotos starts downloading a track's offline photos in the background (nothing when one is
// already running). Tiles already kept are not fetched again.
func (m *Maps) DownloadPhotos(track string, g telemetry.GeoTrack) error {
	s, ok := SourceFor(track)
	if !ok {
		return errors.New("no offline aerial photos for this track")
	}
	dir, err := m.trackDir(track)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if j := m.jobs[track]; j != nil && j.done < j.total && j.err == "" {
		m.mu.Unlock()
		return nil
	}
	tiles := tilesFor(g, s.Zoom)
	job := &Job{total: len(tiles)}
	m.jobs[track] = job
	m.mu.Unlock()
	go func() {
		pdir := filepath.Join(dir, "photos", s.ID)
		var mu sync.Mutex
		var size int64
		var firstErr error
		ch := make(chan Tile)
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ { // four at a time: polite to public services
			wg.Add(1)
			go func() {
				defer wg.Done()
				for t := range ch {
					n, err := m.fetchTile(s, pdir, t)
					mu.Lock()
					size += n
					if err != nil && firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					m.mu.Lock()
					job.done++
					m.mu.Unlock()
				}
			}()
		}
		for _, t := range tiles {
			mu.Lock()
			stop := firstErr != nil
			mu.Unlock()
			if stop {
				break
			}
			ch <- t
		}
		close(ch)
		wg.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if firstErr != nil {
			job.err = "Download stopped: " + firstErr.Error()
			return
		}
		job.done = job.total
		mf := manifest{Source: s.ID, Tiles: len(tiles), Bytes: size, When: time.Now().Format(time.RFC3339)}
		b, _ := json.Marshal(mf)
		os.WriteFile(filepath.Join(dir, "photos.json"), b, 0o644)
	}()
	return nil
}

// fetchTile downloads one tile unless it is kept already; it returns the tile's size.
func (m *Maps) fetchTile(s Source, dir string, t Tile) (int64, error) {
	p := filepath.Join(dir, fmt.Sprint(t.Z), fmt.Sprint(t.X), fmt.Sprintf("%d.jpg", t.Y))
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		return st.Size(), nil
	}
	var b []byte
	var ct string
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if b, ct, err = m.get(s.tileURL(t)); err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(ct, "image/") {
		return 0, fmt.Errorf("the photo service answered with %q instead of a photo", ct)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	return int64(len(b)), os.WriteFile(p, b, 0o644)
}

// PhotoTile is the file of one kept tile ("" when not kept).
func (m *Maps) PhotoTile(track string, t Tile) string {
	s, ok := SourceFor(track)
	dir, err := m.trackDir(track)
	if !ok || err != nil || t.Z != s.Zoom || t.X < 0 || t.Y < 0 {
		return ""
	}
	p := filepath.Join(dir, "photos", s.ID, fmt.Sprint(t.Z), fmt.Sprint(t.X), fmt.Sprintf("%d.jpg", t.Y))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// DeletePhotos removes a track's kept photos (the terrain stays: it is small).
func (m *Maps) DeletePhotos(track string) error {
	dir, err := m.trackDir(track)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if j := m.jobs[track]; j != nil && j.done < j.total && j.err == "" {
		m.mu.Unlock()
		return errors.New("a download is running")
	}
	delete(m.jobs, track)
	m.mu.Unlock()
	os.Remove(filepath.Join(dir, "photos.json"))
	return os.RemoveAll(filepath.Join(dir, "photos"))
}

// Kept lists the tracks with kept offline photos and their size (for Settings).
func (m *Maps) Kept() map[string]int64 {
	out := map[string]int64{}
	ds, _ := os.ReadDir(m.Dir)
	for _, d := range ds {
		var mf manifest
		if b, err := os.ReadFile(filepath.Join(m.Dir, d.Name(), "photos.json")); err == nil && json.Unmarshal(b, &mf) == nil {
			out[d.Name()] = mf.Bytes
		}
	}
	return out
}
