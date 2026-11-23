// Package motec reads MoTeC i2 log files (.ld) as Assetto Corsa Competizione writes them (spec D-160).
// Files are only ever opened for reading; nothing here writes, moves or deletes a file.
//
// Layout (worked out by the community, checked on ACC's files 2026-11-22): a header with pointers
// to a linked list of channel descriptions; each channel's samples are one block of numbers.
//
//	header:  0 marker (0x40) · 8 first channel · 12 first data · 36 event
//	channel: 0 previous · 4 next · 8 data · 12 samples · 18 type group · 20 type size · 22 Hz
//	         24 shift · 26 multiplier · 28 scale · 30 decimals · 32 name[32] · 64 short[8] · 72 unit[12]
//	event:   0 name[64] · 64 session[64] · 128 comment[1024]
//
// ACC writes every channel as 4-byte floats with no scaling, but the other types are handled too.
package motec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Channel is one recorded value.
type Channel struct {
	Name  string `json:"name"`
	Short string `json:"short,omitempty"`
	Unit  string `json:"unit"`
	Hz    int    `json:"hz"`
	Count int    `json:"count"` // number of samples
	// Start is when the first sample was taken, in seconds from the start of the log. Set by
	// (*File).Laps: ACC keeps only its last laps in most channels (see there).
	Start float64 `json:"start,omitempty"`

	data              int64
	group, size       uint16
	shift, mul, scale int16
	dec               int16
}

// File is an open .ld file.
type File struct {
	Date, Time string // as the header has them, e.g. "17/11/2026", "22:06:38"
	Venue      string
	Channels   []Channel
	f          *os.File
	size       int64
	laps       []Lap
	split      bool
}

const (
	headerSize  = 0x6E2
	channelSize = 124
	maxChannels = 2000
)

// ErrFormat is returned for a file that is not a MoTeC log SetupMan can read.
var ErrFormat = errors.New("not a MoTeC log file")

// Open opens a .ld file for reading and reads its header and channel list (a few KB).
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	lf, err := parse(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return lf, nil
}

func parse(f *os.File) (*File, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, headerSize)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return nil, ErrFormat
	}
	le := binary.LittleEndian
	if le.Uint32(hdr[0:]) != 0x40 {
		return nil, ErrFormat
	}
	lf := &File{f: f, size: st.Size(),
		Date: cstr(hdr[0x5e:0x6e]), Time: cstr(hdr[0x7e:0x8e]), Venue: cstr(hdr[0x15e:0x19e])}
	seen := map[uint32]bool{}
	buf := make([]byte, channelSize)
	for p := le.Uint32(hdr[8:]); p != 0; {
		if seen[p] || len(lf.Channels) >= maxChannels || int64(p)+channelSize > lf.size {
			return nil, ErrFormat
		}
		seen[p] = true
		if _, err := f.ReadAt(buf, int64(p)); err != nil {
			return nil, ErrFormat
		}
		c := Channel{
			Name: cstr(buf[32:64]), Short: cstr(buf[64:72]), Unit: cstr(buf[72:84]),
			Hz: int(le.Uint16(buf[22:])), Count: int(le.Uint32(buf[12:])),
			data: int64(le.Uint32(buf[8:])), group: le.Uint16(buf[18:]), size: le.Uint16(buf[20:]),
			shift: int16(le.Uint16(buf[24:])), mul: int16(le.Uint16(buf[26:])),
			scale: int16(le.Uint16(buf[28:])), dec: int16(le.Uint16(buf[30:])),
		}
		if c.Unit == ".." { // ACC's placeholder for "no unit"
			c.Unit = ""
		}
		if c.valid(lf.size) {
			lf.Channels = append(lf.Channels, c)
		}
		p = le.Uint32(buf[4:])
	}
	if len(lf.Channels) == 0 {
		return nil, ErrFormat
	}
	return lf, nil
}

func (c Channel) valid(fileSize int64) bool {
	if c.Name == "" || c.Hz <= 0 || c.Hz > 10000 || c.Count < 0 {
		return false
	}
	switch {
	case c.group == 0x07 && (c.size == 2 || c.size == 4):
	case (c.group == 0 || c.group == 3 || c.group == 5) && (c.size == 2 || c.size == 4):
	default:
		return false
	}
	return c.data > 0 && c.data+int64(c.Count)*int64(c.size) <= fileSize
}

// Close closes the file.
func (lf *File) Close() error { return lf.f.Close() }

// Channel finds a channel by name (case doesn't matter).
func (lf *File) Channel(name string) (Channel, bool) {
	for _, c := range lf.Channels {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return Channel{}, false
}

// Window returns the sample range [from, to) of the channel that covers startS … startS+lenS
// (seconds from the start of the log), with one sample to spare at each end. It can lie partly or
// wholly outside the samples the channel has; Read clips it.
func (c Channel) Window(startS, lenS float64) (from, to int) {
	hz := float64(c.Hz)
	return int(math.Floor((startS - c.Start) * hz)), int(math.Ceil((startS-c.Start+lenS)*hz)) + 1
}

// Duration is the length of the log, from its longest-running channel.
func (lf *File) Duration() float64 {
	d := 0.0
	for _, c := range lf.Channels {
		d = math.Max(d, float64(c.Count)/float64(c.Hz))
	}
	return d
}

// Read returns the samples from index from up to (not including) to, already scaled. The range is
// clipped to the samples the channel has; only that part of the file is read.
func (lf *File) Read(c Channel, from, to int) ([]float32, error) {
	from = max(from, 0)
	to = min(to, c.Count)
	if to <= from {
		return []float32{}, nil
	}
	n := to - from
	raw := make([]byte, n*int(c.size))
	if _, err := lf.f.ReadAt(raw, c.data+int64(from)*int64(c.size)); err != nil && err != io.EOF {
		return nil, err
	}
	out := make([]float32, n)
	le := binary.LittleEndian
	plain := c.shift == 0 && (c.mul == 0 || c.mul == 1) && (c.scale == 0 || c.scale == 1) && c.dec == 0
	for i := range out {
		var v float64
		switch {
		case c.group == 0x07 && c.size == 4:
			if plain {
				out[i] = math.Float32frombits(le.Uint32(raw[4*i:]))
				continue
			}
			v = float64(math.Float32frombits(le.Uint32(raw[4*i:])))
		case c.group == 0x07:
			v = float64(half(le.Uint16(raw[2*i:])))
		case c.size == 4:
			v = float64(int32(le.Uint32(raw[4*i:])))
		default:
			v = float64(int16(le.Uint16(raw[2*i:])))
		}
		out[i] = float32(c.apply(v))
	}
	return out, nil
}

// ReadAll returns every sample of a channel.
func (lf *File) ReadAll(c Channel) ([]float32, error) { return lf.Read(c, 0, c.Count) }

// apply turns a stored number into the real value: (raw / scale × 10^-decimals + shift) × multiplier.
func (c Channel) apply(v float64) float64 {
	scale, mul := float64(c.scale), float64(c.mul)
	if scale == 0 {
		scale = 1
	}
	if mul == 0 {
		mul = 1
	}
	return (v/scale*math.Pow(10, -float64(c.dec)) + float64(c.shift)) * mul
}

// half converts an IEEE 754 half-precision number.
func half(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h & 0x3ff)
	switch {
	case exp == 0 && frac == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal
		f := float32(frac) / 1024 * float32(math.Pow(2, -14))
		if sign != 0 {
			return -f
		}
		return f
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | frac<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | frac<<13)
}

func cstr(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(b), ""))
}
