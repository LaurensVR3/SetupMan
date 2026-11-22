package telemetry

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

func TestNords24SamplesAndCompare(t *testing.T) {
	refMeta, cmpMeta, refTr, cmpTr := BuildNords24Samples()
	if refTr.N != 1200 || cmpTr.N != 1200 {
		t.Fatalf("expected 1200 bins, got ref=%d cmp=%d", refTr.N, cmpTr.N)
	}
	if !refTr.HasCoords || len(refTr.X) != 1200 || len(refTr.Y) != 1200 {
		t.Fatalf("expected track coordinates on sample lap")
	}

	// Verify gzip round-trip.
	gz, err := EncodeTrace(refTr)
	if err != nil {
		t.Fatalf("EncodeTrace: %v", err)
	}
	if len(gz) > 100_000 {
		t.Errorf("encoded trace unexpectedly large: %d bytes", len(gz))
	}
	decoded, err := DecodeTrace(gz)
	if err != nil {
		t.Fatalf("DecodeTrace: %v", err)
	}
	if decoded.N != refTr.N || decoded.SpeedKmh[500] != refTr.SpeedKmh[500] {
		t.Fatalf("decoded trace mismatch")
	}

	cmp := Compare(refMeta, cmpMeta, refTr, cmpTr)
	if len(cmp.Sections) != 15 {
		t.Fatalf("expected 15 Nords24 sections, got %d", len(cmp.Sections))
	}
	if !cmp.HasMap || len(cmp.MapX) != 1200 {
		t.Fatalf("expected comparison map")
	}
	if len(cmp.TopLosses) == 0 {
		t.Errorf("expected at least one losing section in Nords24 sample")
	}
	if len(cmp.TopGains) == 0 {
		t.Errorf("expected at least one gaining section in Nords24 sample")
	}
	if math.Abs(cmp.TotalDeltaS-2.46) > 0.05 {
		t.Errorf("expected total delta ~+2.46s, got %.3f", cmp.TotalDeltaS)
	}
	// Final delta point should match total lap time delta closely.
	lastDelta := float64(cmp.DeltaS[len(cmp.DeltaS)-1])
	if math.Abs(lastDelta-cmp.TotalDeltaS) > 0.25 {
		t.Errorf("final cumulative delta %.3f != total delta %.3f", lastDelta, cmp.TotalDeltaS)
	}
}

func TestParseMoTeCSyntheticLD(t *testing.T) {
	ld := buildSyntheticLD(t, true)
	ldx := []byte(`<?xml version="1.0"?><LDXFile><Layers><Layer><MarkerBlock><MarkerGroup Name="Beacons"><Marker ClassName="BCN" Time="0" /><Marker ClassName="BCN" Time="5000000" /><Marker ClassName="BCN" Time="95000000" /></MarkerGroup></MarkerBlock></Layer></Layers></LDXFile>`)

	parsed, err := ParseMoTeC(ld, ldx, "nurburgring_24h-mclaren_720s_gt3_evo-1-2026.11.21-14.00.00.ld")
	if err != nil {
		t.Fatalf("ParseMoTeC: %v", err)
	}
	if parsed.Track != "nurburgring_24h" || parsed.Car != "mclaren_720s_gt3_evo" {
		t.Errorf("unexpected track/car: %q / %q", parsed.Track, parsed.Car)
	}
	if len(parsed.Laps) != 1 {
		t.Fatalf("expected 1 completed lap (90s), got %d", len(parsed.Laps))
	}
	lap := parsed.Laps[0]
	if lap.LapMs != 90000 {
		t.Errorf("expected 90000ms lap, got %d", lap.LapMs)
	}
	if lap.Stats.MaxSpeedKmh < 190 || lap.Stats.MaxSpeedKmh > 230 {
		t.Errorf("unexpected max speed: %.1f", lap.Stats.MaxSpeedKmh)
	}
	if !lap.Trace.HasCoords {
		t.Errorf("expected reconstructed track map coordinates")
	}
}

