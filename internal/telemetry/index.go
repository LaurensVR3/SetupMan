package telemetry

import (
	"math"
	"os"
	"path/filepath"
	"time"

	"setupman/internal/motec"
	"setupman/internal/store"
)

// IndexFile reads what the library keeps about a MoTeC file: car, track, start, length and laps.
// Only the header and the lap clock are read (about 1.5 MB for a two-hour session).
func IndexFile(dir, name, game string) (*store.TeleFile, []store.TeleLap, error) {
	track, car, saved, ok := ParseName(name)
	if !ok {
		return nil, nil, motec.ErrFormat
	}
	// The name holds when ACC saved the file, not when the session started: ACC saves the whole
	// session so far again and again (checked on the user's files, 2026-11-22). Start = saved − length.
	path := filepath.Join(dir, name)
	st, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	lf, err := motec.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer lf.Close()
	f := &store.TeleFile{Game: game, Name: name, Size: st.Size(), Modified: st.ModTime(), Car: car, Track: track,
		DurationMs: int64(math.Round(lf.Duration() * 1000)), Channels: len(lf.Channels)}
	f.StartedAt = saved.Add(-time.Duration(f.DurationMs) * time.Millisecond)
	var laps []store.TeleLap
	ml, err := lf.Laps() // also works out when each channel starts
	if err != nil {
		return nil, nil, err
	}
	for _, l := range ml {
		laps = append(laps, store.TeleLap{N: l.N, StartMs: int64(math.Round(l.Start * 1000)), LapMs: int(math.Round(l.Time * 1000)), Complete: l.Complete})
	}
	if len(laps) == 0 && f.DurationMs > 1000 {
		laps = []store.TeleLap{{N: 0, LapMs: int(f.DurationMs)}}
	}
	if sp, ok := lf.Channel("SPEED"); ok {
		for i := range laps { // one lap at a time: little memory even for a two-hour session
			from, to := sp.Window(float64(laps[i].StartMs)/1000, float64(laps[i].LapMs)/1000)
			v, err := lf.Read(sp, from, to)
			if err != nil {
				return nil, nil, err
			}
			d := distance(v, sp.Hz)
			if len(d) > 0 {
				laps[i].DistM = float64(d[len(d)-1])
			}
			laps[i].Profile = profile(v, d)
		}
	}
	if yw, ok := lf.Channel("ROTY"); ok { // how far the car turned: ±1 on a normal lap (D-163)
		for i := range laps {
			from, to := yw.Window(float64(laps[i].StartMs)/1000, float64(laps[i].LapMs)/1000)
			v, err := lf.Read(yw, from, to)
			if err != nil {
				return nil, nil, err
			}
			sum := 0.0
			for _, r := range v {
				sum += float64(r)
			}
			laps[i].Turns = sum / float64(yw.Hz) / (2 * math.Pi)
		}
	}
	return f, laps, nil
}

// Changed tells whether a file on disk differs from what was indexed.
func Changed(k store.TeleKnown, size int64, mod time.Time) bool {
	return k.Size != size || k.Modified.UnixMilli() != mod.UnixMilli()
}

// Match picks the recorded session a file belongs to and links its laps to recorded laps.
// A session fits when it started between 15 minutes before the file and the file's end; among
// those, the one with the most laps of the same time (within 3 ms, and completed within 90 s of
// when the file says) wins. A session without matching laps is only taken when it started within
// two minutes of the file and the file has no complete laps.
func Match(f store.TeleFile, laps []store.TeleLap, sessions []store.Session, recorded map[int64][]store.Lap) (int64, map[int]int64) {
	var bestID int64
	var bestLinks map[int]int64
	best := 0
	for _, s := range sessions {
		links := map[int]int64{}
		used := map[int64]bool{}
		for _, tl := range laps {
			if !tl.Timed { // every timed lap: pit laps and laps without data need their recorded lap too
				continue
			}
			end := f.StartedAt.Add(time.Duration(tl.StartMs+int64(tl.LapMs)) * time.Millisecond)
			for _, rl := range recorded[s.ID] {
				if used[rl.ID] || abs(rl.LapMs-tl.LapMs) > 3 || math.Abs(rl.At.Sub(end).Seconds()) > 90 {
					continue
				}
				links[tl.N], used[rl.ID] = rl.ID, true
				break
			}
		}
		if len(links) > best {
			best, bestID, bestLinks = len(links), s.ID, links
		}
	}
	if best > 0 {
		// ACC keeps one file for a whole server weekend (practice, qualifying and race; rig
		// 2026-11-23): the file goes to the session with the most laps, but every lap is linked to
		// its own recorded lap, whichever session that is in (D-179).
		used := map[int64]bool{}
		for _, id := range bestLinks {
			used[id] = true
		}
		for _, s := range sessions {
			if s.ID == bestID {
				continue
			}
			for _, tl := range laps {
				if !tl.Timed || bestLinks[tl.N] != 0 {
					continue
				}
				end := f.StartedAt.Add(time.Duration(tl.StartMs+int64(tl.LapMs)) * time.Millisecond)
				for _, rl := range recorded[s.ID] {
					if used[rl.ID] || abs(rl.LapMs-tl.LapMs) > 3 || math.Abs(rl.At.Sub(end).Seconds()) > 90 {
						continue
					}
					bestLinks[tl.N], used[rl.ID] = rl.ID, true
					break
				}
			}
		}
		return bestID, bestLinks
	}
	complete := 0
	for _, l := range laps {
		if l.Timed {
			complete++
		}
	}
	if complete > 0 {
		return 0, nil
	}
	for _, s := range sessions {
		if math.Abs(s.StartedAt.Sub(f.StartedAt).Seconds()) <= 120 {
			return s.ID, map[int]int64{}
		}
	}
	return 0, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// profile is the lowest speed in each 1 % of a lap's distance, in km/h ÷ 2 (store.ProfileBins
// values; nil for a lap without data). RefreshTelemetryFull uses it to spot cut chicanes (D-167).
func profile(speed []float32, dist Floats) []byte {
	if len(speed) < 2 || len(dist) != len(speed) || dist[len(dist)-1] < 100 {
		return nil
	}
	total := float64(dist[len(dist)-1])
	out := make([]byte, store.ProfileBins)
	for i := range out {
		out[i] = 255
	}
	for i, v := range speed {
		b := int(float64(dist[i]) / total * store.ProfileBins)
		b = min(max(b, 0), store.ProfileBins-1)
		kmh := math.Max(0, float64(v)*3.6/2)
		if k := byte(math.Min(255, kmh)); k < out[b] {
			out[b] = k
		}
	}
	for i := range out { // a part without a sample (very short laps): its neighbour's value
		if out[i] == 255 && i > 0 {
			out[i] = out[i-1]
		}
	}
	return out
}
