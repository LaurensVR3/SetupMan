// Package telemetry handles ACC lap telemetry: live high-rate recording,
// MoTeC (.ld / .ldx) file parsing, distance-aligned lap comparison,
// track section breakdowns, and sample Nürburgring 24h laps.
package telemetry

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"time"
)

// FlagSlice stores per-bin binary flags (0/1) as a JSON number array,
// while still decoding legacy base64-encoded uint8 slices without error.
type FlagSlice []int8

// MarshalJSON encodes FlagSlice as a JSON array of numbers ([0,1,0,...]).
func (f FlagSlice) MarshalJSON() ([]byte, error) {
	if f == nil {
		return []byte("null"), nil
	}
	return json.Marshal([]int8(f))
}

// UnmarshalJSON accepts either a JSON array of numbers or a legacy base64 string.
func (f *FlagSlice) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = nil
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return err
		}
		out := make(FlagSlice, len(raw))
		for i, b := range raw {
			out[i] = int8(b)
		}
		*f = out
		return nil
	}
	var arr []int8
	if err := json.Unmarshal(trimmed, &arr); err != nil {
		return err
	}
	*f = FlagSlice(arr)
	return nil
}

// Source constants describe where a stored telemetry lap came from.
const (
	SourceLive   = "live"
	SourceMoTeC  = "motec"
	SourceSample = "sample"
)

// RawSample is one time-domain telemetry reading before distance resampling.
type RawSample struct {
	TimeS    float64
	DistM    float64 // cumulative distance along the lap, metres (optional; integrated from SpeedKmh if 0)
	NormDist float64 // 0..1 when known from game shared memory (optional)
	SpeedKmh float64
	Throttle float64 // 0..100 %
	Brake    float64 // 0..100 %
	SteerDeg float64 // steering angle in degrees (- = left, + = right)
	Gear     int     // 0 = N, 1..6 = forward gears
	RPM      int
	TC       bool
	ABS      bool
	GLat     float64 // lateral acceleration in G
	GLon     float64 // longitudinal acceleration in G
	X        float64 // 2D track X coordinate in metres
	Y        float64 // 2D track Y coordinate in metres
	HasXY    bool
}

// Trace is one lap resampled onto N uniform normalized-distance bins (0.0 .. 1.0).
// Columnar slices compress well with gzip (~20-35 KB even for Nürburgring 24h).
type Trace struct {
	N            int       `json:"n"`
	TrackLengthM float64   `json:"trackLengthM"`
	HasCoords    bool      `json:"hasCoords"`
	DistM        []float32 `json:"distM"`
	TimeS        []float32 `json:"timeS"`
	SpeedKmh     []float32 `json:"speedKmh"`
	Throttle     []float32 `json:"throttle"`
	Brake        []float32 `json:"brake"`
	SteerDeg     []float32 `json:"steerDeg"`
	Gear         []int8    `json:"gear"`
	RPM          []int32   `json:"rpm"`
	TC           FlagSlice `json:"tc"`
	ABS          FlagSlice `json:"abs"`
	GLat         []float32 `json:"gLat,omitempty"`
	GLon         []float32 `json:"gLon,omitempty"`
	X            []float32 `json:"x,omitempty"`
	Y            []float32 `json:"y,omitempty"`
}

// Stats summarises one telemetry lap for KPI cards and lap pickers.
type Stats struct {
	MaxSpeedKmh     float64 `json:"maxSpeedKmh"`
	MinSpeedKmh     float64 `json:"minSpeedKmh"`
	AvgSpeedKmh     float64 `json:"avgSpeedKmh"`
	FullThrottlePct float64 `json:"fullThrottlePct"`
	BrakePct        float64 `json:"brakePct"`
	CoastPct        float64 `json:"coastPct"`
	TCActivePct     float64 `json:"tcActivePct"`
	ABSActivePct    float64 `json:"absActivePct"`
	MaxRPM          int     `json:"maxRpm"`
	PeakGLat        float64 `json:"peakGLat"`
	PeakGLon        float64 `json:"peakGLon"`
}

// LapMeta is the metadata for one stored telemetry lap.
type LapMeta struct {
	ID           int64     `json:"id"`
	Game         string    `json:"game"`
	Car          string    `json:"car"`
	Track        string    `json:"track"`
	CarName      string    `json:"carName,omitempty"`
	TrackName    string    `json:"trackName,omitempty"`
	Source       string    `json:"source"`
	SourceFile   string    `json:"sourceFile,omitempty"`
	Label        string    `json:"label,omitempty"`
	LapMs        int       `json:"lapMs"`
	LapNumber    int       `json:"lapNumber"`
	Valid        bool      `json:"valid"`
	IsReference  bool      `json:"isReference"`
	TrackLengthM float64   `json:"trackLengthM"`
	SessionID    int64     `json:"sessionId,omitempty"`
	SetupID      int64     `json:"setupId,omitempty"`
	SetupName    string    `json:"setupName,omitempty"`
	RecordedAt   time.Time `json:"recordedAt"`
	HasCoords    bool      `json:"hasCoords"`
	Stats        Stats     `json:"stats"`
}

