package acc

import (
	"encoding/binary"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"unicode/utf16"

	"setupman/internal/game"
)

func carPacket(idx uint16, position, laps uint16, spline float32, lastMs int32, lastOut bool) []byte {
	le := binary.LittleEndian
	b := []byte{bcRealtimeCar}
	b = le.AppendUint16(b, idx)
	b = le.AppendUint16(b, 0) // driver index
	b = append(b, 1, 5)       // driver count, gear
	for i := 0; i < 3; i++ {  // x, y, yaw
		b = le.AppendUint32(b, math.Float32bits(1.5))
	}
	b = append(b, 1)            // on track
	b = le.AppendUint16(b, 180) // km/h
	b = le.AppendUint16(b, position)
	b = le.AppendUint16(b, position)
	b = le.AppendUint16(b, position)
	b = le.AppendUint32(b, math.Float32bits(spline))
	b = le.AppendUint16(b, laps)
	b = le.AppendUint32(b, 0) // delta
	lap := func(ms int32, out bool) {
		b = le.AppendUint32(b, uint32(ms))
		b = le.AppendUint16(b, idx)
		b = le.AppendUint16(b, 0)
		b = append(b, 3)
		for i := 0; i < 3; i++ {
			b = le.AppendUint32(b, 30000)
		}
		o := byte(0)
		if out {
			o = 1
		}
		b = append(b, 0, 1, o, 0)
	}
	lap(99000, false)
	lap(lastMs, lastOut)
	lap(math.MaxInt32, false)
	return b
}

func TestBroadcastCarsAndLeader(t *testing.T) {
	b := newBroadcast()
	b.cars = map[uint16]bcCar{}
	now := time.Now()
	b.handle(carPacket(7, 1, 20, 0.25, 101000, false), now)
	b.handle(carPacket(3, 4, 19, 0.95, 0, false), now)
	focus := []byte{bcRealtimeUpdate, 0, 0, 0, 0, 1, 5}
	focus = binary.LittleEndian.AppendUint32(focus, 0)
	focus = binary.LittleEndian.AppendUint32(focus, 0)
	focus = binary.LittleEndian.AppendUint32(focus, 3)
	b.handle(focus, now)
	if b.focused != 3 || len(b.cars) != 2 || b.cars[7].laps != 20 || b.cars[7].lastLapMs != 101000 {
		t.Fatalf("parsed %+v focused %d", b.cars, b.focused)
	}

	var lt leaderTrack
	tel := game.Telemetry{TimeLeftMs: 3000, TimeLeftKnown: true}
	fillLeader(&tel, b.cars, b.focused, &lt, now)
	if !tel.LeaderKnown || tel.LeaderIsPlayer || tel.LeaderFinished || tel.LeaderLapMs != 101000 || math.Abs(tel.LeaderProgress-0.25) > 1e-6 {
		t.Fatalf("leader %+v", tel)
	}
	// Clock run out, leader not yet across: not finished. Then a lap more: finished.
	tel = game.Telemetry{TimeLeftMs: -1000, TimeLeftKnown: true}
	fillLeader(&tel, b.cars, b.focused, &lt, now)
	if tel.LeaderFinished {
		t.Fatal("finished too early")
	}
	b.handle(carPacket(7, 1, 21, 0.01, 0, true), now) // an out-lap time is not the leader's pace
	tel = game.Telemetry{TimeLeftMs: -2000, TimeLeftKnown: true}
	fillLeader(&tel, b.cars, b.focused, &lt, now)
	if !tel.LeaderFinished || tel.LeaderLapMs != 99000 {
		t.Fatalf("after the leader's last lap: %+v", tel)
	}
	// A short message must not crash or add a car.
	b.handle([]byte{bcRealtimeCar, 1, 0}, now)
	if len(b.cars) != 2 {
		t.Fatal("short message added a car")
	}
}

func TestReadBroadcastConfigUTF16(t *testing.T) {
	doc := t.TempDir()
	dir := filepath.Join(doc, "Assetto Corsa Competizione", "Config")
	os.MkdirAll(dir, 0o755)
	text := "{\r\n    \"updListenerPort\": 9000,\r\n    \"connectionPassword\": \"asd\",\r\n    \"commandPassword\": \"\"\r\n}"
	raw := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(text)) {
		raw = binary.LittleEndian.AppendUint16(raw, u)
	}
	os.WriteFile(filepath.Join(dir, "broadcasting.json"), raw, 0o644)
	port, pw, err := readBroadcastConfig(doc)
	if err != nil || port != 9000 || pw != "asd" {
		t.Fatalf("got %d %q %v", port, pw, err)
	}
	os.WriteFile(filepath.Join(dir, "broadcasting.json"), []byte(text), 0o644)
	if port, _, err := readBroadcastConfig(doc); err != nil || port != 9000 {
		t.Fatalf("UTF-8: got %d %v", port, err)
	}
}

func TestRegisterPacket(t *testing.T) {
	p := registerPacket("asd")
	want := []byte{1, 4, 8, 0, 'S', 'e', 't', 'u', 'p', 'M', 'a', 'n', 3, 0, 'a', 's', 'd', 0xE8, 3, 0, 0, 0, 0}
	if string(p) != string(want) {
		t.Fatalf("got % x", p)
	}
}

// A pretend ACC on a local UDP port: SetupMan registers, ACC answers and sends cars, the leader
// shows up in the telemetry; stopping says goodbye.
func TestBroadcastConnection(t *testing.T) {
	srv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Skip("no local UDP:", err)
	}
	defer srv.Close()
	doc := t.TempDir()
	dir := filepath.Join(doc, "Assetto Corsa Competizione", "Config")
	os.MkdirAll(dir, 0o755)
	port := srv.LocalAddr().(*net.UDPAddr).Port
	os.WriteFile(filepath.Join(dir, "broadcasting.json"), []byte(`{"updListenerPort": `+strconv.Itoa(port)+`, "connectionPassword": "asd", "commandPassword": ""}`), 0o644)

	b := newBroadcast()
	b.docs = func() string { return doc }
	b.want(true)

	buf := make([]byte, 2048)
	srv.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, client, err := srv.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != string(registerPacket("asd")) {
		t.Fatalf("register: % x %v", buf[:n], err)
	}
	reply := binary.LittleEndian.AppendUint32([]byte{bcRegistration}, 42)
	reply = append(reply, 1, 0, 0, 0)
	srv.WriteToUDP(reply, client)
	n, _, err = srv.ReadFromUDP(buf) // entry list request
	if err != nil || string(buf[:n]) != string(idPacket(bcRequestEntries, 42)) {
		t.Fatalf("entry list request: % x %v", buf[:n], err)
	}
	srv.WriteToUDP(carPacket(5, 1, 30, 0.5, 100000, false), client)
	srv.WriteToUDP(carPacket(9, 3, 30, 0.4, 101000, false), client)

	deadline := time.Now().Add(3 * time.Second)
	var tel game.Telemetry
	for time.Now().Before(deadline) {
		tel = game.Telemetry{TimeLeftMs: 60000, TimeLeftKnown: true}
		b.fill(&tel, time.Now())
		if tel.LeaderKnown {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !tel.LeaderKnown || tel.LeaderLapMs != 100000 {
		t.Fatalf("leader not seen: %+v", tel)
	}
	b.want(false)
	n, _, err = srv.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != string(idPacket(bcUnregister, 42)) {
		t.Fatalf("unregister: % x %v", buf[:n], err)
	}
}
