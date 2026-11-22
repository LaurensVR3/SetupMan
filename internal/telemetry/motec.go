package telemetry

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MoTeCFileInfo summarises one .ld file in ACC's MoTeC folder for the import picker.
type MoTeCFileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	Modified    time.Time `json:"modified"`
	Track       string    `json:"track"`
	Car         string    `json:"car"`
	HasData     bool      `json:"hasData"`     // true when SPEED/THROTTLE/BRAKE channels contain samples
	LapCount    int       `json:"lapCount"`    // estimated completed laps from .ldx beacons
	DrivingSecs float64   `json:"drivingSecs"` // duration of SPEED channel in seconds
	Imported    bool      `json:"imported"`    // true if already stored in SetupMan
	Note        string    `json:"note,omitempty"`
}

// MoTeCLap is one decoded lap extracted from a MoTeC .ld/.ldx pair.
type MoTeCLap struct {
	LapNumber  int
	LapMs      int
	Valid      bool
	RecordedAt time.Time
	Trace      *Trace
	Stats      Stats
}

// MoTeCParsed is the full result of parsing a MoTeC .ld (and optional .ldx) file.
type MoTeCParsed struct {
	Track      string
	Car        string
	RecordedAt time.Time
	SourceFile string
	Laps       []MoTeCLap
}

type ldChannel struct {
	name    string
	unit    string
	dataPtr uint32
	dataLen uint32
	dtypeA  uint16
	dtype   uint16
	freq    uint16
	shift   int16
	mul     int16
	scale   int16
	dec     int16
}

type ldHeader struct {
	metaPtr  uint32
	dataPtr  uint32
	dateStr  string
	timeStr  string
	driver   string
	vehicle  string
	venue    string
	channels []ldChannel
}

// ErrNoDrivingData is returned when a MoTeC file has channel definitions but 0 driving samples
// (e.g., ACC Telemetry Laps = 0 or no flying lap recorded).
var ErrNoDrivingData = errors.New("this MoTeC file has no driving samples (SPEED, THROTTLE and BRAKE are empty); in ACC set Setup → Electronics → Telemetry Laps > 0 and complete a lap")

// InspectMoTeC reads only the header and channel directory of a .ld file (and paired .ldx)
// so scanning ACC's MoTeC folder is nearly instantaneous.
func InspectMoTeC(ldPath string) (MoTeCFileInfo, error) {
	st, err := os.Stat(ldPath)
	if err != nil {
		return MoTeCFileInfo{}, err
	}
	f, err := os.Open(ldPath)
	if err != nil {
		return MoTeCFileInfo{}, err
	}
	defer f.Close()

	// Read up to the first 64 KB for header + channel metadata directory (usually < 20 KB).
	head := make([]byte, 65536)
	n, _ := f.Read(head)
	head = head[:n]

	info := MoTeCFileInfo{
		Name:     filepath.Base(ldPath),
		Path:     ldPath,
		Size:     st.Size(),
		Modified: st.ModTime(),
	}
	track, car := parseACCFilename(info.Name)

	hdr, err := parseLDHeader(head)
	if err == nil {
		if track == "" {
			track = normalizeTrackID(hdr.venue)
		}
		if car == "" {
			car = normalizeCarID(hdr.vehicle)
		}
		for _, ch := range hdr.channels {
			if strings.EqualFold(ch.name, "SPEED") && ch.dataLen > 0 {
				info.HasData = true
				if ch.freq > 0 {
					info.DrivingSecs = math.Round(float64(ch.dataLen)/float64(ch.freq)*10) / 10
				}
				break
			}
		}
	}
	info.Track = track
	info.Car = car

	// Check paired .ldx for lap beacons.
	ldxPath := strings.TrimSuffix(ldPath, filepath.Ext(ldPath)) + ".ldx"
	if ldxBytes, err := os.ReadFile(ldxPath); err == nil {
		beacons := parseLDXBeacons(ldxBytes)
		if len(beacons) >= 2 {
			info.LapCount = len(beacons) - 1
		}
	}
	if !info.HasData {
		info.Note = "No driving samples (enable Telemetry Laps > 0 in ACC setup and complete a lap)"
	}
	return info, nil
}