// BinsForTrackLength picks a clean uniform distance resolution:
// ~600 bins for standard 4-7 km circuits, 1200 bins for Nürburgring 24h (~25 km).
func BinsForTrackLength(lengthM float64) int {
	if lengthM >= 15000 {
		return 1200
	}
	if lengthM >= 9000 {
		return 900
	}
	return 600
}

// Resample turns raw time-ordered lap samples into a uniform distance-grid Trace and Stats.
// If lapMs > 0, the final TimeS is scaled gently so TimeS[N-1] matches lapMs/1000.
func Resample(raw []RawSample, lapMs int, bins int) (*Trace, Stats) {
	if len(raw) < 2 {
		return nil, Stats{}
	}
	if bins < 100 {
		bins = 600
	}

	// Copy and ensure monotonic TimeS and DistM.
	pts := make([]RawSample, 0, len(raw))
	t0 := raw[0].TimeS
	var cumDist float64
	hasDist := raw[len(raw)-1].DistM > raw[0].DistM+100
	hasXY := 0

	for i, r := range raw {
		s := r
		s.TimeS = r.TimeS - t0
		if s.TimeS < 0 {
			s.TimeS = 0
		}
		if i > 0 {
			prev := pts[len(pts)-1]
			dt := s.TimeS - prev.TimeS
			if dt < 0 {
				continue
			}
			if hasDist {
				if s.DistM < prev.DistM {
					s.DistM = prev.DistM
				}
			} else {
				avgV := (prev.SpeedKmh + s.SpeedKmh) / 2.0 / 3.6
				if avgV < 0 {
					avgV = 0
				}
				cumDist += avgV * dt
				s.DistM = cumDist
			}
		} else if !hasDist {
			s.DistM = 0
		} else {
			s.DistM = r.DistM - raw[0].DistM
		}
		if s.HasXY {
			hasXY++
		}
		pts = append(pts, s)
	}
	if len(pts) < 2 {
		return nil, Stats{}
	}

	totalDist := pts[len(pts)-1].DistM
	if totalDist < 200 {
		return nil, Stats{}
	}
	totalTime := pts[len(pts)-1].TimeS
	timeScale := 1.0
	if lapMs > 0 && totalTime > 1.0 {
		target := float64(lapMs) / 1000.0
		// Only calibrate gently if within 15% (avoids distorting badly truncated traces).
		if math.Abs(target-totalTime)/target < 0.15 {
			timeScale = target / totalTime
		}
	}

	useCoords := hasXY >= len(pts)/2
	tr := &Trace{
		N:            bins,
		TrackLengthM: totalDist,
		HasCoords:    useCoords,
		DistM:        make([]float32, bins),
		TimeS:        make([]float32, bins),
		SpeedKmh:     make([]float32, bins),
		Throttle:     make([]float32, bins),
		Brake:        make([]float32, bins),
		SteerDeg:     make([]float32, bins),
		Gear:         make([]int8, bins),
		RPM:          make([]int32, bins),
		TC:           make(FlagSlice, bins),
		ABS:          make(FlagSlice, bins),
		GLat:         make([]float32, bins),
		GLon:         make([]float32, bins),
	}
	if useCoords {
		tr.X = make([]float32, bins)
		tr.Y = make([]float32, bins)
	}

	srcIdx := 0
	for i := 0; i < bins; i++ {
		frac := float64(i) / float64(bins-1)
		targetD := frac * totalDist
		for srcIdx+1 < len(pts)-1 && pts[srcIdx+1].DistM < targetD {
			srcIdx++
		}
		a, b := pts[srcIdx], pts[srcIdx+1]
		alpha := 0.0
		if span := b.DistM - a.DistM; span > 1e-6 {
			alpha = clamp01((targetD - a.DistM) / span)
		}
		lerp := func(va, vb float64) float32 {
			return float32(va + (vb-va)*alpha)
		}
		tr.DistM[i] = float32(targetD)
		tr.TimeS[i] = float32((a.TimeS + (b.TimeS-a.TimeS)*alpha) * timeScale)
		tr.SpeedKmh[i] = lerp(a.SpeedKmh, b.SpeedKmh)
		tr.Throttle[i] = float32(clamp(a.Throttle+(b.Throttle-a.Throttle)*alpha, 0, 100))
		tr.Brake[i] = float32(clamp(a.Brake+(b.Brake-a.Brake)*alpha, 0, 100))
		tr.SteerDeg[i] = lerp(a.SteerDeg, b.SteerDeg)
		tr.GLat[i] = lerp(a.GLat, b.GLat)
		tr.GLon[i] = lerp(a.GLon, b.GLon)
		tr.RPM[i] = int32(math.Round(float64(a.RPM) + float64(b.RPM-a.RPM)*alpha))
		if alpha < 0.5 {
			tr.Gear[i] = int8(a.Gear)
		} else {
			tr.Gear[i] = int8(b.Gear)
		}
		if a.TC || b.TC {
			tr.TC[i] = 1
		}
		if a.ABS || b.ABS {
			tr.ABS[i] = 1
		}
		if useCoords {
			tr.X[i] = lerp(a.X, b.X)
			tr.Y[i] = lerp(a.Y, b.Y)
		}
	}

	return tr, ComputeStats(tr)
}