func TestParseMoTeCEmptyChannels(t *testing.T) {
	ld := buildSyntheticLD(t, false)
	_, err := ParseMoTeC(ld, nil, "nurburgring_24h-mclaren_720s_gt3_evo-1-2026.11.21-14.00.00.ld")
	if err != ErrNoDrivingData {
		t.Fatalf("expected ErrNoDrivingData, got %v", err)
	}
}

func TestFlagSliceBackwardCompat(t *testing.T) {
	// Encodes as JSON number array (not base64 string).
	f := FlagSlice{0, 1, 0, 1}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[0,1,0,1]" {
		t.Fatalf("expected [0,1,0,1], got %s", string(b))
	}
	// Decodes both JSON number arrays and legacy base64 strings.
	var decArr, decLegacy FlagSlice
	if err := json.Unmarshal(b, &decArr); err != nil || len(decArr) != 4 || decArr[1] != 1 {
		t.Fatalf("unmarshal array failed: %v %v", decArr, err)
	}
	if err := json.Unmarshal([]byte(`"AAEBAA=="`), &decLegacy); err != nil || len(decLegacy) != 4 || decLegacy[2] != 1 {
		t.Fatalf("unmarshal legacy base64 failed: %v %v", decLegacy, err)
	}
}

func TestTrackCenterlinesAndImola(t *testing.T) {
	tracks := KnownTrackMaps()
	if len(tracks) < 17 {
		t.Fatalf("expected at least 17 built-in ACC track maps, got %d", len(tracks))
	}
	for _, id := range tracks {
		x, y, curv, ok := TrackCenterline(id, 600)
		if !ok || len(x) != 600 || len(y) != 600 || len(curv) != 600 {
			t.Fatalf("TrackCenterline(%q) failed: ok=%v len=%d", id, ok, len(x))
		}
		// Verify closed loop (start and end meet closely).
		gap := math.Hypot(float64(x[0]-x[599]), float64(y[0]-y[599]))
		if gap > 25 {
			t.Errorf("TrackCenterline(%q) start/end gap too large: %.1f m", id, gap)
		}
		// Verify named sections span [0, 1] continuously.
		secs := SectionsForTrack(id)
		if len(secs) < 5 {
			t.Errorf("expected named sections for %q, got %d", id, len(secs))
		}
		if secs[0].Start != 0 || secs[len(secs)-1].End != 1 {
			t.Errorf("sections for %q do not cover [0,1]: start=%.3f end=%.3f", id, secs[0].Start, secs[len(secs)-1].End)
		}
	}

	// Specifically verify Imola (9 named sections + MoTeC auto-attaches GPS centerline).
	imolaSecs := SectionsForTrack("imola")
	if len(imolaSecs) != 9 || imolaSecs[1].Name != "Tamburello (T2–T4)" || imolaSecs[7].Name != "Rivazza 1 & 2 (T17–T18)" {
		t.Fatalf("unexpected Imola sections: %+v", imolaSecs)
	}
	ld := buildSyntheticLD(t, true)
	ldx := []byte(`<?xml version="1.0"?><LDXFile><Layers><Layer><MarkerBlock><MarkerGroup Name="Beacons"><Marker ClassName="BCN" Time="5000000" /><Marker ClassName="BCN" Time="95000000" /></MarkerGroup></MarkerBlock></Layer></Layers></LDXFile>`)
	parsed, err := ParseMoTeC(ld, ldx, "imola-mclaren_720s_gt3_evo-1-2026.11.22-15.40.00.ld")
	if err != nil {
		t.Fatalf("ParseMoTeC Imola: %v", err)
	}
	if parsed.Track != "imola" || len(parsed.Laps) != 1 || !parsed.Laps[0].Trace.HasCoords {
		t.Fatalf("expected Imola MoTeC lap with GPS coordinates, got track=%q laps=%d", parsed.Track, len(parsed.Laps))
	}
}

