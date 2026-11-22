package telemetry

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// SectionDiff summarises how the comparison lap performed vs the reference lap in one track section.
type SectionDiff struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	StartNorm   float64 `json:"startNorm"`
	EndNorm     float64 `json:"endNorm"`
	StartM      float64 `json:"startM"`
	EndM        float64 `json:"endM"`
	RefTimeS    float64 `json:"refTimeS"`
	CmpTimeS    float64 `json:"cmpTimeS"`
	DeltaS      float64 `json:"deltaS"` // positive = slower (lost time), negative = faster (gained time)
	RefMinSpeed float64 `json:"refMinSpeed"`
	CmpMinSpeed float64 `json:"cmpMinSpeed"`
	RefMaxSpeed float64 `json:"refMaxSpeed"`
	CmpMaxSpeed float64 `json:"cmpMaxSpeed"`
	Hint        string  `json:"hint,omitempty"`
}

// Comparison is the full aligned payload returned by GET /api/telemetry/compare.
type Comparison struct {
	Ref          LapMeta       `json:"ref"`
	Cmp          LapMeta       `json:"cmp"`
	TotalDeltaS  float64       `json:"totalDeltaS"` // cmp - ref in seconds
	TrackLengthM float64       `json:"trackLengthM"`
	N            int           `json:"n"`
	DistM        []float32     `json:"distM"`
	NormDist     []float32     `json:"normDist"`
	DeltaS       []float32     `json:"deltaS"`    // cumulative cmp.TimeS - ref.TimeS
	DeltaRate    []float32     `json:"deltaRate"` // local gain/loss rate (seconds per 100m, smoothed)
	SpeedDiff    []float32     `json:"speedDiff"` // cmp - ref km/h
	RefSpeed     []float32     `json:"refSpeed"`
	CmpSpeed     []float32     `json:"cmpSpeed"`
	RefThrottle  []float32     `json:"refThrottle"`
	CmpThrottle  []float32     `json:"cmpThrottle"`
	RefBrake     []float32     `json:"refBrake"`
	CmpBrake     []float32     `json:"cmpBrake"`
	RefSteer     []float32     `json:"refSteer"`
	CmpSteer     []float32     `json:"cmpSteer"`
	RefGear      []int8        `json:"refGear"`
	CmpGear      []int8        `json:"cmpGear"`
	RefRPM       []int32       `json:"refRpm"`
	CmpRPM       []int32       `json:"cmpRpm"`
	RefTC        FlagSlice     `json:"refTc"`
	CmpTC        FlagSlice     `json:"cmpTc"`
	RefABS       FlagSlice     `json:"refAbs"`
	CmpABS       FlagSlice     `json:"cmpAbs"`
	HasMap       bool          `json:"hasMap"`
	MapX         []float32     `json:"mapX,omitempty"`
	MapY         []float32     `json:"mapY,omitempty"`
	Sections     []SectionDiff `json:"sections"`
	TopLosses    []SectionDiff `json:"topLosses"`
	TopGains     []SectionDiff `json:"topGains"`
}