// ParseMoTeC decodes an ACC MoTeC .ld file (with optional .ldx beacons) into distance-aligned laps.
func ParseMoTeC(ldBytes, ldxBytes []byte, filename string) (*MoTeCParsed, error) {
	hdr, err := parseLDHeader(ldBytes)
	if err != nil {
		return nil, err
	}
	chMap := map[string]ldChannel{}
	for _, ch := range hdr.channels {
		chMap[strings.ToUpper(ch.name)] = ch
	}

	speedCh, ok := chMap["SPEED"]
	if !ok || speedCh.dataLen < 20 {
		return nil, ErrNoDrivingData
	}

	speedVals, err := readChannel(ldBytes, speedCh)
	if err != nil || len(speedVals) < 20 {
		return nil, ErrNoDrivingData
	}
	speedFreq := float64(speedCh.freq)
	if speedFreq <= 0 {
		speedFreq = 60
	}
	speedMul := 1.0
	unitLower := strings.ToLower(strings.TrimSpace(speedCh.unit))
	if unitLower == "m/s" || unitLower == "mps" {
		speedMul = 3.6
	} else {
		// Auto-detect m/s if max speed looks like m/s (< 105) while car is clearly racing GT3.
		maxV := 0.0
		for _, v := range speedVals {
			if v > maxV {
				maxV = v
			}
		}
		if maxV > 15 && maxV < 105 && unitLower != "km/h" && unitLower != "kph" {
			speedMul = 3.6
		}
	}

	readOpt := func(names ...string) ([]float64, float64, string) {
		for _, name := range names {
			if ch, exists := chMap[name]; exists && ch.dataLen > 0 {
				if vals, err := readChannel(ldBytes, ch); err == nil && len(vals) > 0 {
					f := float64(ch.freq)
					if f <= 0 {
						f = speedFreq
					}
					return vals, f, strings.ToLower(strings.TrimSpace(ch.unit))
				}
			}
		}
		return nil, 0, ""
	}

	throtVals, throtFreq, _ := readOpt("THROTTLE", "GAS", "APPS")
	brakeVals, brakeFreq, _ := readOpt("BRAKE", "BRAKE_POS")
	steerVals, steerFreq, steerUnit := readOpt("STEERANGLE", "STEER", "STEERING")
	gearVals, gearFreq, _ := readOpt("GEAR")
	rpmVals, rpmFreq, _ := readOpt("RPMS", "RPM", "ENG_RPM")
	tcVals, tcFreq, _ := readOpt("TC", "TC_ACT")
	absVals, absFreq, _ := readOpt("ABS", "ABS_ACT")
	glatVals, glatFreq, glatUnit := readOpt("G_LAT", "GLAT", "LAT_G")
	glonVals, glonFreq, glonUnit := readOpt("G_LON", "GLON", "LON_G")
	beaconVals, beaconFreq, _ := readOpt("LAP_BEACON")

	// Detect whether throttle/brake are 0..1 or 0..100.
	throtScale := scaleTo100(throtVals)
	brakeScale := scaleTo100(brakeVals)
	glatScale := gUnitScale(glatUnit, glatVals)
	glonScale := gUnitScale(glonUnit, glonVals)
	steerScale := 1.0
	if steerUnit == "rad" {
		steerScale = 180.0 / math.Pi
	}

	totalDur := float64(len(speedVals)-1) / speedFreq

	track, car := parseACCFilename(filename)
	if track == "" {
		track = normalizeTrackID(hdr.venue)
	}
	if car == "" {
		car = normalizeCarID(hdr.vehicle)
	}
	recordedAt := parseLDTimestamp(hdr.dateStr, hdr.timeStr)
	if recordedAt.IsZero() {
		recordedAt = time.Now()
	}

	// Collect lap boundary timestamps (in seconds).
	beacons := parseLDXBeacons(ldxBytes)
	if len(beacons) < 2 && len(beaconVals) > 0 && beaconFreq > 0 {
		beacons = extractBeaconEdges(beaconVals, beaconFreq)
	}

	distOf := func(t0, t1 float64) float64 {
		i0 := int(math.Max(0, math.Round(t0*speedFreq)))
		i1 := int(math.Min(float64(len(speedVals)), math.Round(t1*speedFreq)))
		if i1 <= i0 {
			return 0
		}
		var sum float64
		for i := i0; i < i1; i++ {
			v := speedVals[i] * speedMul / 3.6
			if v > 0 {
				sum += v
			}
		}
		return sum / speedFreq
	}

	type span struct {
		t0, t1 float64
		dist   float64
	}
	var spans []span
	for i := 0; i+1 < len(beacons); i++ {
		t0, t1 := beacons[i], beacons[i+1]
		if t1-t0 >= 25.0 && t0 >= 0 && t1 <= totalDur+1.0 {
			tEnd := math.Min(t1, totalDur)
			spans = append(spans, span{t0, tEnd, distOf(t0, tEnd)})
		}
	}
	// ACC often closes the .ld file right after the final lap finishes without appending a final
	// .ldx beacon. Include the trailing segment if it covers a complete lap (~85%+ of expected length).
	if len(beacons) >= 1 {
		lastB := beacons[len(beacons)-1]
		if totalDur-lastB >= 25.0 {
			dTrail := distOf(lastB, totalDur)
			expected := TrackLengthHint(track)
			for _, sp := range spans {
				if sp.dist > expected*0.75 {
					expected = sp.dist
				}
			}
			if dTrail >= expected*0.85 {
				spans = append(spans, span{lastB, totalDur, dTrail})
			}
		}
	}
	// Filter out short GP-shortcut / out-lap segments (e.g., 4.5 km Nürburgring GP shortcut
	// between two full 25.2 km Nords24 flying laps) whenever full-length laps are present.
	if len(spans) > 1 {
		maxDist := 0.0
		for _, sp := range spans {
			if sp.dist > maxDist {
				maxDist = sp.dist
			}
		}
		var full []span
		for _, sp := range spans {
			if sp.dist >= maxDist*0.65 {
				full = append(full, sp)
			}
		}
		if len(full) > 0 {
			spans = full
		}
	}
	// Fallback if no lap beacons exist (single outing): use the moving portion if >= 30 s.
	if len(spans) == 0 && totalDur >= 30.0 {
		tStart, tEnd := 0.0, totalDur
		for i, v := range speedVals {
			if v*speedMul > 15 {
				tStart = float64(i) / speedFreq
				break
			}
		}
		for i := len(speedVals) - 1; i >= 0; i-- {
			if speedVals[i]*speedMul > 15 {
				tEnd = float64(i) / speedFreq
				break
			}
		}
		if tEnd-tStart >= 25.0 {
			spans = append(spans, span{tStart, tEnd, distOf(tStart, tEnd)})
		}
	}
	if len(spans) == 0 {
		return nil, errors.New("no completed laps found in this MoTeC file")
	}

	sampleAt := func(vals []float64, freq, t float64) float64 {
		if len(vals) == 0 || freq <= 0 {
			return 0
		}
		pos := t * freq
		idx := int(pos)
		if idx < 0 {
			return vals[0]
		}
		if idx >= len(vals)-1 {
			return vals[len(vals)-1]
		}
		f := pos - float64(idx)
		return vals[idx] + (vals[idx+1]-vals[idx])*f
	}

	var laps []MoTeCLap
	for idx, sp := range spans {
		lapSec := sp.t1 - sp.t0
		lapMs := int(math.Round(lapSec * 1000))
		// Step at 20 Hz (50 ms) over the lap to build RawSamples cleanly.
		step := 0.05
		nSteps := int(math.Ceil(lapSec/step)) + 1
		raw := make([]RawSample, 0, nSteps)
		var dist float64
		prevV := sampleAt(speedVals, speedFreq, sp.t0) * speedMul
		for k := 0; k < nSteps; k++ {
			t := sp.t0 + float64(k)*step
			if t > sp.t1 {
				t = sp.t1
			}
			v := sampleAt(speedVals, speedFreq, t) * speedMul
			if v < 0 {
				v = 0
			}
			if k > 0 {
				dt := step
				if k == nSteps-1 {
					dt = t - (sp.t0 + float64(k-1)*step)
					if dt < 0 {
						dt = 0
					}
				}
				dist += ((prevV + v) / 2.0 / 3.6) * dt
			}
			prevV = v
			g := int(math.Round(sampleAt(gearVals, gearFreq, t)))
			if g < 0 {
				g = 0
			}
			raw = append(raw, RawSample{
				TimeS:    t - sp.t0,
				DistM:    dist,
				SpeedKmh: v,
				Throttle: clamp(sampleAt(throtVals, throtFreq, t)*throtScale, 0, 100),
				Brake:    clamp(sampleAt(brakeVals, brakeFreq, t)*brakeScale, 0, 100),
				SteerDeg: sampleAt(steerVals, steerFreq, t) * steerScale,
				Gear:     g,
				RPM:      int(math.Round(sampleAt(rpmVals, rpmFreq, t))),
				TC:       sampleAt(tcVals, tcFreq, t) > 0.1,
				ABS:      sampleAt(absVals, absFreq, t) > 0.1,
				GLat:     sampleAt(glatVals, glatFreq, t) * glatScale,
				GLon:     sampleAt(glonVals, glonFreq, t) * glonScale,
			})
		}
		if dist < 500 {
			continue
		}
		bins := BinsForTrackLength(dist)
		tr, st := Resample(raw, lapMs, bins)
		if tr == nil {
			continue
		}
		// For circuits with a built-in real-world GPS centerline (Imola, Nürburgring 24h, Spa,
		// Monza, Silverstone, etc.), use it directly since MoTeC .ld files carry no GPS X/Y
		// channels and dead-reckoning from G_LAT drifts over a lap.
		if x, y, _, ok := TrackCenterline(track, tr.N); ok {
			tr.X, tr.Y, tr.HasCoords = x, y, true
		} else {
			ReconstructMap(tr)
		}
		laps = append(laps, MoTeCLap{
			LapNumber:  idx + 1,
			LapMs:      lapMs,
			Valid:      true,
			RecordedAt: recordedAt,
			Trace:      tr,
			Stats:      st,
		})
	}
	if len(laps) == 0 {
		return nil, errors.New("no complete driving laps could be extracted from this MoTeC file")
	}

	return &MoTeCParsed{
		Track:      track,
		Car:        car,
		RecordedAt: recordedAt,
		SourceFile: filepath.Base(filename),
		Laps:       laps,
	}, nil
}

