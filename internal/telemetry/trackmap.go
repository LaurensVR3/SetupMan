package telemetry

import (
	"math"

	"setupman/internal/motec"
	"setupman/internal/store"
)

// TrackMap is the line of one lap, in metres, x to the right and y up, with the distance along
// the lap of every point (to link the map to the graphs).
type TrackMap struct {
	X      Floats  `json:"x"`
	Y      Floats  `json:"y"`
	Dist   Floats  `json:"dist"`
	Gap    float64 `json:"-"`      // calculated maps: how far the end was from the start before closing, per metre of lap
	Source string  `json:"source"` // "position" (the game's car position, D-162) or "calculated"
}

// MapFromMotion calculates the line of a lap from speed and yaw rate (MoTeC has no position,
// D-160). The heading is corrected so a full lap turns exactly once, and the small gap left at the
// end is spread over the lap. Checked on the user's files: clockwise tracks give −1 turn,
// anticlockwise +1, and the distance matches the track length.
func MapFromMotion(lf *motec.File, lap store.TeleLap) (*TrackMap, error) {
	sp, ok1 := lf.Channel("SPEED")
	yw, ok2 := lf.Channel("ROTY")
	if !ok1 || !ok2 {
		return nil, nil
	}
	if _, err := lf.Laps(); err != nil { // sets when each channel starts
		return nil, err
	}
	sp, _ = lf.Channel("SPEED")
	yw, _ = lf.Channel("ROTY")
	startS, lapS := float64(lap.StartMs)/1000, float64(lap.LapMs)/1000
	sf, st := sp.Window(startS, lapS)
	speed, err := lf.Read(sp, sf, st)
	if err != nil {
		return nil, err
	}
	yf, yt := yw.Window(startS, lapS)
	yaw, err := lf.Read(yw, yf, yt)
	if err != nil || len(yaw) < 10 || len(speed) < 10 {
		return nil, err
	}
	dt := 1 / float64(yw.Hz)
	speedAt := func(i int) float64 { // speed at the time of yaw sample i
		j := int(float64(i) * float64(sp.Hz) / float64(yw.Hz))
		return float64(speed[min(j, len(speed)-1)])
	}
	total := 0.0
	for _, v := range yaw {
		total += float64(v) * dt
	}
	turns := total / (2 * math.Pi)
	scale := 1.0
	if lap.Complete && math.Abs(math.Abs(turns)-1) < 0.2 {
		scale = math.Copysign(1, turns) / turns // exactly one turn
	}
	n := len(yaw)
	m := &TrackMap{X: make(Floats, n), Y: make(Floats, n), Dist: make(Floats, n), Source: "calculated"}
	h, x, y, d := 0.0, 0.0, 0.0, 0.0
	xs, ys := make([]float64, n), make([]float64, n)
	for i := 1; i < n; i++ {
		h += float64(yaw[i-1]) * scale * dt
		v := speedAt(i)
		x += v * math.Cos(h) * dt
		y += v * math.Sin(h) * dt
		d += v * dt
		xs[i], ys[i], m.Dist[i] = x, y, float32(d)
	}
	// the first yaw sample lies just before the lap start: 0 m is the lap start, as in LapData.Dist
	if shift := float32((startS - yw.Start - float64(max(yf, 0))*dt) * speedAt(0)); shift != 0 {
		for i := range m.Dist {
			m.Dist[i] -= shift
		}
	}
	ex, ey := 0.0, 0.0
	if lap.Complete {
		ex, ey = xs[n-1], ys[n-1] // a full lap ends where it started
	}
	if d > 0 {
		m.Gap = math.Hypot(xs[n-1], ys[n-1]) / d
	}
	for i := range xs {
		f := float64(i) / float64(n-1)
		m.X[i], m.Y[i] = float32(xs[i]-ex*f), float32(ys[i]-ey*f)
	}
	return m, nil
}

// MapFromPosition takes the line of a lap from the extra log's car positions. dist turns a lap
// time into the distance along the lap (from the MoTeC speed). clockwise says which way the track
// runs (from the yaw rate), so the map is never drawn mirrored whatever the game's axes are.
func MapFromPosition(extra *ExtraLog, xlap ExtraLap, dist func(t float64) float64, clockwise bool) *TrackMap {
	xi, zi, ti := extra.Index(ExtraPosX), extra.Index(ExtraPosZ), extra.Index(ExtraLapTime)
	if xi < 0 || zi < 0 || ti < 0 || xlap.To-xlap.From < 20 {
		return nil
	}
	m := &TrackMap{Source: "position"}
	for r := xlap.From; r < xlap.To; r++ {
		x, z := extra.Data[xi][r], extra.Data[zi][r]
		if x == 0 && z == 0 {
			continue
		}
		m.X = append(m.X, x)
		m.Y = append(m.Y, z)
		m.Dist = append(m.Dist, float32(dist(float64(extra.Data[ti][r])/1000)))
	}
	if len(m.X) < 20 {
		return nil
	}
	area := 0.0 // shoelace: positive = anticlockwise
	for i := range m.X {
		j := (i + 1) % len(m.X)
		area += float64(m.X[i])*float64(m.Y[j]) - float64(m.X[j])*float64(m.Y[i])
	}
	if (area < 0) != clockwise {
		for i := range m.Y {
			m.Y[i] = -m.Y[i]
		}
	}
	return m
}

// Clockwise tells from a calculated map which way the track runs.
func (m *TrackMap) Clockwise() bool {
	area := 0.0
	for i := range m.X {
		j := (i + 1) % len(m.X)
		area += float64(m.X[i])*float64(m.Y[j]) - float64(m.X[j])*float64(m.Y[i])
	}
	return area < 0
}

// Outline is a small drawing of a track for tiles and headers: about 160 points, scaled to fit
// 0 … 1000 with the shape's proportions kept, y down (screen), starting at the line.
type Outline struct {
	X []int16 `json:"x"`
	Y []int16 `json:"y"`
	W int16   `json:"w"` // width and height of the drawing (the longer side is 1000)
	H int16   `json:"h"`
}

// MakeOutline turns a lap's line into an Outline.
func MakeOutline(m *TrackMap, points int) *Outline {
	if m == nil || len(m.X) < 10 {
		return nil
	}
	x0, x1, y0, y1 := float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Inf(1)), float32(math.Inf(-1))
	for i := range m.X {
		x0, x1 = min(x0, m.X[i]), max(x1, m.X[i])
		y0, y1 = min(y0, m.Y[i]), max(y1, m.Y[i])
	}
	span := max(x1-x0, y1-y0)
	if span <= 0 {
		return nil
	}
	k := 1000 / span
	o := &Outline{W: int16((x1 - x0) * k), H: int16((y1 - y0) * k)}
	step := max(1, len(m.X)/points)
	for i := 0; i < len(m.X); i += step {
		o.X = append(o.X, int16((m.X[i]-x0)*k))
		o.Y = append(o.Y, int16((y1-m.Y[i])*k)) // screen y runs down
	}
	return o
}
