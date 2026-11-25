package motec

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cutAndOpen cuts laps first … last out of f into a new file and opens it.
func cutAndOpen(t *testing.T, f *File, first, last int) (*File, CutResult) {
	t.Helper()
	var buf bytes.Buffer
	res, err := f.Cut(&buf, first, last)
	if err != nil {
		t.Fatalf("cut %d…%d: %v", first, last, err)
	}
	p := filepath.Join(t.TempDir(), "cut.ld")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Open(p)
	if err != nil {
		t.Fatalf("cut file doesn't open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c, res
}

// checkCut: the cut log holds the same full laps (times to the millisecond) and each channel's
// samples sit at the same moment as in the source (within one sample).
func checkCut(t *testing.T, name string, src *File, first, last int) {
	t.Helper()
	srcLaps, _ := src.Laps()
	cut, res := cutAndOpen(t, src, first, last)
	laps, err := cut.Laps()
	if err != nil {
		t.Fatal(err)
	}
	var full []Lap
	for _, l := range laps {
		if l.Complete {
			full = append(full, l)
		}
	}
	if len(full) != last-first+1 {
		t.Fatalf("%s: cut %d…%d has %d full laps, want %d: %+v", name, first, last, len(full), last-first+1, laps)
	}
	for i, l := range full {
		want := srcLaps[first+i]
		if math.Round(l.Time*1000) != math.Round(want.Time*1000) {
			t.Errorf("%s lap %d: %.3f s, want %.3f s", name, want.N, l.Time, want.Time)
		}
		if math.Abs(l.Start+res.Offset-want.Start) > 0.001 {
			t.Errorf("%s lap %d starts at %.3f s, want %.3f s", name, want.N, l.Start+res.Offset, want.Start)
		}
	}
	for _, sc := range src.Channels {
		cc, ok := cut.Channel(sc.Name)
		if !ok {
			continue // no data for these laps in the source
		}
		// the first sample of the cut channel was this sample of the source
		k := int(math.Round((cc.Start + res.Offset - sc.Start) * float64(sc.Hz)))
		if math.Abs(cc.Start+res.Offset-(sc.Start+float64(k)/float64(sc.Hz))) > 1.01/float64(sc.Hz) {
			t.Errorf("%s %s: cut starts between samples", name, sc.Name)
			continue
		}
		a, _ := src.Read(sc, k, k+cc.Count)
		b, _ := cut.ReadAll(cc)
		if len(a) != len(b) {
			t.Errorf("%s %s: %d samples, source has %d there", name, sc.Name, len(b), len(a))
			continue
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("%s %s: sample %d is %v, source %v", name, sc.Name, i, b[i], a[i])
				break
			}
		}
	}
}

func TestCutSmall(t *testing.T) {
	// 10 Hz clock: an out-lap of 2 s, laps of 3 s and 3 s, then 1.5 s of an in-lap; SPEED (20 Hz)
	// holds only the last full lap, RPM the whole log
	var clock, rpm []float32
	for _, part := range []int{20, 30, 30, 15} {
		for i := 0; i < part; i++ {
			clock = append(clock, float32(i)*0.1)
			rpm = append(rpm, float32(len(rpm)))
		}
	}
	speed := make([]float32, 60)
	for k := range speed {
		speed[k] = float32(100 + k)
	}
	p := filepath.Join(t.TempDir(), "spa-ferrari_296_gt3-3-2026.11.17-22.06.38.ld")
	writeTestFile(t, p, "spa", []testChannel{{"TIME", "s", 10, clock}, {"SPEED", "m/s", 20, speed}, {"RPM", "rpm", 10, rpm}})
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkCut(t, "small", f, 2, 2)
	checkCut(t, "small", f, 1, 2)
	// lap 1 has no SPEED data: the cut leaves that channel out
	cut, res := cutAndOpen(t, f, 1, 1)
	if _, ok := cut.Channel("SPEED"); ok || res.Channels != 2 {
		t.Fatalf("lap 1 without speed data: %d channels", res.Channels)
	}
	if _, err := f.Cut(&bytes.Buffer{}, 0, 0); err == nil {
		t.Fatal("the out-lap is not a full lap and can't be cut")
	}
	var ldx bytes.Buffer
	WriteLDX(&ldx, res.Crossings)
	if !strings.Contains(ldx.String(), `Name="1, id=99" Flags="13" Time="4000000"`) {
		t.Fatalf("ldx: %s", ldx.String())
	}
}

// TestCutRealFiles cuts every full lap with data out of some of the user's ACC files (git-ignored,
// D-160) and reads the cut logs back.
func TestCutRealFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "Telemetry", "Assetto Corsa Competizione", "MoTeC")
	files, _ := filepath.Glob(filepath.Join(dir, "*.ld"))
	if len(files) == 0 {
		t.Skip("no MoTeC files")
	}
	step := max(1, len(files)/40)
	cuts := 0
	for i := 0; i < len(files); i += step {
		f, err := Open(files[i])
		if err != nil {
			t.Fatal(err)
		}
		laps, _ := f.Laps()
		sp, hasSpeed := f.Channel("SPEED")
		for _, l := range laps {
			if !l.Complete || !hasSpeed || sp.Start > l.Start+0.1 || sp.Start+float64(sp.Count)/float64(sp.Hz) < l.Start+l.Time-0.1 {
				continue
			}
			checkCut(t, filepath.Base(files[i]), f, l.N, l.N)
			cuts++
		}
		f.Close()
	}
	t.Logf("%d laps cut and read back", cuts)
}
