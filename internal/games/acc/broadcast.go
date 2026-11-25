package acc

// ACC's broadcasting connection (spec D-198): the UDP connection timing and overlay tools use.
// SetupMan connects only during a timed race, to know where the overall leader is: a timed race
// ends when the leader crosses the line after the clock has run out, and every other car finishes
// at its next crossing. Shared memory only describes the player's own car.
//
// The connection is set up in Documents\Assetto Corsa Competizione\Config\broadcasting.json
// (port and password). SetupMan only reads that file, it never changes it.
// Protocol: Kunos' broadcasting SDK (ksBroadcastingNetwork), version 4, little-endian.

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf16"

	"setupman/internal/game"
)

const (
	bcProtocol   = 4
	bcIntervalMs = 1000             // how often ACC sends the cars (the live reader reads once a second)
	bcRetry      = 30 * time.Second // wait before connecting again after a failure
	bcStale      = 5 * time.Second  // a car (or the whole connection) not heard from for this long is ignored
)

// Message types (SDK: OutboundMessageTypes / InboundMessageTypes).
const (
	bcRegister        = 1
	bcUnregister      = 9
	bcRequestEntries  = 10
	bcRegistration    = 1
	bcRealtimeUpdate  = 2
	bcRealtimeCar     = 3
	bcNoLapTime       = math.MaxInt32
	bcDisplayName     = "SetupMan"
	bcConfigFileName  = "broadcasting.json"
	bcLoopbackAddress = "127.0.0.1"
)

// bcCar is what SetupMan keeps of one car.
type bcCar struct {
	position  int     // overall race position (1 = leader, 0 = not known yet)
	laps      int     // laps completed
	spline    float64 // how far round the lap, 0 … 1
	lastLapMs int     // last lap, when it was a clean racing lap (not an out-lap or in-lap)
	bestLapMs int     // best lap of the session
	seen      time.Time
}

// broadcast is the connection; it runs only while wanted (see want).
type broadcast struct {
	mu      sync.Mutex
	conn    *net.UDPConn // nil = not connected
	connID  int32
	focused int // the car the game shows: the player's while driving (-1 = unknown)
	cars    map[uint16]bcCar
	heard   time.Time // last message from ACC
	lastTry time.Time
	logged  string        // last problem logged (each problem is logged once)
	docs    func() string // the Documents folder (tests use their own)

	track leaderTrack
}

// leaderTrack remembers each car's laps at the last reading before the clock ran out, so a lap
// completed after that is the leader's finishing lap.
type leaderTrack struct {
	laps map[uint16]int // nil = not seen with time on the clock
}

func newBroadcast() *broadcast { return &broadcast{focused: -1, docs: documentsDir} }

// want connects when needed (a timed race) and disconnects otherwise. Called once a second by the
// live reader; it does nothing heavy.
func (b *broadcast) want(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !on {
		b.closeLocked()
		b.track = leaderTrack{}
		return
	}
	if b.conn != nil || time.Since(b.lastTry) < bcRetry {
		return
	}
	b.lastTry = time.Now()
	port, password, err := readBroadcastConfig(b.docs())
	if err != nil {
		b.logOnce("ACC broadcasting connection not available: " + err.Error())
		return
	}
	addr := &net.UDPAddr{IP: net.ParseIP(bcLoopbackAddress), Port: port}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		b.logOnce("ACC broadcasting connection: " + err.Error())
		return
	}
	if _, err := conn.Write(registerPacket(password)); err != nil {
		conn.Close()
		b.logOnce("ACC broadcasting connection: " + err.Error())
		return
	}
	b.conn, b.connID, b.cars, b.focused = conn, -1, map[uint16]bcCar{}, -1
	go b.read(conn)
}

func (b *broadcast) logOnce(msg string) {
	if msg != b.logged {
		log.Print(msg)
		b.logged = msg
	}
}