func parseLDHeader(b []byte) (*ldHeader, error) {
	if len(b) < 0x1A0 {
		return nil, errors.New("file is too small to be a MoTeC .ld log")
	}
	magic := binary.LittleEndian.Uint32(b[0:4])
	if magic != 0x40 {
		return nil, fmt.Errorf("not a valid MoTeC .ld file (unexpected header 0x%x)", magic)
	}
	metaPtr := binary.LittleEndian.Uint32(b[8:12])
	dataPtr := binary.LittleEndian.Uint32(b[12:16])
	h := &ldHeader{
		metaPtr: metaPtr,
		dataPtr: dataPtr,
		dateStr: cstr(b, 0x5E, 16),
		timeStr: cstr(b, 0x7E, 16),
		driver:  cstr(b, 0x9E, 64),
		vehicle: cstr(b, 0xDE, 64),
		venue:   cstr(b, 0x15E, 64),
	}

	ptr := metaPtr
	seen := map[uint32]bool{}
	for ptr != 0 && int(ptr)+124 <= len(b) && len(h.channels) < 512 {
		if seen[ptr] {
			break
		}
		seen[ptr] = true
		cb := b[ptr : ptr+124]
		nextPtr := binary.LittleEndian.Uint32(cb[4:8])
		ch := ldChannel{
			dataPtr: binary.LittleEndian.Uint32(cb[8:12]),
			dataLen: binary.LittleEndian.Uint32(cb[12:16]),
			dtypeA:  binary.LittleEndian.Uint16(cb[18:20]),
			dtype:   binary.LittleEndian.Uint16(cb[20:22]),
			freq:    binary.LittleEndian.Uint16(cb[22:24]),
			shift:   int16(binary.LittleEndian.Uint16(cb[24:26])),
			mul:     int16(binary.LittleEndian.Uint16(cb[26:28])),
			scale:   int16(binary.LittleEndian.Uint16(cb[28:30])),
			dec:     int16(binary.LittleEndian.Uint16(cb[30:32])),
			name:    cstr(cb, 32, 32),
			unit:    cstr(cb, 72, 12),
		}
		h.channels = append(h.channels, ch)
		ptr = nextPtr
	}
	return h, nil
}

