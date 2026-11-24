package telemetry

import (
	"math"
	"testing"
)

// The fit must find a track that was turned, mirrored and shifted back onto its OSM lines.
func TestFitToGeoRecoversPlacement(t *testing.T) {
	g, ok := Geo("zolder")
	if !ok {
		t.Fatal("no shipped data for zolder")
	}
	// the longest OSM line stands in for a driven lap
	var lap []float32
	for _, w := range g.Ways {
		if len(w) > len(lap) {
			lap = w
		}
	}
	if len(lap) < 60 {
		t.Fatalf("longest line has only %d points", len(lap)/2)
	}
	// resample to every 2 m, as a lap's positions would be
	var es, ns []float64
	for i := 0; i+3 < len(lap); i += 2 {
		x1, y1, x2, y2 := float64(lap[i]), float64(lap[i+1]), float64(lap[i+2]), float64(lap[i+3])
		l := math.Hypot(x2-x1, y2-y1)
		for d := 0.0; d < l; d += 2 {
			es, ns = append(es, x1+(x2-x1)*d/l), append(ns, y1+(y2-y1)*d/l)
		}
	}
	// "game" coordinates: the inverse of a known placement
	want := GeoFit{Mirror: true, Angle: 0.65, Scale: 1, E: 120, N: -80}
	xs, zs := make([]float64, len(es)), make([]float64, len(es))
	c, s := math.Cos(want.Angle), math.Sin(want.Angle)
	for i := range es {
		de, dn := es[i]-want.E, ns[i]-want.N
		x, z := c*de+s*dn, -s*de+c*dn // turn back
		xs[i], zs[i] = x, -z          // mirror back
	}
	f, ok := FitToGeo(g, xs, zs, false)
	if !ok || f.RMS > 0.5 {
		t.Fatalf("fit failed: ok %v, %+v", ok, f)
	}
	for _, i := range []int{0, len(xs) / 3, len(xs) / 2} {
		e, n := f.Apply(xs[i], zs[i])
		if d := math.Hypot(e-es[i], n-ns[i]); d > 0.5 {
			t.Errorf("point %d placed %.2f m off", i, d)
		}
	}
	if f.Mirror != want.Mirror {
		t.Errorf("mirror %v, want %v", f.Mirror, want.Mirror)
	}
}

func TestGeoLatLonRoundTrip(t *testing.T) {
	g, _ := Geo("spa")
	lat, lon := g.LatLon(1234.5, -987.6)
	e, n := g.EN(lat, lon)
	if math.Abs(e-1234.5) > 0.01 || math.Abs(n+987.6) > 0.01 {
		t.Errorf("round trip gave %.3f, %.3f", e, n)
	}
}

// Every track SetupMan names should have real-world data (Mount Panorama runs on public roads
// without race track lines in OpenStreetMap; it waits for another source).
func TestGeoTracksPresent(t *testing.T) {
	for _, tr := range []string{"barcelona", "brands_hatch", "cota", "donington", "hungaroring", "imola", "indianapolis", "kyalami",
		"laguna_seca", "misano", "monza", "nurburgring", "nurburgring_24h", "oulton_park", "paul_ricard", "red_bull_ring",
		"silverstone", "snetterton", "spa", "suzuka", "valencia", "watkins_glen", "zandvoort", "zolder"} {
		g, ok := Geo(tr)
		if !ok || len(g.Ways) == 0 {
			t.Errorf("%s: no OpenStreetMap lines", tr)
		}
	}
}