// Compare aligns cmpTrace onto refTrace's normalized distance grid and computes all deltas and insights.
func Compare(refMeta, cmpMeta LapMeta, refTrace, cmpTrace *Trace) Comparison {
	n := refTrace.N
	if n < 2 {
		return Comparison{Ref: refMeta, Cmp: cmpMeta}
	}
	trackLen := refTrace.TrackLengthM
	if trackLen <= 0 {
		trackLen = cmpTrace.TrackLengthM
	}
	if trackLen <= 0 {
		trackLen = TrackLengthHint(refMeta.Track)
	}

	// Recompute time-weighted KPI stats from traces so already-stored laps immediately reflect them.
	refMeta.Stats = ComputeStats(refTrace)
	cmpMeta.Stats = ComputeStats(cmpTrace)

	// Resample cmpTrace onto refTrace's N bins if their lengths differ.
	cmpAligned := alignTrace(cmpTrace, n)

	c := Comparison{
		Ref:          refMeta,
		Cmp:          cmpMeta,
		TotalDeltaS:  math.Round(float64(cmpMeta.LapMs-refMeta.LapMs)) / 1000.0,
		TrackLengthM: math.Round(trackLen),
		N:            n,
		DistM:        make([]float32, n),
		NormDist:     make([]float32, n),
		DeltaS:       make([]float32, n),
		DeltaRate:    make([]float32, n),
		SpeedDiff:    make([]float32, n),
		RefSpeed:     refTrace.SpeedKmh,
		CmpSpeed:     cmpAligned.SpeedKmh,
		RefThrottle:  refTrace.Throttle,
		CmpThrottle:  cmpAligned.Throttle,
		RefBrake:     refTrace.Brake,
		CmpBrake:     cmpAligned.Brake,
		RefSteer:     refTrace.SteerDeg,
		CmpSteer:     cmpAligned.SteerDeg,
		RefGear:      refTrace.Gear,
		CmpGear:      cmpAligned.Gear,
		RefRPM:       refTrace.RPM,
		CmpRPM:       cmpAligned.RPM,
		RefTC:        refTrace.TC,
		CmpTC:        cmpAligned.TC,
		RefABS:       refTrace.ABS,
		CmpABS:       cmpAligned.ABS,
	}

	for i := 0; i < n; i++ {
		frac := float64(i) / float64(n-1)
		c.NormDist[i] = float32(frac)
		c.DistM[i] = float32(frac * trackLen)
		c.DeltaS[i] = cmpAligned.TimeS[i] - refTrace.TimeS[i]
		c.SpeedDiff[i] = cmpAligned.SpeedKmh[i] - refTrace.SpeedKmh[i]
	}

	// Smoothed local delta rate (seconds per 100 m) over a ~6-bin window so the track map
	// cleanly highlights corners where you are actively gaining vs losing time.
	win := 6
	if n < 800 {
		win = 4
	}
	for i := 0; i < n; i++ {
		lo := i - win
		if lo < 0 {
			lo = 0
		}
		hi := i + win
		if hi >= n {
			hi = n - 1
		}
		dd := float64(c.DistM[hi] - c.DistM[lo])
		if dd > 1 {
			rate := float64(c.DeltaS[hi]-c.DeltaS[lo]) / (dd / 100.0)
			c.DeltaRate[i] = float32(rate)
		}
	}

	// Choose the best track map coordinates.
	trackID := refMeta.Track
	if trackID == "" {
		trackID = cmpMeta.Track
	}
	switch {
	case refTrace.HasCoords && len(refTrace.X) == n && len(refTrace.Y) == n:
		c.HasMap, c.MapX, c.MapY = true, refTrace.X, refTrace.Y
	case cmpAligned.HasCoords && len(cmpAligned.X) == n && len(cmpAligned.Y) == n:
		c.HasMap, c.MapX, c.MapY = true, cmpAligned.X, cmpAligned.Y
	default:
		if x, y, _, ok := TrackCenterline(trackID, n); ok {
			c.HasMap, c.MapX, c.MapY = true, x, y
		}
	}

	// Section-by-section breakdown.
	secs := SectionsForTrack(trackID)
	c.Sections = make([]SectionDiff, 0, len(secs))
	for idx, s := range secs {
		i0 := int(math.Round(s.Start * float64(n-1)))
		i1 := int(math.Round(s.End * float64(n-1)))
		if i0 < 0 {
			i0 = 0
		}
		if i1 >= n {
			i1 = n - 1
		}
		if i1 <= i0 {
			continue
		}
		refT := float64(refTrace.TimeS[i1] - refTrace.TimeS[i0])
		cmpT := float64(cmpAligned.TimeS[i1] - cmpAligned.TimeS[i0])
		delta := cmpT - refT

		refMin, cmpMin := 9999.0, 9999.0
		refMax, cmpMax := 0.0, 0.0
		var refBrakeStart, cmpBrakeStart int = -1, -1
		var refFullThrot, cmpFullThrot, cmpTC, cmpABS int
		for i := i0; i <= i1; i++ {
			rv, cv := float64(refTrace.SpeedKmh[i]), float64(cmpAligned.SpeedKmh[i])
			if rv < refMin {
				refMin = rv
			}
			if cv < cmpMin {
				cmpMin = cv
			}
			if rv > refMax {
				refMax = rv
			}
			if cv > cmpMax {
				cmpMax = cv
			}
			if refBrakeStart < 0 && refTrace.Brake[i] >= 25 {
				refBrakeStart = i
			}
			if cmpBrakeStart < 0 && cmpAligned.Brake[i] >= 25 {
				cmpBrakeStart = i
			}
			if refTrace.Throttle[i] >= 95 {
				refFullThrot++
			}
			if cmpAligned.Throttle[i] >= 95 {
				cmpFullThrot++
			}
			if i < len(cmpAligned.TC) && cmpAligned.TC[i] > 0 {
				cmpTC++
			}
			if i < len(cmpAligned.ABS) && cmpAligned.ABS[i] > 0 {
				cmpABS++
			}
		}

		sd := SectionDiff{
			Index:       idx,
			Name:        s.Name,
			StartNorm:   s.Start,
			EndNorm:     s.End,
			StartM:      math.Round(s.Start * trackLen),
			EndM:        math.Round(s.End * trackLen),
			RefTimeS:    round3(refT),
			CmpTimeS:    round3(cmpT),
			DeltaS:      round3(delta),
			RefMinSpeed: round1(refMin),
			CmpMinSpeed: round1(cmpMin),
			RefMaxSpeed: round1(refMax),
			CmpMaxSpeed: round1(cmpMax),
			Hint:        sectionHint(delta, refMin, cmpMin, refMax, cmpMax, refBrakeStart, cmpBrakeStart, refFullThrot, cmpFullThrot, cmpTC, cmpABS, trackLen/float64(n-1)),
		}
		c.Sections = append(c.Sections, sd)
	}

	// Top 3 biggest losses and top 3 biggest gains.
	var losses, gains []SectionDiff
	for _, s := range c.Sections {
		if s.DeltaS > 0.03 {
			losses = append(losses, s)
		} else if s.DeltaS < -0.03 {
			gains = append(gains, s)
		}
	}
	sort.Slice(losses, func(i, j int) bool { return losses[i].DeltaS > losses[j].DeltaS })
	sort.Slice(gains, func(i, j int) bool { return gains[i].DeltaS < gains[j].DeltaS })
	if len(losses) > 3 {
		losses = losses[:3]
	}
	if len(gains) > 3 {
		gains = gains[:3]
	}
	c.TopLosses = losses
	c.TopGains = gains

	return c
}

