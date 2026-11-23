package motec

// Lap is one lap inside a log. Times are seconds from the start of the log.
type Lap struct {
	N        int     `json:"n"`        // 0 = from the start of the log to the first line crossing
	Start    float64 `json:"start"`    // seconds from the start of the log
	Time     float64 `json:"time"`     // lap time in seconds
	Complete bool    `json:"complete"` // crossed the line at the start and the end
}

// Laps splits a log into laps using ACC's TIME channel: the running lap clock, which goes back to
// zero on every crossing of the line (checked on ACC's files, 2026-11-22). Because the clock keeps
// running between samples, a lap time is known to the millisecond even at 50 samples per second:
// the lap ends at sample i minus the clock value at sample i.
// The first part (before the first crossing) and the last part (after the last) are partial laps.
func Laps(clock []float32, hz int) []Lap {
	if hz <= 0 || len(clock) == 0 {
		return nil
	}
	dt := 1 / float64(hz)
	var laps []Lap
	start := 0.0
	for i := 1; i < len(clock); i++ {
		prev, cur := float64(clock[i-1]), float64(clock[i])
		if cur >= prev-0.5 {
			continue // the clock runs on
		}
		cross := float64(i)*dt - cur
		if cross < start {
			cross = start
		}
		lap := Lap{N: len(laps), Start: start, Time: prev + dt - cur, Complete: len(laps) > 0}
		if !lap.Complete {
			lap.Time = cross - start // the part before the first crossing: its own clock started with the log
		}
		laps = append(laps, lap)
		start = cross
	}
	end := float64(len(clock)) * dt
	if end-start > 1 {
		laps = append(laps, Lap{N: len(laps), Start: start, Time: end - start})
	}
	return laps
}

// Laps splits the log into laps with its TIME channel (nil when it has none) and works out when
// each channel starts.
//
// ACC writes TIME (and the engine channels) for the whole session, but most channels (speed,
// pedals, suspension, tyres …) only for its last laps: they hold whole laps and end at the last
// line crossing. Checked on the user's files (2026-11-23): in 927 of 2135 files these channels are
// shorter than TIME, and in 924 of them they start exactly at a lap start; in the others they run
// to the last crossing too (the part after it is never saved). So a channel that is shorter than
// TIME starts at the last crossing minus its own length. Laps before that have no data there.
func (lf *File) Laps() ([]Lap, error) {
	if lf.split {
		return lf.laps, nil
	}
	c, ok := lf.Channel("TIME")
	if !ok {
		lf.split = true
		return nil, nil
	}
	clock, err := lf.ReadAll(c)
	if err != nil {
		return nil, err
	}
	lf.laps, lf.split = Laps(clock, c.Hz), true
	full := float64(c.Count) / float64(c.Hz)
	last := -1.0 // the last line crossing
	for i, l := range lf.laps {
		if l.Complete || (i == 0 && len(lf.laps) > 1) {
			last = l.Start + l.Time
		}
	}
	if last <= 0 {
		return lf.laps, nil
	}
	for i := range lf.Channels {
		ch := &lf.Channels[i]
		d := float64(ch.Count) / float64(ch.Hz)
		if full-d > 0.5 && last-d > -0.5 {
			ch.Start = max(0, last-d)
		}
	}
	return lf.laps, nil
}
