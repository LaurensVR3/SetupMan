package telemetry

// SetupMan's extra log (spec D-162): the values ACC's MoTeC files lack, about 10 readings per
// second while a session is live, one file per recorded session in %APPDATA%\SetupMan\telemetry.
//
// File: gzip of the line "SMXL1", one line of JSON (ExtraHeader), then rows of little-endian
// float32: seconds since the start, then one value per channel. The writer flushes every few
// seconds, so a file stays readable up to the last flush if SetupMan stops unexpectedly.

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	extraMagic  = "SMXL1"
	extraPrefix = "x:" // extra channels are named "x:<name>" next to the MoTeC channels
	flushEvery  = 5 * time.Second
)

// Extra channels SetupMan's extra log uses for itself (not shown in the channel picker).
const (
	ExtraLapCount = "LAP"      // laps completed in the session
	ExtraLapTime  = "LAP_TIME" // current lap time, ms (the same clock as MoTeC's TIME)
	ExtraLastLap  = "LAST_LAP" // last lap time, ms
	ExtraPosX     = "POS_X"    // car position, m (horizontal)
	ExtraPosZ     = "POS_Z"    // car position, m (horizontal)
)

// extraKnown: readable names of the extra channels. Units are only given where the game's
// documentation states them; the others are shown as the game reports them.
var extraKnown = map[string]known{
	"FUEL":        {"Fuel", "Engine", "L", 1, false},
	"WATER_TEMP":  {"Water temperature", "Engine", "°C", 1, false},
	"LAP_VALID":   {"Lap valid", "Timing", "", 1, true},
	"LAP_POS":     {"Position along the lap", "Timing", "%", 100, false},
	"PIT_LANE":    {"In the pit lane", "Timing", "", 1, true},
	"SLIP_ANGLE":  {"Slip angle", "Tyres", "", 1, false},
	"SLIP_RATIO":  {"Slip ratio", "Tyres", "", 1, false},
	"WHEEL_SLIP":  {"Wheel slip", "Tyres", "", 1, false},
	"BRAKE_PRESS": {"Brake pressure", "Brakes", "", 1, false},
	"BRAKE_TEMP":  {"Brake temperature", "Brakes", "°C", 1, false},
	"PAD_LIFE":    {"Brake pad life", "Brakes", "", 1, false},
	"DISC_LIFE":   {"Brake disc life", "Brakes", "", 1, false},
	"RIDE_HEIGHT": {"Ride height", "Suspension", "", 1, false},
	"PITCH":       {"Pitch", "Chassis", "°", 57.29578, false},
	"ROLL":        {"Roll", "Chassis", "°", 57.29578, false},
	"HEADING":     {"Heading", "Chassis", "°", 57.29578, false},
	"TC_ACTIVE":   {"TC active", "Electronics", "", 1, true},
	"ABS_ACTIVE":  {"ABS active", "Electronics", "", 1, true},
	"TC_LEVEL":    {"TC level (live)", "Electronics", "", 1, true},
	"TC_CUT":      {"TC cut", "Electronics", "", 1, true},
	"ABS_LEVEL":   {"ABS level (live)", "Electronics", "", 1, true},
	"ENGINE_MAP":  {"Engine map", "Electronics", "", 1, true},
	"BRAKE_BIAS":  {"Brake bias (raw)", "Brakes", "", 1, false},
	"AIR_TEMP":    {"Air temperature", "Track", "°C", 1, false},
	"ROAD_TEMP":   {"Track temperature", "Track", "°C", 1, false},
	"TRACK_GRIP":  {"Track grip status", "Track", "", 1, true},
	"RAIN":        {"Rain intensity", "Track", "", 1, true},
}

// hiddenExtra are used to align and draw (map), not offered as channels.
var hiddenExtra = map[string]bool{ExtraLapCount: true, ExtraLapTime: true, ExtraLastLap: true, ExtraPosX: true, ExtraPosZ: true, "POS_Y": true}

// ExtraChannel is one channel of the extra log.
type ExtraChannel struct {
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
}

// ExtraHeader starts every extra log.
type ExtraHeader struct {
	Game     string         `json:"game"`
	Session  int64          `json:"session"`
	Start    time.Time      `json:"start"`
	Hz       int            `json:"hz"`
	Channels []ExtraChannel `json:"channels"`
}

// ExtraPath is the extra log of a session in dir.
func ExtraPath(dir, game string, session int64) string {
	return filepath.Join(dir, fmt.Sprintf("%s-%d.smx.gz", game, session))
}

// ExtraWriter writes one extra log.
type ExtraWriter struct {
	f         *os.File
	gz        *gzip.Writer
	bw        *bufio.Writer
	width     int
	row       []byte
	lastFlush time.Time
}

// CreateExtra starts a new extra log (an existing file of the same name is replaced).
func CreateExtra(path string, h ExtraHeader) (*ExtraWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	bw := bufio.NewWriterSize(gz, 64<<10)
	hj, _ := json.Marshal(h)
	bw.WriteString(extraMagic + "\n")
	bw.Write(hj)
	bw.WriteByte('\n')
	w := &ExtraWriter{f: f, gz: gz, bw: bw, width: len(h.Channels), lastFlush: time.Now()}
	w.row = make([]byte, 4*(1+w.width))
	return w, w.flush()
}