// closeLocked says goodbye to ACC and closes the connection (b.mu held).
func (b *broadcast) closeLocked() {
	if b.conn == nil {
		return
	}
	if b.connID >= 0 {
		b.conn.Write(idPacket(bcUnregister, b.connID))
	}
	b.conn.Close()
	b.conn, b.cars = nil, nil
}

// read handles ACC's messages until the connection is closed or ACC stays silent.
func (b *broadcast) read(conn *net.UDPConn) {
	buf := make([]byte, 64*1024)
	for {
		conn.SetReadDeadline(time.Now().Add(bcStale))
		n, err := conn.Read(buf)
		b.mu.Lock()
		if b.conn != conn { // closed by want(false)
			b.mu.Unlock()
			return
		}
		if err != nil {
			// No answer (ACC not listening, wrong port or password) or ACC went quiet: try again later.
			if b.connID < 0 {
				b.logOnce("ACC broadcasting connection: no answer from ACC on port " + strconv.Itoa(conn.RemoteAddr().(*net.UDPAddr).Port))
			}
			b.closeLocked()
			b.mu.Unlock()
			return
		}
		b.handle(buf[:n], time.Now())
		b.mu.Unlock()
	}
}

// handle reads one message from ACC (b.mu held).
func (b *broadcast) handle(msg []byte, now time.Time) {
	r := &bcReader{b: msg}
	switch r.u8() {
	case bcRegistration:
		id, ok, _, errMsg := r.i32(), r.u8() == 1, r.u8(), r.str()
		if r.bad || !ok {
			b.logOnce("ACC broadcasting connection refused: " + errMsg)
			return
		}
		b.connID, b.heard = id, now
		b.logOnce("ACC broadcasting connection open")
		b.conn.Write(idPacket(bcRequestEntries, id))
	case bcRealtimeUpdate:
		r.u16() // event index
		r.u16() // session index
		r.u8()  // session type
		r.u8()  // session phase
		r.f32() // session time
		r.f32() // session end time
		focused := r.i32()
		if !r.bad {
			b.focused, b.heard = int(focused), now
		}
	case bcRealtimeCar:
		idx, c, ok := parseCarUpdate(r)
		if ok && b.cars != nil {
			c.seen = now
			b.cars[idx] = c
			b.heard = now
		}
	}
}

// parseCarUpdate reads a REALTIME_CAR_UPDATE after its type byte.
func parseCarUpdate(r *bcReader) (uint16, bcCar, bool) {
	idx := r.u16()
	r.u16() // driver index
	r.u8()  // driver count
	r.u8()  // gear
	r.f32() // world x
	r.f32() // world y
	r.f32() // yaw
	r.u8()  // car location
	r.u16() // speed
	c := bcCar{position: int(r.u16())}
	r.u16() // cup position
	r.u16() // track position
	c.spline = r.f32()
	c.laps = int(r.u16())
	r.i32() // delta
	best := r.lap()
	last := r.lap()
	r.lap() // current lap
	if r.bad || math.IsNaN(c.spline) {
		return 0, bcCar{}, false
	}
	c.spline = math.Min(1, math.Max(0, c.spline))
	if best.ms > 0 {
		c.bestLapMs = best.ms
	}
	if last.ms > 0 && !last.outLap && !last.inLap {
		c.lastLapMs = last.ms
	}
	return idx, c, true
}

// fill adds the race leader to a telemetry snapshot (D-198).
func (b *broadcast) fill(t *game.Telemetry, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil || now.Sub(b.heard) > bcStale {
		return
	}
	fillLeader(t, b.cars, b.focused, &b.track, now)
}

