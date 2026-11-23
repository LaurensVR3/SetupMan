package telemetry

import (
	"math"
	"sort"
	"strconv"

	"setupman/internal/motec"
	"setupman/internal/store"
)

// Floats is a list of numbers sent to the browser with 5 significant digits (smaller and faster
// than the default encoding; telemetry is never more exact than that).
type Floats []float32

// MarshalJSON writes the numbers compactly.
func (fs Floats) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, len(fs)*7+2)
	b = append(b, '[')
	for i, v := range fs {
		if i > 0 {
			b = append(b, ',')
		}
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			b = append(b, '0')
			continue
		}
		b = strconv.AppendFloat(b, float64(v), 'g', 5, 32)
	}
	return append(b, ']'), nil
}

// ChannelData is one channel over one lap.
type ChannelData struct {
	ChannelInfo
	Data Floats `json:"data"`
	// T: the lap time of every value, seconds (extra log only: its readings are not evenly spaced).
	// MoTeC values are evenly spaced at Hz; the first lies T0 seconds from the lap start (−1/Hz … 0).
	T  Floats  `json:"t,omitempty"`
	T0 float64 `json:"t0"`
}

// LapData is everything the analysis page draws for one lap.
type LapData struct {
	FileID int64 `json:"fileId"`
	N      int   `json:"n"`
	LapMs  int   `json:"lapMs"`
	// Dist: metres along the lap at DistHz, the first value DistT0 seconds from the lap start (speed
	// integrated over time; 0 m at the lap start).
	Dist     Floats        `json:"dist"`
	DistHz   int           `json:"distHz"`
	DistT0   float64       `json:"distT0"`
	Channels []ChannelData `json:"channels"`
	Map      *TrackMap     `json:"map,omitempty"`
}

// Channels lists the channels of a MoTeC file plus the extra log (nil when none), in picker order.
func Channels(lf *motec.File, extra *ExtraLog) []ChannelInfo {
	var out []ChannelInfo
	for _, c := range lf.Channels {
		out = append(out, describe(c.Name, c.Unit, c.Hz, "motec"))
	}
	if extra != nil {
		for _, c := range extra.Header.Channels {
			if !hiddenExtra[c.Name] {
				out = append(out, describe(extraPrefix+c.Name, c.Unit, extra.Header.Hz, "extra"))
			}
		}
	}
	order := map[string]int{}
	for i, g := range Groups {
		order[g] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].Group] != order[out[j].Group] {
			return order[out[i].Group] < order[out[j].Group]
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// ReadLap reads the named channels of one lap. xlap (with extra) is the same lap in the extra log.
func ReadLap(lf *motec.File, lap store.TeleLap, names []string, extra *ExtraLog, xlap *ExtraLap) (*LapData, error) {
	startS, lapS := float64(lap.StartMs)/1000, float64(lap.LapMs)/1000
	if _, err := lf.Laps(); err != nil { // sets when each channel starts
		return nil, err
	}
	out := &LapData{FileID: lap.FileID, N: lap.N, LapMs: lap.LapMs, Channels: []ChannelData{}}
	if sp, ok := lf.Channel("SPEED"); ok {
		from, to := sp.Window(startS, lapS)
		v, err := lf.Read(sp, from, to)
		if err != nil {
			return nil, err
		}
		from = max(from, 0) // Read clips the range
		out.Dist, out.DistHz, out.DistT0 = distance(v, sp.Hz), sp.Hz, sp.Start+float64(from)/float64(sp.Hz)-startS
		// distance() starts at the first sample, which lies just before the lap start: shift so the
		// lap start is 0 m
		if len(v) > 0 {
			shift := float32(-out.DistT0) * v[0]
			for i := range out.Dist {
				out.Dist[i] -= shift
			}
		}
	}
	for _, name := range names {
		if len(name) > len(extraPrefix) && name[:len(extraPrefix)] == extraPrefix {
			if cd, ok := extraChannel(extra, xlap, name); ok {
				out.Channels = append(out.Channels, cd)
			}
			continue
		}
		c, ok := lf.Channel(name)
		if !ok {
			continue
		}
		from, to := c.Window(startS, lapS)
		v, err := lf.Read(c, from, to)
		if err != nil {
			return nil, err
		}
		from = max(from, 0)
		ci := describe(c.Name, c.Unit, c.Hz, "motec")
		if ci.Scale != 1 {
			for i := range v {
				v[i] *= float32(ci.Scale)
			}
		}
		out.Channels = append(out.Channels, ChannelData{ChannelInfo: ci, Data: v, T0: c.Start + float64(from)/float64(c.Hz) - startS})
	}
	return out, nil
}

// distance integrates speed (m/s, evenly spaced at hz) into metres from the first sample.
func distance(speed []float32, hz int) Floats {
	d := make(Floats, len(speed))
	dt := 1 / float64(hz)
	sum := 0.0
	for i := 1; i < len(speed); i++ {
		sum += (float64(speed[i-1]) + float64(speed[i])) / 2 * dt
		d[i] = float32(sum)
	}
	return d
}

// extraChannel cuts one extra-log channel to a lap; T is the game's own lap clock.
func extraChannel(extra *ExtraLog, xlap *ExtraLap, name string) (ChannelData, bool) {
	if extra == nil || xlap == nil {
		return ChannelData{}, false
	}
	idx, ti := extra.Index(name[len(extraPrefix):]), extra.Index(ExtraLapTime)
	if idx < 0 || ti < 0 {
		return ChannelData{}, false
	}
	ci := describe(name, extra.Header.Channels[idx].Unit, extra.Header.Hz, "extra")
	cd := ChannelData{ChannelInfo: ci}
	for r := xlap.From; r < xlap.To; r++ {
		cd.T = append(cd.T, extra.Data[ti][r]/1000)
		cd.Data = append(cd.Data, extra.Data[idx][r]*float32(ci.Scale))
	}
	return cd, true
}