func sectionHint(delta, refMin, cmpMin, refMax, cmpMax float64, refBrake, cmpBrake, refFT, cmpFT, cmpTC, cmpABS int, mPerBin float64) string {
	var parts []string
	if delta > 0.06 {
		if refBrake >= 0 && cmpBrake >= 0 && cmpBrake < refBrake {
			m := int(math.Round(float64(refBrake-cmpBrake) * mPerBin))
			if m >= 8 {
				parts = append(parts, fmt.Sprintf("Braked ~%d m earlier", m))
			}
		}
		if refMin-cmpMin >= 2.5 {
			parts = append(parts, fmt.Sprintf("Min speed −%.0f km/h", refMin-cmpMin))
		}
		if refMax-cmpMax >= 3.0 {
			parts = append(parts, fmt.Sprintf("Top speed −%.0f km/h", refMax-cmpMax))
		}
		if cmpABS >= 3 {
			parts = append(parts, "ABS intervention")
		}
		if cmpTC >= 3 {
			parts = append(parts, "TC cut on exit")
		}
		if len(parts) == 0 && cmpFT+2 < refFT {
			parts = append(parts, "Less full-throttle time")
		}
	} else if delta < -0.06 {
		if cmpMin-refMin >= 2.5 {
			parts = append(parts, fmt.Sprintf("Carried +%.0f km/h min speed", cmpMin-refMin))
		}
		if cmpMax-refMax >= 3.0 {
			parts = append(parts, fmt.Sprintf("Top speed +%.0f km/h", cmpMax-refMax))
		}
		if refBrake >= 0 && cmpBrake >= 0 && cmpBrake > refBrake {
			m := int(math.Round(float64(cmpBrake-refBrake) * mPerBin))
			if m >= 8 {
				parts = append(parts, fmt.Sprintf("Braked ~%d m later", m))
			}
		}
		if len(parts) == 0 {
			parts = append(parts, "Stronger exit / throttle")
		}
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, " · ")
}

func alignTrace(src *Trace, n int) *Trace {
	if src.N == n {
		return src
	}
	out := &Trace{
		N:            n,
		TrackLengthM: src.TrackLengthM,
		HasCoords:    src.HasCoords,
		DistM:        make([]float32, n),
		TimeS:        make([]float32, n),
		SpeedKmh:     make([]float32, n),
		Throttle:     make([]float32, n),
		Brake:        make([]float32, n),
		SteerDeg:     make([]float32, n),
		Gear:         make([]int8, n),
		RPM:          make([]int32, n),
		TC:           make(FlagSlice, n),
		ABS:          make(FlagSlice, n),
	}
	if src.HasCoords && len(src.X) == src.N && len(src.Y) == src.N {
		out.X = make([]float32, n)
		out.Y = make([]float32, n)
	}
	for i := 0; i < n; i++ {
		pos := (float64(i) / float64(n-1)) * float64(src.N-1)
		i0 := int(pos)
		if i0 >= src.N-1 {
			i0 = src.N - 2
		}
		i1 := i0 + 1
		a := float32(pos - float64(i0))
		lerp32 := func(arr []float32) float32 {
			if len(arr) <= i1 {
				return 0
			}
			return arr[i0] + (arr[i1]-arr[i0])*a
		}
		out.DistM[i] = lerp32(src.DistM)
		out.TimeS[i] = lerp32(src.TimeS)
		out.SpeedKmh[i] = lerp32(src.SpeedKmh)
		out.Throttle[i] = lerp32(src.Throttle)
		out.Brake[i] = lerp32(src.Brake)
		out.SteerDeg[i] = lerp32(src.SteerDeg)
		if len(src.RPM) > i1 {
			out.RPM[i] = int32(float32(src.RPM[i0]) + float32(src.RPM[i1]-src.RPM[i0])*a)
		}
		near := i0
		if a >= 0.5 {
			near = i1
		}
		if len(src.Gear) > near {
			out.Gear[i] = src.Gear[near]
		}
		if len(src.TC) > near {
			out.TC[i] = src.TC[near]
		}
		if len(src.ABS) > near {
			out.ABS[i] = src.ABS[near]
		}
		if out.X != nil {
			out.X[i] = lerp32(src.X)
			out.Y[i] = lerp32(src.Y)
		}
	}
	return out
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