// ComputeStats calculates time-weighted KPI summary metrics over a distance-resampled Trace
// (matching MoTeC i2's % of lap time rather than % of track distance).
func ComputeStats(tr *Trace) Stats {
	if tr == nil || tr.N == 0 {
		return Stats{}
	}
	st := Stats{MinSpeedKmh: 9999}
	hasTime := len(tr.TimeS) == tr.N && tr.N > 1 && tr.TimeS[tr.N-1] > tr.TimeS[0]
	var totalW, speedSum, fullT, brake, coast, tcW, absW float64
	for i := 0; i < tr.N; i++ {
		w := 1.0
		if hasTime {
			switch {
			case i == 0:
				w = float64(tr.TimeS[1] - tr.TimeS[0])
			case i == tr.N-1:
				w = float64(tr.TimeS[tr.N-1] - tr.TimeS[tr.N-2])
			default:
				w = 0.5 * float64(tr.TimeS[i+1]-tr.TimeS[i-1])
			}
			if w < 0 {
				w = 0
			}
		}
		totalW += w
		v := float64(tr.SpeedKmh[i])
		if v > st.MaxSpeedKmh {
			st.MaxSpeedKmh = v
		}
		if v > 5 && v < st.MinSpeedKmh {
			st.MinSpeedKmh = v
		}
		speedSum += v * w
		if tr.Throttle[i] >= 95 {
			fullT += w
		}
		if tr.Brake[i] >= 5 {
			brake += w
		}
		if tr.Throttle[i] < 5 && tr.Brake[i] < 5 {
			coast += w
		}
		if i < len(tr.TC) && tr.TC[i] > 0 {
			tcW += w
		}
		if i < len(tr.ABS) && tr.ABS[i] > 0 {
			absW += w
		}
		if i < len(tr.RPM) && int(tr.RPM[i]) > st.MaxRPM {
			st.MaxRPM = int(tr.RPM[i])
		}
		if i < len(tr.GLat) {
			if g := math.Abs(float64(tr.GLat[i])); g > st.PeakGLat {
				st.PeakGLat = g
			}
		}
		if i < len(tr.GLon) {
			if g := math.Abs(float64(tr.GLon[i])); g > st.PeakGLon {
				st.PeakGLon = g
			}
		}
	}
	if st.MinSpeedKmh > 9000 {
		st.MinSpeedKmh = 0
	}
	if totalW <= 0 {
		totalW = float64(tr.N)
	}
	st.AvgSpeedKmh = round1(speedSum / totalW)
	st.MaxSpeedKmh = round1(st.MaxSpeedKmh)
	st.MinSpeedKmh = round1(st.MinSpeedKmh)
	st.FullThrottlePct = round1(100 * fullT / totalW)
	st.BrakePct = round1(100 * brake / totalW)
	st.CoastPct = round1(100 * coast / totalW)
	st.TCActivePct = round1(100 * tcW / totalW)
	st.ABSActivePct = round1(100 * absW / totalW)
	st.PeakGLat = math.Round(st.PeakGLat*100) / 100
	st.PeakGLon = math.Round(st.PeakGLon*100) / 100
	return st
}

// EncodeTrace compresses a Trace as gzipped JSON for compact SQLite storage.
func EncodeTrace(tr *Trace) ([]byte, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(gz).Encode(tr); err != nil {
		gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeTrace restores a gzipped JSON Trace from SQLite.
func DecodeTrace(data []byte) (*Trace, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	var tr Trace
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }
func round1(v float64) float64  { return math.Round(v*10) / 10 }