// AppendExtra continues an extra log (a session that goes on after SetupMan restarted, A-5): the
// rows are added as a new gzip part after the old one, which the reader reads as one stream. Only
// when the log has the same channels and its end is complete (SetupMan closed it); returns the log's
// start, to which new rows' times are relative.
func AppendExtra(path string, chans []ExtraChannel) (*ExtraWriter, time.Time, error) {
	x, err := ReadExtra(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(x.Header.Channels) != len(chans) {
		return nil, time.Time{}, errors.New("its channels differ")
	}
	for i, c := range chans {
		if x.Header.Channels[i].Name != c.Name {
			return nil, time.Time{}, errors.New("its channels differ")
		}
	}
	if !x.Complete {
		return nil, time.Time{}, errors.New("its end is incomplete")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, time.Time{}, err
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	w := &ExtraWriter{f: f, gz: gz, bw: bufio.NewWriterSize(gz, 64<<10), width: len(chans), lastFlush: time.Now()}
	w.row = make([]byte, 4*(1+w.width))
	return w, x.Header.Start, nil
}

// Write adds one reading: t = seconds since the start, vals = one value per channel.
func (w *ExtraWriter) Write(t float64, vals []float32) error {
	if len(vals) != w.width {
		return errors.New("extra log: wrong number of values")
	}
	le := binary.LittleEndian
	le.PutUint32(w.row, math.Float32bits(float32(t)))
	for i, v := range vals {
		le.PutUint32(w.row[4+4*i:], math.Float32bits(v))
	}
	if _, err := w.bw.Write(w.row); err != nil {
		return err
	}
	if time.Since(w.lastFlush) >= flushEvery {
		return w.flush()
	}
	return nil
}

func (w *ExtraWriter) flush() error {
	w.lastFlush = time.Now()
	if err := w.bw.Flush(); err != nil {
		return err
	}
	return w.gz.Flush()
}

// Close finishes the file.
func (w *ExtraWriter) Close() error {
	err := w.bw.Flush()
	if cerr := w.gz.Close(); err == nil {
		err = cerr
	}
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// ExtraLog is a whole extra log in memory.
type ExtraLog struct {
	Header ExtraHeader
	T      []float32   // seconds since the start
	Data   [][]float32 // Data[channel][row]
	// Complete: the file ended cleanly (no cut-off tail), so rows can be appended (A-5)
	Complete bool
}

// Index returns the position of a channel (-1 when absent).
func (x *ExtraLog) Index(name string) int {
	for i, c := range x.Header.Channels {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// ReadExtra reads an extra log. A file cut short (SetupMan stopped while writing) is read up to
// its last complete row.
func ReadExtra(path string) (*ExtraLog, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(gz, 64<<10)
	magic, err := br.ReadString('\n')
	if err != nil || strings.TrimSpace(magic) != extraMagic {
		return nil, errors.New("not a SetupMan extra log")
	}
	hl, err := br.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	x := &ExtraLog{}
	if err := json.Unmarshal(hl, &x.Header); err != nil {
		return nil, err
	}
	n := len(x.Header.Channels)
	if n == 0 || n > 500 {
		return nil, errors.New("extra log: bad header")
	}
	x.Data = make([][]float32, n)
	row := make([]byte, 4*(1+n))
	le := binary.LittleEndian
	for {
		if _, err := io.ReadFull(br, row); err != nil {
			x.Complete = err == io.EOF // a clean end; ErrUnexpectedEOF = a cut-off tail
			break                      // end of file, or the cut-off tail of a file that was not closed
		}
		x.T = append(x.T, math.Float32frombits(le.Uint32(row)))
		for i := 0; i < n; i++ {
			x.Data[i] = append(x.Data[i], math.Float32frombits(le.Uint32(row[4+4*i:])))
		}
	}
	return x, nil
}

// ExtraLap is one lap found in an extra log: rows [From, To) and the lap time the game reported.
type ExtraLap struct {
	From, To int
	LapMs    int
}

// Laps splits the log into laps, using the game's lap counter and its last-lap time.
func (x *ExtraLog) Laps() []ExtraLap {
	li, ti, lt := x.Index(ExtraLapCount), x.Index(ExtraLapTime), x.Index(ExtraLastLap)
	if li < 0 || ti < 0 || lt < 0 || len(x.T) == 0 {
		return nil
	}
	var out []ExtraLap
	from := 0
	for i := 1; i < len(x.T); i++ {
		if x.Data[li][i] > x.Data[li][i-1] {
			out = append(out, ExtraLap{From: from, To: i, LapMs: int(x.Data[lt][i])})
			from = i
		} else if x.Data[li][i] < x.Data[li][i-1] {
			from = i // the lap counter went back: a new session in the same file
		}
	}
	return out
}

// FindLap returns the first extra-log lap with this lap time (within 3 ms). Two laps of one session
// with the same time to the millisecond are rare enough to ignore.
func (x *ExtraLog) FindLap(lapMs int) (ExtraLap, bool) {
	for _, l := range x.Laps() {
		if d := l.LapMs - lapMs; d >= -3 && d <= 3 {
			return l, true
		}
	}
	return ExtraLap{}, false
}
