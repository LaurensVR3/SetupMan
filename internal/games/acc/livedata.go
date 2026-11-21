package acc

import (
	"encoding/binary"
	"log"
	"math"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"setupman/internal/game"
	"setupman/internal/games/gamekit"
)

// Live data comes from ACC's shared memory (spec D-38, D-39, D-44, D-48). Layout from Kunos'
// "ACC Shared Memory Documentation": structs packed to 4 bytes, wchar_t = 2 bytes.
// Offsets were worked out from that layout and still have to be checked on a real rig (D-39);
// every value is checked for plausibility and marked unknown when it doesn't make sense.
//
//	acpmf_physics:  fuel @12, speedKmh @28, wheelsPressure[4] @88, airTemp @288, roadTemp @292
//	acpmf_graphics: status @4 (0 off, 1 replay, 2 live, 3 pause), session @8,
//	                completedLaps @132, iLastTime @144, sessionTimeLeft @152, isInPit @160,
//	                numberOfLaps @172, normalizedCarPosition @248, isInPitLane @1236,
//	                TC @1268, EngineMap @1276, ABS @1280, rainTyres @1316, isValidLap @1408,
//	                missingMandatoryPits @1484, mfdFuelToAdd @1536, mfdTyrePressure LF RF LR RR @1540…1552
//	                DriverStintTimeLeft @1312
//	acpmf_static:   carModel wchar[33] @68, track wchar[33] @134, maxFuel @416
const (
	physicsMapName  = `Local\acpmf_physics`
	graphicsMapName = `Local\acpmf_graphics`
	staticMapName   = `Local\acpmf_static`

	physicsReadSize  = 568
	graphicsReadSize = 1556
	staticReadSize   = 420

	offFuel      = 12
	offSpeedKmh  = 28
	offPressures = 88
	offAirTemp   = 288
	offRoadTemp  = 292
	offBrakeBias = 564 // after tyreContactHeading[4][3] @516 (Kunos' physics page)

	offStatus        = 4
	offSession       = 8
	offCompletedLaps = 132
	offLastLapMs     = 144
	offTimeLeft      = 152
	offSessionLaps   = 172
	offLapProgress   = 248
	offRainTyres     = 1316
	offStintLeft     = 1312
	offStopsLeft     = 1484
	offPitFuel       = 1536
	offPitPressures  = 1540
	offIsInPit       = 160
	offIsInPitLane   = 1236
	offTC            = 1268
	offEngineMap     = 1276
	offABS           = 1280
	offIsValidLap    = 1408

	offCarModel = 68
	offTrack    = 134
	offMaxFuel  = 416

	statusLive        = 2
	drivingSpeedKmh   = 5                // faster than this counts as moving
	stationaryTimeout = 10 * time.Second // standing still this long = no longer driving
	gameCheckEvery    = 5 * time.Second
	sampleEvery       = 1 * time.Second
)

// sample is one reading of the live data.
type sample struct {
	ok     bool // shared memory could be read
	status int32
	inPit  bool
	speed  float32
	t      game.Telemetry
}

var sessionNames = map[int32]string{0: "practice", 1: "qualifying", 2: "race", 3: "hotlap", 4: "time attack", 7: "hotstint", 8: "superpole"}

// layout says where a game keeps the values whose place differs between ACC and Assetto Corsa.
// The first part of every page (status, session, laps, lap times, fuel, speed, pressures,
// temperatures, car, track, tank) is the same in both: ACC's live data started as a copy of AC's.
// 0 = the game doesn't have that value.
type layout struct {
	graphicsReadSize                int
	pitLane, validLap               int
	tc, engineMap, abs              int
	rainTyres, stintLeft, stopsLeft int
	pitFuel, pitPressures           int
	sessions                        map[int32]string
}