// fillLeader finds the overall leader among the cars heard from recently.
func fillLeader(t *game.Telemetry, cars map[uint16]bcCar, focused int, lt *leaderTrack, now time.Time) {
	leader, found := uint16(0), false
	for idx, c := range cars {
		if c.position == 1 && now.Sub(c.seen) <= bcStale {
			leader, found = idx, true
		}
	}
	if t.TimeLeftKnown && t.TimeLeftMs > 0 {
		lt.laps = make(map[uint16]int, len(cars))
		for idx, c := range cars {
			lt.laps[idx] = c.laps
		}
	}
	if !found {
		return
	}
	c := cars[leader]
	t.LeaderKnown = true
	t.LeaderIsPlayer = int(leader) == focused
	t.LeaderProgress = c.spline
	t.LeaderLapMs = c.lastLapMs
	if t.LeaderLapMs == 0 {
		t.LeaderLapMs = c.bestLapMs
	}
	if t.TimeLeftKnown && t.TimeLeftMs <= 0 && lt.laps != nil {
		if before, ok := lt.laps[leader]; ok && c.laps > before {
			t.LeaderFinished = true
		}
	}
}

// readBroadcastConfig reads the port and password from ACC's broadcasting.json. ACC writes its
// config files in UTF-16.
func readBroadcastConfig(documents string) (port int, password string, err error) {
	if documents == "" {
		return 0, "", errors.New("Documents folder not found")
	}
	raw, err := os.ReadFile(filepath.Join(documents, "Assetto Corsa Competizione", "Config", bcConfigFileName))
	if err != nil {
		return 0, "", err
	}
	var cfg struct {
		Port     int    `json:"updListenerPort"`
		Password string `json:"connectionPassword"`
	}
	if err := json.Unmarshal(decodeConfigText(raw), &cfg); err != nil {
		return 0, "", errors.New(bcConfigFileName + ": " + err.Error())
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return 0, "", errors.New(bcConfigFileName + " has no port set")
	}
	return cfg.Port, cfg.Password, nil
}

// decodeConfigText turns UTF-16 (with or without byte order mark) or UTF-8 text into UTF-8.
func decodeConfigText(raw []byte) []byte {
	switch {
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		raw = raw[2:]
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF:
		return raw[3:]
	case len(raw) >= 2 && raw[1] == 0 && raw[0] != 0:
		// UTF-16 little-endian without a byte order mark
	default:
		return raw
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	return []byte(string(utf16.Decode(u)))
}

// Outgoing messages.

func registerPacket(password string) []byte {
	b := []byte{bcRegister, bcProtocol}
	b = appendStr(b, bcDisplayName)
	b = appendStr(b, password)
	b = binary.LittleEndian.AppendUint32(b, bcIntervalMs)
	return appendStr(b, "") // command password: SetupMan sends no commands
}

func idPacket(kind byte, id int32) []byte {
	return binary.LittleEndian.AppendUint32([]byte{kind}, uint32(id))
}

func appendStr(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

// bcReader reads incoming messages; reading past the end sets bad and returns zeros.
type bcReader struct {
	b   []byte
	i   int
	bad bool
}

func (r *bcReader) take(n int) []byte {
	if r.bad || r.i+n > len(r.b) {
		r.bad = true
		return make([]byte, n)
	}
	p := r.b[r.i : r.i+n]
	r.i += n
	return p
}

func (r *bcReader) u8() byte    { return r.take(1)[0] }
func (r *bcReader) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *bcReader) i32() int32  { return int32(binary.LittleEndian.Uint32(r.take(4))) }
func (r *bcReader) f32() float64 {
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(r.take(4))))
}
func (r *bcReader) str() string { return string(r.take(int(r.u16()))) }

type bcLap struct {
	ms            int // 0 = no time
	outLap, inLap bool
}

// lap reads the SDK's LapInfo.
func (r *bcReader) lap() bcLap {
	ms := r.i32()
	r.u16() // car index
	r.u16() // driver index
	splits := int(r.u8())
	for i := 0; i < splits; i++ {
		r.i32()
	}
	r.u8() // invalid
	r.u8() // valid for best
	l := bcLap{outLap: r.u8() == 1, inLap: r.u8() == 1}
	if ms > 0 && ms != bcNoLapTime {
		l.ms = int(ms)
	}
	return l
}