func readChannel(b []byte, ch ldChannel) ([]float64, error) {
	if ch.dataLen == 0 || ch.dtype == 0 {
		return nil, nil
	}
	width := int(ch.dtype)
	if width != 2 && width != 4 {
		return nil, fmt.Errorf("unsupported channel width %d", width)
	}
	start := int(ch.dataPtr)
	need := int(ch.dataLen) * width
	if start < 0 || start+need > len(b) {
		// Clamp cleanly if file was truncated slightly.
		if start >= len(b) {
			return nil, errors.New("channel data out of range")
		}
		ch.dataLen = uint32((len(b) - start) / width)
	}
	mul := float64(ch.mul)
	if mul == 0 {
		mul = 1
	}
	scale := float64(ch.scale)
	if scale == 0 {
		scale = 1
	}
	pow := math.Pow10(int(ch.dec))
	shift := float64(ch.shift)
	isFloat := ch.dtypeA == 7 || ch.dtypeA == 0

	out := make([]float64, ch.dataLen)
	for i := 0; i < int(ch.dataLen); i++ {
		off := start + i*width
		var raw float64
		switch {
		case width == 4 && isFloat:
			raw = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[off : off+4])))
		case width == 2 && isFloat:
			raw = float16ToFloat64(binary.LittleEndian.Uint16(b[off : off+2]))
		case width == 2:
			raw = float64(int16(binary.LittleEndian.Uint16(b[off : off+2])))
		case width == 4:
			raw = float64(int32(binary.LittleEndian.Uint32(b[off : off+4])))
		}
		if math.IsNaN(raw) || math.IsInf(raw, 0) {
			raw = 0
		}
		out[i] = (raw*mul/scale)/pow + shift
	}
	return out, nil
}