var accLayout = layout{
	graphicsReadSize: graphicsReadSize,
	pitLane:          offIsInPitLane, validLap: offIsValidLap,
	tc: offTC, engineMap: offEngineMap, abs: offABS,
	rainTyres: offRainTyres, stintLeft: offStintLeft, stopsLeft: offStopsLeft,
	pitFuel: offPitFuel, pitPressures: offPitPressures,
	sessions: sessionNames,
}

// acLayout is Assetto Corsa's graphics page, from Kunos' "Assetto Corsa Shared Memory" documentation:
// after normalizedCarPosition @248 come carCoordinates[3] @252, penaltyTime @264, flag @268,
// idealLineOn @272 and isInPitLane @276. AC has no lap-valid flag, no electronics positions as
// clicks, no rain tyres, stint or mandatory-stop values and no pit menu (D-137). To be checked on the rig.
var acLayout = layout{
	graphicsReadSize: 280,
	pitLane:          276,
	sessions:         map[int32]string{0: "practice", 1: "qualifying", 2: "race", 3: "hotlap", 4: "time attack", 5: "drift", 6: "drag"},
}

func u32(b []byte, off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }
func i32(b []byte, off int) int32  { return int32(u32(b, off)) }
func f32(b []byte, off int) float64 {
	return float64(math.Float32frombits(u32(b, off)))
}

