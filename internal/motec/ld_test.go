package motec

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestChannel is a channel for WriteTestFile.
type testChannel struct {
	name, unit string
	hz         int
	data       []float32
}

// writeTestFile writes a small .ld file in ACC's layout (4-byte floats, no scaling).
func writeTestFile(t *testing.T, path, venue string, chans []testChannel) {
	t.Helper()
	le := binary.LittleEndian
	metaStart := headerSize
	dataStart := metaStart + channelSize*len(chans)
	buf := make([]byte, dataStart)
	le.PutUint32(buf[0:], 0x40)
	le.PutUint32(buf[8:], uint32(metaStart))
	le.PutUint32(buf[12:], uint32(dataStart))
	copy(buf[0x5e:], "17/11/2026")
	copy(buf[0x7e:], "22:06:38")
	copy(buf[0x15e:], venue)
	off := dataStart
	for i, c := range chans {
		m := buf[metaStart+i*channelSize:]
		if i > 0 {
			le.PutUint32(m[0:], uint32(metaStart+(i-1)*channelSize))
		}
		if i < len(chans)-1 {
			le.PutUint32(m[4:], uint32(metaStart+(i+1)*channelSize))
		}
		le.PutUint32(m[8:], uint32(off))
		le.PutUint32(m[12:], uint32(len(c.data)))
		le.PutUint16(m[18:], 0x07)
		le.PutUint16(m[20:], 4)
		le.PutUint16(m[22:], uint16(c.hz))
		le.PutUint16(m[26:], 1)
		le.PutUint16(m[28:], 1)
		copy(m[32:64], c.name)
		copy(m[72:84], c.unit)
		off += 4 * len(c.data)
	}
	for _, c := range chans {
		for _, v := range c.data {
			buf = le.AppendUint32(buf, math.Float32bits(v))
		}
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAndRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "spa-ferrari_296_gt3-3-2026.11.17-22.06.38.ld")
	speed := []float32{0, 10, 20, 30, 40, 50}
	writeTestFile(t, p, "spa", []testChannel{{"SPEED", "m/s", 60, speed}, {"GEAR", "..", 20, []float32{1, 2}}})
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Venue != "spa" || len(f.Channels) != 2 || f.Date != "17/11/2026" {
		t.Fatalf("header: %+v", f)
	}
	g, _ := f.Channel("gear")
	if g.Unit != "" || g.Hz != 20 {
		t.Fatalf("gear: %+v", g)
	}
	s, ok := f.Channel("SPEED")
	if !ok {
		t.Fatal("no SPEED")
	}
	v, err := f.Read(s, 2, 4)
	if err != nil || len(v) != 2 || v[0] != 20 || v[1] != 30 {
		t.Fatalf("read: %v %v", v, err)
	}
	if v, _ := f.Read(s, 4, 100); len(v) != 2 {
		t.Fatalf("clipped read: %v", v)
	}
	if d := f.Duration(); math.Abs(d-0.1) > 1e-9 {
		t.Fatalf("duration %v", d)
	}
}

func TestNotMoTeC(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.ld")
	os.WriteFile(p, []byte("hello"), 0o644)
	if _, err := Open(p); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLaps(t *testing.T) {
	// 10 Hz clock: an out-lap of 1.15 s, a lap of 2.004 s, then 2 s of an in-lap.
	var clock []float32
	for i := 0; i < 12; i++ { // samples 0..11: 0 … 1.1 s
		clock = append(clock, float32(i)*0.1)
	}
	for i := 0; i < 20; i++ { // sample 12 (1.2 s) reads 0.05: the line was crossed at 1.15 s
		clock = append(clock, 0.05+float32(i)*0.1)
	}
	// sample 32 reads 0.046 where the old clock would read 2.05: the lap took 1.95 + 0.1 − 0.046 = 2.004 s
	for i := 0; i < 20; i++ {
		clock = append(clock, 0.046+float32(i)*0.1)
	}
	laps := Laps(clock, 10)
	if len(laps) != 3 {
		t.Fatalf("laps: %+v", laps)
	}
	if laps[0].Complete || laps[1].Complete != true || laps[2].Complete {
		t.Fatalf("complete flags: %+v", laps)
	}
	if math.Abs(laps[1].Time-2.004) > 1e-4 {
		t.Fatalf("lap time %.4f", laps[1].Time)
	}
	if math.Abs(laps[1].Start-1.15) > 0.11 {
		t.Fatalf("lap start %.4f", laps[1].Start)
	}
}

// ACC keeps most channels only for its last laps: they end at the last line crossing (D-167).
func TestFileLapsChannelStart(t *testing.T) {
	// 10 Hz clock: an out-lap of 2 s, laps of 3 s and 3 s, then 1.5 s of an in-lap
	var clock []float32
	for _, part := range []int{20, 30, 30, 15} {
		for i := 0; i < part; i++ {
			clock = append(clock, float32(i)*0.1)
		}
	}
	// SPEED (20 Hz) holds only the last full lap (5 … 8 s): its sample k was taken at 5 + k/20 s
	speed := make([]float32, 60)
	for k := range speed {
		speed[k] = float32(k)
	}
	p := filepath.Join(t.TempDir(), "spa-ferrari_296_gt3-3-2026.11.17-22.06.38.ld")
	writeTestFile(t, p, "spa", []testChannel{{"TIME", "s", 10, clock}, {"SPEED", "m/s", 20, speed}})
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	laps, err := f.Laps()
	if err != nil || len(laps) != 4 {
		t.Fatalf("laps: %+v %v", laps, err)
	}
	s, _ := f.Channel("SPEED")
	if math.Abs(s.Start-5) > 1e-3 {
		t.Fatalf("SPEED starts at %.3f s, want 5", s.Start)
	}
	if tc, _ := f.Channel("TIME"); tc.Start != 0 {
		t.Fatalf("TIME starts at %.3f s", tc.Start)
	}
	// the last full lap (5 … 8 s) is the whole channel; the lap before it has no speed data
	from, to := s.Window(laps[2].Start, laps[2].Time)
	if v, _ := f.Read(s, from, to); len(v) < 60 || v[0] != 0 {
		t.Fatalf("lap 2: %d samples from %v", len(v), v[:1])
	}
	from, to = s.Window(laps[1].Start, laps[1].Time)
	if v, _ := f.Read(s, from, to); len(v) > 2 { // the spare sample at each end
		t.Fatalf("lap 1 should have (almost) no speed data, got %d samples", len(v))
	}
}

func TestHalf(t *testing.T) {
	for h, want := range map[uint16]float32{0x3c00: 1, 0xc000: -2, 0x3555: 0.33325195, 0: 0} {
		if got := half(h); math.Abs(float64(got-want)) > 1e-6 {
			t.Errorf("half(%#x) = %v, want %v", h, got, want)
		}
	}
}

// TestRealFiles reads the user's ACC MoTeC files when they are present (git-ignored, D-160).
func TestRealFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "Telemetry", "Assetto Corsa Competizione", "MoTeC")
	files, _ := filepath.Glob(filepath.Join(dir, "*.ld"))
	if len(files) == 0 {
		t.Skip("no MoTeC files")
	}
	for _, p := range files[:min(5, len(files))] {
		f, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		c, ok := f.Channel("TIME")
		if !ok {
			t.Fatalf("%s: no TIME", p)
		}
		clock, err := f.ReadAll(c)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range Laps(clock, c.Hz) {
			if l.Complete && (l.Time < 30 || l.Time > 1200) {
				t.Errorf("%s: odd lap %+v", filepath.Base(p), l)
			}
		}
	}
}