func float16ToFloat64(h uint16) float64 {
	sign := float64(1)
	if (h & 0x8000) != 0 {
		sign = -1
	}
	exp := int((h >> 10) & 0x1F)
	frac := int(h & 0x03FF)
	switch exp {
	case 0:
		return sign * math.Ldexp(float64(frac), -24)
	case 31:
		return 0
	default:
		return sign * math.Ldexp(float64(frac+1024), exp-25)
	}
}

func parseLDXBeacons(ldxBytes []byte) []float64 {
	if len(ldxBytes) == 0 {
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(ldxBytes))
	var out []float64
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || !strings.EqualFold(se.Name.Local, "Marker") {
			continue
		}
		var cls, timeAttr string
		for _, a := range se.Attr {
			switch strings.ToLower(a.Name.Local) {
			case "classname":
				cls = a.Value
			case "time":
				timeAttr = a.Value
			}
		}
		if cls != "" && !strings.EqualFold(cls, "BCN") {
			continue
		}
		if us, err := strconv.ParseFloat(strings.TrimSpace(timeAttr), 64); err == nil && us > 0 {
			sec := us / 1e6
			if len(out) == 0 || sec-out[len(out)-1] > 1.0 {
				out = append(out, sec)
			}
		}
	}
	sort.Float64s(out)
	return out
}

func extractBeaconEdges(vals []float64, freq float64) []float64 {
	var out []float64
	for i := 1; i < len(vals); i++ {
		if vals[i] > 0.5 && vals[i-1] <= 0.5 {
			t := float64(i) / freq
			if len(out) == 0 || t-out[len(out)-1] > 15.0 {
				out = append(out, t)
			}
		}
	}
	return out
}

func scaleTo100(vals []float64) float64 {
	maxV := 0.0
	for _, v := range vals {
		if v > maxV {
			maxV = v
		}
	}
	if maxV > 0 && maxV <= 1.05 {
		return 100.0
	}
	return 1.0
}

func gUnitScale(unit string, vals []float64) float64 {
	if strings.Contains(unit, "m/s") {
		return 1.0 / 9.80665
	}
	maxV := 0.0
	for _, v := range vals {
		if a := math.Abs(v); a > maxV {
			maxV = a
		}
	}
	if maxV > 6.5 {
		return 1.0 / 9.80665
	}
	return 1.0
}

func cstr(b []byte, off, maxLen int) string {
	if off >= len(b) {
		return ""
	}
	end := off + maxLen
	if end > len(b) {
		end = len(b)
	}
	s := b[off:end]
	if i := bytes.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(string(s))
}

// ACC MoTeC filenames look like: nurburgring_24h-mclaren_720s_gt3_evo-2-2026.11.21-13.37.40.ld
var accFileRe = regexp.MustCompile(`^([a-z0-9_]+)-([a-z0-9_]+)-\d+-\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2}`)

func parseACCFilename(name string) (track, car string) {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if m := accFileRe.FindStringSubmatch(base); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

func normalizeTrackID(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func normalizeCarID(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func parseLDTimestamp(d, t string) time.Time {
	s := strings.TrimSpace(d + " " + t)
	for _, layout := range []string{"02/01/2006 15:04:05", "2006-01-02 15:04:05", "01/02/2006 15:04:05"} {
		if ts, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return ts
		}
	}
	return time.Time{}
}