// wstr reads a zero-terminated UTF-16 string of at most n characters.
func wstr(b []byte, off, n int) string {
	var u []uint16
	for i := 0; i < n && off+2*i+1 < len(b); i++ {
		c := binary.LittleEndian.Uint16(b[off+2*i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// parse turns ACC's three shared-memory pages into a sample.
func parse(phys, graph, stat []byte) sample { return accLayout.parse(phys, graph, stat) }

// parse turns the three shared-memory pages into a sample.
func (lay layout) parse(phys, graph, stat []byte) sample {
	if len(phys) < physicsReadSize || len(graph) < lay.graphicsReadSize {
		return sample{}
	}
	s := sample{ok: true, status: i32(graph, offStatus), inPit: u32(graph, offIsInPit) != 0, speed: float32(f32(phys, offSpeedKmh))}
	t := game.Telemetry{
		Live:          s.status == statusLive,
		Session:       lay.sessions[i32(graph, offSession)],
		CompletedLaps: int(i32(graph, offCompletedLaps)),
		LastLapMs:     int(i32(graph, offLastLapMs)),
		InPit:         s.inPit,
		Fuel:          f32(phys, offFuel),
		TC:            -1, ABS: -1, Map: -1,
	}
	if lay.pitLane > 0 {
		if v := u32(graph, lay.pitLane); v <= 1 {
			t.InPitLane, t.PitLaneKnown = v == 1, true
		}
	}
	if lay.validLap > 0 {
		if v := u32(graph, lay.validLap); v <= 1 {
			t.ValidLap, t.ValidKnown = v == 1, true
		}
	}
	if air, road := f32(phys, offAirTemp), f32(phys, offRoadTemp); air > -30 && air < 60 && road > -30 && road < 80 && (air != 0 || road != 0) {
		t.AirTemp, t.RoadTemp, t.TempsKnown = air, road, true
	}
	if b := f32(phys, offBrakeBias); b > 0 && b < 100 {
		t.BrakeBias, t.BiasKnown = b, true
	}
	for _, e := range []struct {
		dst *int
		off int
	}{{&t.TC, lay.tc}, {&t.Map, lay.engineMap}, {&t.ABS, lay.abs}} {
		if e.off == 0 {
			continue
		}
		if v := i32(graph, e.off); v >= 0 && v <= 20 {
			*e.dst = int(v)
		}
	}
	if t.Fuel < 0 || t.Fuel > 500 || math.IsNaN(t.Fuel) {
		t.Fuel = 0
	}
	if t.LastLapMs < 0 || t.LastLapMs > 3_600_000 {
		t.LastLapMs = 0
	}
	lay.pitAdvice(&t, phys, graph)
	if len(stat) >= staticReadSize {
		t.Car, t.Track = wstr(stat, offCarModel, 33), wstr(stat, offTrack, 33)
		if v := f32(stat, offMaxFuel); !math.IsNaN(v) && v > 10 && v < 500 {
			t.TankSize, t.TankKnown = v, true
		}
	}
	s.t = t
	return s
}

// pitAdvice reads the values the pit stop advice needs (D-124); each is checked for plausibility.
func (lay layout) pitAdvice(t *game.Telemetry, phys, graph []byte) {
	ok := true
	for i := 0; i < 4; i++ {
		p := f32(phys, offPressures+4*i)
		if math.IsNaN(p) || p < 10 || p > 50 {
			ok = false
		}
		t.Pressures[i] = p
	}
	if t.PressuresKnown = ok; !ok {
		t.Pressures = [4]float64{}
	}
	if lay.rainTyres > 0 {
		if v := u32(graph, lay.rainTyres); v <= 1 {
			t.WetTyres, t.WetKnown = v == 1, true
		}
	}
	if v := f32(graph, offTimeLeft); !math.IsNaN(v) && v > -86_400_000 && v < 86_400_000 {
		t.TimeLeftMs, t.TimeLeftKnown = v, true
	}
	if v := i32(graph, offSessionLaps); v > 0 && v < 1000 {
		t.SessionLaps = int(v)
	}
	if v := f32(graph, offLapProgress); !math.IsNaN(v) && v >= 0 && v <= 1 {
		t.LapProgress, t.ProgressKnown = v, true
	}
	if lay.stintLeft > 0 {
		if v := i32(graph, lay.stintLeft); v >= 0 && v < 86_400_000 { // -1 = no stint limit
			t.StintLeftMs, t.StintKnown = int(v), true
		}
	}
	if lay.stopsLeft > 0 {
		if v := i32(graph, lay.stopsLeft); v >= 0 && v <= 10 {
			t.StopsLeft, t.StopsKnown = int(v), true
		}
	}
	if lay.pitFuel == 0 || lay.pitPressures == 0 {
		return // no pit menu in the live data
	}
	fuel := f32(graph, lay.pitFuel)
	menu := !math.IsNaN(fuel) && fuel >= 0 && fuel <= 200
	for i := 0; i < 4; i++ {
		p := f32(graph, lay.pitPressures+4*i)
		if math.IsNaN(p) || p < 15 || p > 40 {
			menu = false
		}
		t.PitPressures[i] = p
	}
	if t.PitMenuKnown = menu; menu {
		t.PitFuel = fuel
	} else {
		t.PitPressures = [4]float64{}
	}
}

// driveState turns samples into a driving / not-driving decision.
type driveState struct {
	driving   bool
	lastMoved time.Time
}

func (d *driveState) update(s sample, now time.Time) game.LiveState {
	if !s.ok {
		d.driving = false
		return game.LiveUnknown
	}
	switch {
	case s.status != statusLive || s.inPit:
		d.driving = false
	case s.speed > drivingSpeedKmh:
		d.driving = true
		d.lastMoved = now
	case d.driving && now.Sub(d.lastMoved) >= stationaryTimeout:
		d.driving = false // stopped on track for a while (e.g. after a spin, or parked)
	}
	if d.driving {
		return game.LiveDriving
	}
	return game.LiveNotDriving
}

// liveReader watches ACC in the background: once per second while ACC runs,
// otherwise a cheap check every few seconds.
type liveReader struct {
	lay   layout
	exes  []string // the game's program file names, e.g. "AC2-Win64-Shipping.exe"
	once  sync.Once
	state atomic.Value // game.LiveState
	tele  atomic.Value // *game.Telemetry (nil pointer = game not running)
}

func newLiveReader() *liveReader { return newLiveReaderFor(accLayout, accProcess) }

func newLiveReaderFor(lay layout, exes ...string) *liveReader {
	r := &liveReader{lay: lay, exes: exes}
	r.state.Store(game.LiveUnknown)
	r.tele.Store((*game.Telemetry)(nil))
	return r
}

func (r *liveReader) start() { r.once.Do(func() { go r.loop() }) }

// State returns the latest state; the background loop starts on first use.
func (r *liveReader) State() game.LiveState {
	r.start()
	return r.state.Load().(game.LiveState)
}

// Telemetry returns the latest snapshot (false when ACC isn't running).
func (r *liveReader) Telemetry() (game.Telemetry, bool) {
	r.start()
	t := r.tele.Load().(*game.Telemetry)
	if t == nil {
		return game.Telemetry{}, false
	}
	return *t, true
}

// accProcess is ACC's game program (user-confirmed, Q-G2; acc.exe is only the launcher). Its shared memory alone is not proof that ACC runs: tools such as
// SimHub keep it open, and Assetto Corsa uses the same names.
const accProcess = "AC2-Win64-Shipping.exe"

// processRunning: whether the game's program runs. The process list is shared by all games and
// refreshed at most every few seconds; if it can't be read, the shared memory decides.
func processRunning(exes ...string) bool {
	running, known := gamekit.Check(exes...)
	return running || !known
}

// loop: check whether the game's program runs (shared process list, see processRunning); while it
// does, read the shared memory once per second.
func (r *liveReader) loop() {
	var d driveState
	for {
		time.Sleep(r.step(&d))
	}
}

// step reads the game once and returns how long to wait before the next read. An unexpected
// error is logged and treated as "game not readable", so the reader keeps running.
func (r *liveReader) step(d *driveState) (wait time.Duration) {
	wait = gameCheckEvery
	defer func() {
		if p := recover(); p != nil {
			log.Printf("reading %s live data: unexpected error: %v\n%s", r.exes[0], p, debug.Stack())
			r.state.Store(game.LiveUnknown)
			r.tele.Store((*game.Telemetry)(nil))
			*d = driveState{}
		}
	}()
	if !processRunning(r.exes...) {
		r.state.Store(game.LiveUnknown)
		r.tele.Store((*game.Telemetry)(nil))
		*d = driveState{}
		return gameCheckEvery
	}
	s := readSample(r.lay)
	if !s.ok {
		r.state.Store(game.LiveUnknown)
		r.tele.Store((*game.Telemetry)(nil))
		*d = driveState{}
		return gameCheckEvery
	}
	r.state.Store(d.update(s, time.Now()))
	t := s.t
	r.tele.Store(&t)
	return sampleEvery
}

func readSample(lay layout) sample {
	g, err := readShared(graphicsMapName, lay.graphicsReadSize)
	if err != nil {
		return sample{}
	}
	p, err := readShared(physicsMapName, physicsReadSize)
	if err != nil {
		return sample{}
	}
	st, _ := readShared(staticMapName, staticReadSize) // optional
	return lay.parse(p, g, st)
}

// ACLive reads Assetto Corsa's live data. AC uses the same shared memory names as ACC (ACC's
// live data started as a copy of AC's), with its own layout for the graphics page (acLayout, D-137).
type ACLive struct{ r *liveReader }

// NewACLive watches Assetto Corsa, whose program file is one of exes.
func NewACLive(exes ...string) *ACLive { return &ACLive{r: newLiveReaderFor(acLayout, exes...)} }

// Live reports whether the player is driving.
func (s *ACLive) Live() game.LiveState { return s.r.State() }

// Telemetry returns the latest snapshot (false when AC isn't running).
func (s *ACLive) Telemetry() (game.Telemetry, bool) { return s.r.Telemetry() }

// StartedAt returns when AC's program started (D-147).
func (s *ACLive) StartedAt() (time.Time, bool) { return gamekit.StartedAt(s.r.exes...) }
