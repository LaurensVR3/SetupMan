package library

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setupman/internal/compare"
	"setupman/internal/datapack"
	"setupman/internal/game"
	"setupman/internal/games/acc"
	"setupman/internal/store"
	"setupman/internal/telemetry"
)

// motecGame is a fake ACC with a telemetry folder.
type motecGame struct {
	fakeGame
	tele string
}

func (g *motecGame) TelemetryFolder() string { return g.tele }

// writeLD writes a small MoTeC log in ACC's layout: TIME (10 Hz) with an out-lap of 2 s, laps of
// 3.0, 3.1 and 3.2 s and 1.5 s of in-lap, and SPEED (20 Hz, km/h) for the whole log.
func writeLD(t *testing.T, path string) {
	t.Helper()
	var clock, speed []float32
	for _, part := range []int{20, 30, 31, 32, 15} {
		for i := 0; i < part; i++ {
			clock = append(clock, float32(i)*0.1)
		}
	}
	for i := 0; i < 2*len(clock); i++ {
		speed = append(speed, 180+float32(i%10))
	}
	type ch struct {
		name string
		hz   int
		data []float32
	}
	chans := []ch{{"TIME", 10, clock}, {"SPEED", 20, speed}}
	le := binary.LittleEndian
	const head, meta = 0x6E2, 124
	dataStart := head + meta*len(chans)
	buf := make([]byte, dataStart)
	le.PutUint32(buf[0:], 0x40)
	le.PutUint32(buf[8:], head)
	le.PutUint32(buf[12:], uint32(dataStart))
	off := dataStart
	for i, c := range chans {
		m := buf[head+i*meta:]
		if i > 0 {
			le.PutUint32(m[0:], uint32(head+(i-1)*meta))
		}
		if i < len(chans)-1 {
			le.PutUint32(m[4:], uint32(head+(i+1)*meta))
		}
		le.PutUint32(m[8:], uint32(off))
		le.PutUint32(m[12:], uint32(len(c.data)))
		le.PutUint16(m[18:], 0x07)
		le.PutUint16(m[20:], 4)
		le.PutUint16(m[22:], uint16(c.hz))
		copy(m[32:64], c.name)
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

// senderWithLaps: a library with setup "a" at Spa, a recorded session of three laps with it, the
// game's telemetry file of that session and SetupMan's extra log.
func senderWithLaps(t *testing.T) (*Library, int64) {
	t.Helper()
	g := &motecGame{fakeGame: fakeGame{installed: true, folder: setupFolder(t), live: game.LiveUnknown}, tele: t.TempDir()}
	l, st := newLib(t, &g.fakeGame)
	l.games = []game.Game{g}
	l.TelemetryDir = t.TempDir()
	s := st.Load()
	s.Initials = "abc"
	st.Save(s)
	l.Start()
	waitFor(t, "first scan", func() bool { return l.Games()[0].Status == "ok" })
	td, _ := l.Track("acc", "ferrari_296_gt3", "spa")
	id := td.SetupIDs[0]

	saved := time.Date(2026, 11, 24, 21, 0, 12, 0, time.Local)
	name := "Spa-ferrari_296_gt3-3-" + saved.Format("2006.01.02-15.04.05") + ".ld"
	writeLD(t, filepath.Join(g.tele, name))
	f, laps, err := telemetry.IndexFile(g.tele, name, "acc")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.db.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	sess := &store.Session{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Kind: "practice", StartedAt: f.StartedAt, SetupID: id, Match: "auto"}
	if err := l.db.InsertSession(sess); err != nil {
		t.Fatal(err)
	}
	v, _ := l.db.LatestVersion(id)
	links := map[int]int64{}
	at := f.StartedAt.Add(2 * time.Second)
	for i, ms := range []int{3000, 3100, 3200} {
		at = at.Add(time.Duration(ms) * time.Millisecond)
		rl := &store.Lap{SessionID: sess.ID, SetupID: id, VersionID: v.ID, At: at, LapMs: ms, Valid: i != 1, FuelUsed: 2.5, Air: 22, Road: 30, HasTemps: true}
		l.db.InsertLap(rl)
		links[i+1] = rl.ID
	}
	if err := l.db.LinkTelemetry(f.ID, sess.ID, links); err != nil {
		t.Fatal(err)
	}
	l.db.SetTelemetryLapNote(f.ID, 3, "best line through Eau Rouge")

	// the extra log: lap clock (ms), lap counter one reading after the line, last-lap time
	h := telemetry.ExtraHeader{Game: "acc", Session: sess.ID, Start: f.StartedAt, Hz: 10, Channels: []telemetry.ExtraChannel{
		{Name: telemetry.ExtraLapCount}, {Name: telemetry.ExtraLapTime}, {Name: telemetry.ExtraLastLap}, {Name: "FUEL"}}}
	w, err := telemetry.CreateExtra(telemetry.ExtraPath(l.TelemetryDir, "acc", sess.ID), h)
	if err != nil {
		t.Fatal(err)
	}
	lap, last, row := float32(0), float32(0), 0
	parts := []int{20, 30, 31, 32, 15}
	for i, p := range parts {
		for k := 0; k < p; k++ {
			if k == 1 && i > 1 {
				lap++
				last = float32(parts[i-1] * 100)
			}
			w.Write(float64(row)/10, []float32{lap, float32(k) * 100, last, 60 - float32(row)/100})
			row++
		}
	}
	w.Close()
	return l, id
}

func receiver(t *testing.T) *Library {
	t.Helper()
	g := &fakeGame{installed: true, folder: t.TempDir(), live: game.LiveUnknown}
	l, _ := newLib(t, g)
	l.TelemetryDir = t.TempDir()
	l.Start()
	waitFor(t, "receiver scan", func() bool { return l.Games()[0].Status != "" })
	return l
}

func importPack(t *testing.T, l *Library, data []byte) *PackImportResult {
	t.Helper()
	res, err := l.ImportPack(bytes.NewReader(data), int64(len(data)), false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res
}

func TestDataPackRoundTrip(t *testing.T) {
	l, id := senderWithLaps(t)
	req := PackRequest{Setups: []PackSetupReq{{ID: id, LapTimes: true, Telemetry: PackTeleAll}}}
	pv, err := l.PreviewPack(req)
	if err != nil || pv.Laps != 2 || len(pv.Setups) != 1 || pv.Setups[0].LapsAll != 2 || pv.Setups[0].LapsBest != 1 {
		t.Fatalf("preview (the invalid lap is left out): %+v %v", pv, err)
	}
	var buf bytes.Buffer
	name, err := l.WritePack(req, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "ABC Spa") || !strings.HasSuffix(name, ".zip") {
		t.Errorf("file name %q", name)
	}
	p, err := datapack.Open(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	m := p.Manifest
	// lap 2 was invalid: laps 1 and 3 are not consecutive, so two sessions with one lap each
	if m.By != "ABC" || len(m.Setups) != 1 || len(m.Sessions) != 2 || m.Sessions[0].Log == nil || m.Sessions[0].Log.Extra == "" {
		t.Fatalf("manifest: %+v", m)
	}
	if strings.Contains(string(mustReadPack(t, p, datapack.ManifestName)), l.TelemetryDir) {
		t.Error("the manifest holds a folder path of this PC")
	}

	r := receiver(t)
	res := importPack(t, r, buf.Bytes())
	if res.Setups != 1 || res.Sessions != 2 || res.Laps != 2 || res.Track == nil {
		t.Fatalf("import result: %+v", res)
	}
	got, _ := r.db.Setup(res.Items[0].SetupID)
	if got.Ext != "ABC" || got.Name != "a" || got.RelPath != "" || !strings.Contains(got.Note, "From ABC's data pack") {
		t.Errorf("imported setup: %+v", got)
	}
	files, _ := r.db.TelemetryFiles("acc", "", "")
	if len(files) != 2 || files[0].Ext != "ABC" || files[0].Pack != m.ID {
		t.Fatalf("telemetry files: %+v", files)
	}
	rows, _ := r.db.TelemetryTrackLaps("acc", "", "spa")
	if len(rows) == 0 {
		t.Fatal("no laps on the Telemetry page")
	}
	var three *store.TeleLapRow
	for i := range rows {
		if rows[i].LapMs == 3200 {
			three = &rows[i]
		}
	}
	if three == nil || three.Ext != "ABC" || three.SetupID != got.ID || three.Note != "best line through Eau Rouge" || three.Lap != 3 {
		t.Fatalf("lap 3: %+v", three)
	}
	d, err := r.TelemetryLap(three.FileID, three.N, []string{"SPEED", "x:FUEL"}, false)
	if err != nil || len(d.Channels) == 0 {
		t.Fatalf("lap data from the pack's file: %+v %v", d, err)
	}
	if x := r.extraLog("acc", three.SessionID); x == nil {
		t.Error("the extra log came along but isn't found")
	}
	// never the user's own: no laps for statistics
	if laps, _ := r.db.LapsForSetup(got.ID); len(laps) != 0 {
		t.Errorf("external laps counted as own: %d", len(laps))
	}
	if best, _ := r.db.TelemetryBestPerCar("acc", "ferrari_296_gt3"); len(best) != 0 {
		t.Errorf("external lap counted as own best: %v", best)
	}
	// suggested names never rename someone else's setup
	if n := r.SuggestedName(got.ID); n != "" {
		t.Errorf("suggested name for an external setup: %q", n)
	}

	// the same pack again: refused
	if _, err := r.ImportPack(bytes.NewReader(buf.Bytes()), int64(buf.Len()), false); err == nil {
		t.Error("the same pack was imported twice")
	}

	// a newer pack after a change: a new version of the same external setup
	if _, err := l.EditSetup(id, map[string]string{"advancedSetup.mechanicalBalance.brakeBias": "42"}, ""); err != nil {
		t.Fatal(err)
	}
	var buf2 bytes.Buffer
	if _, err := l.WritePack(PackRequest{Setups: []PackSetupReq{{ID: id, Telemetry: PackTeleNone}}}, &buf2); err != nil {
		t.Fatal(err)
	}
	c, err := r.InspectPack(bytes.NewReader(buf2.Bytes()), int64(buf2.Len()))
	if err != nil || len(c.Setups) != 1 || c.Setups[0].Result != "update" || c.By != "ABC" {
		t.Fatalf("inspect: %+v %v", c, err)
	}
	res2 := importPack(t, r, buf2.Bytes())
	if res2.Updated != 1 || res2.Setups != 0 {
		t.Fatalf("second pack: %+v", res2)
	}
	if vs, _ := r.db.Versions(got.ID); len(vs) != 2 {
		t.Errorf("versions after the newer pack: %d", len(vs))
	}

	// remove the first pack: its setup, sessions, files and extra logs go
	packs, _ := r.Packs()
	if len(packs) != 2 {
		t.Fatalf("packs: %+v", packs)
	}
	rm, err := r.RemovePack(m.ID)
	if err != nil || rm.Setups != 1 || rm.Laps != 2 {
		t.Fatalf("remove: %+v %v", rm, err)
	}
	if s, _ := r.db.Setup(got.ID); s != nil {
		t.Error("the pack's setup is still there")
	}
	if files, _ := r.db.TelemetryFiles("acc", "", ""); len(files) != 0 {
		t.Errorf("telemetry files left: %d", len(files))
	}
	if _, err := os.Stat(filepath.Join(r.PacksDir(), m.ID)); !os.IsNotExist(err) {
		t.Error("the pack's folder is still there")
	}
	if left, _ := filepath.Glob(filepath.Join(r.TelemetryDir, "*.smx.gz")); len(left) != 0 {
		t.Errorf("extra logs left: %v", left)
	}
}

func TestDataPackRules(t *testing.T) {
	l, id := senderWithLaps(t)
	// no initials: no pack
	s := l.store.Load()
	s.Initials = ""
	l.store.Save(s)
	l.Apply()
	if _, err := l.WritePack(PackRequest{Setups: []PackSetupReq{{ID: id}}}, &bytes.Buffer{}); err != ErrNoInitials {
		t.Errorf("without initials: %v", err)
	}
	s.Initials = "ABC"
	l.store.Save(s)
	l.Apply()
	var buf bytes.Buffer
	if _, err := l.WritePack(PackRequest{Setups: []PackSetupReq{{ID: id, Telemetry: PackTeleBest}}}, &buf); err != nil {
		t.Fatal(err)
	}
	// the receiver's own identical setup: the laps go to that one, no copy
	r := receiver(t)
	base, _ := os.ReadFile("../games/acc/testdata/base.json")
	fields, _ := acc.New().ParseSetup(base)
	mine := &store.Setup{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Name: "mine"}
	r.db.InsertSetup(mine)
	r.db.InsertVersion(&store.Version{SetupID: mine.ID, Fingerprint: compare.Fingerprint(fields), Content: base, Source: "import"})
	res := importPack(t, r, buf.Bytes())
	if res.Setups != 0 || res.Present != 1 || res.Laps != 1 {
		t.Fatalf("identical setup: %+v", res)
	}
	// a setup from a pack is not sent on
	ext := res.Items[0].SetupID
	sx, _ := r.db.Setup(ext)
	if sx.Ext != "" {
		t.Fatalf("the receiver's own setup became external")
	}
	rows, _ := r.db.TelemetryTrackLaps("acc", "", "spa")
	if len(rows) != 1 || rows[0].SetupID != ext {
		t.Fatalf("lap attached to the own setup: %+v", rows)
	}
	pv, err := r.PreviewPack(PackRequest{Laps: []PackLapReq{{FileID: rows[0].FileID, N: rows[0].N}}})
	if err == nil {
		t.Errorf("someone else's lap can be sent on: %+v", pv)
	}
}

func mustReadPack(t *testing.T, p *datapack.Pack, name string) []byte {
	t.Helper()
	b, err := p.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