func buildSyntheticLD(t *testing.T, withData bool) []byte {
	t.Helper()
	type chSpec struct {
		name string
		unit string
		freq uint16
		vals []float32
	}
	const durSec = 100
	makeSamples := func(freq int, fn func(sec float64) float32) []float32 {
		if !withData {
			return nil
		}
		out := make([]float32, durSec*freq)
		for i := range out {
			out[i] = fn(float64(i) / float64(freq))
		}
		return out
	}

	specs := []chSpec{
		{"SPEED", "m/s", 20, makeSamples(20, func(s float64) float32 { return float32(45 + 15*math.Sin(s*0.2)) })},
		{"THROTTLE", "%", 20, makeSamples(20, func(s float64) float32 {
			if math.Sin(s*0.2) >= 0 {
				return 100
			}
			return 0
		})},
		{"BRAKE", "%", 20, makeSamples(20, func(s float64) float32 {
			if math.Sin(s*0.2) < -0.3 {
				return 80
			}
			return 0
		})},
		{"STEERANGLE", "deg", 20, makeSamples(20, func(s float64) float32 { return float32(25 * math.Cos(s*0.2)) })},
		{"GEAR", "no", 20, makeSamples(20, func(s float64) float32 { return 4 })},
		{"RPMS", "1/min", 20, makeSamples(20, func(s float64) float32 { return 6800 })},
		{"G_LAT", "m/s2", 20, makeSamples(20, func(s float64) float32 { return float32(9.8 * math.Sin(s*0.1)) })},
	}

	const headerSize = 0x200
	const chanSize = 124
	metaStart := uint32(headerSize)
	dataStart := metaStart + uint32(len(specs)*chanSize)

	var dataBuf bytes.Buffer
	ptrs := make([]uint32, len(specs))
	for i, sp := range specs {
		ptrs[i] = dataStart + uint32(dataBuf.Len())
		for _, v := range sp.vals {
			_ = binary.Write(&dataBuf, binary.LittleEndian, v)
		}
	}

	buf := make([]byte, int(dataStart)+dataBuf.Len())
	binary.LittleEndian.PutUint32(buf[0:4], 0x40)
	binary.LittleEndian.PutUint32(buf[8:12], metaStart)
	binary.LittleEndian.PutUint32(buf[12:16], dataStart)
	copy(buf[0x5E:], "21/11/2026")
	copy(buf[0x7E:], "14:00:00")
	copy(buf[0xDE:], "McLaren 720S GT3 Evo")
	copy(buf[0x15E:], "nurburgring_24h")

	for i, sp := range specs {
		off := int(metaStart) + i*chanSize
		var prev, next uint32
		if i > 0 {
			prev = metaStart + uint32((i-1)*chanSize)
		}
		if i+1 < len(specs) {
			next = metaStart + uint32((i+1)*chanSize)
		}
		binary.LittleEndian.PutUint32(buf[off:off+4], prev)
		binary.LittleEndian.PutUint32(buf[off+4:off+8], next)
		binary.LittleEndian.PutUint32(buf[off+8:off+12], ptrs[i])
		binary.LittleEndian.PutUint32(buf[off+12:off+16], uint32(len(sp.vals)))
		binary.LittleEndian.PutUint16(buf[off+18:off+20], 7) // float
		binary.LittleEndian.PutUint16(buf[off+20:off+22], 4) // 4 bytes
		binary.LittleEndian.PutUint16(buf[off+22:off+24], sp.freq)
		binary.LittleEndian.PutUint16(buf[off+26:off+28], 1) // mul
		binary.LittleEndian.PutUint16(buf[off+28:off+30], 1) // scale
		copy(buf[off+32:off+64], sp.name)
		copy(buf[off+72:off+84], sp.unit)
	}
	copy(buf[dataStart:], dataBuf.Bytes())
	return buf
}
