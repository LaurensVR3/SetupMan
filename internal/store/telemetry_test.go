package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// A library made by an earlier 0.24.0 test build lacked the "turns" column (seen on the owner's
// library): the upgrade adds it and has every file indexed again.
func TestTelemetryUpgradeMissingColumn(t *testing.T) {
	for _, c := range []struct {
		version int
		drop    string
		reindex bool
	}{
		{9, "DROP COLUMN turns", true}, // the owner's library
		{11, "DROP COLUMN turns", true},
		{12, "DROP COLUMN profile", true},         // a test build that already said "up to date"
		{8, "DROP COLUMN note", true},             // any other column
		{9, "RENAME TO telemetry_laps_old", true}, // no lap table at all
	} {
		path := filepath.Join(t.TempDir(), "library.db")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		f := &TeleFile{Game: "acc", Name: "a.ld", Size: 5, Modified: time.Now(), Car: "c", Track: "spa", StartedAt: time.Now()}
		if err := s.SaveTelemetryFile(f, []TeleLap{{N: 1, LapMs: 138000, Complete: true}}); err != nil {
			t.Fatal(err)
		}
		s.Close()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE telemetry_laps %s; PRAGMA user_version = %d;`, c.drop, c.version)); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		known, err := s.TelemetryKnown("acc")
		if err != nil || len(known) != 1 || (known["a.ld"].Size == -1) != c.reindex {
			t.Fatalf("%+v: %+v %v", c, known, err)
		}
		if err := s.SaveTelemetryFile(f, []TeleLap{{N: 1, LapMs: 138000, Complete: true, Turns: -1}}); err != nil {
			t.Fatalf("%+v: saving after the upgrade: %v", c, err)
		}
		if err := s.RefreshTelemetrySaves("acc"); err != nil {
			t.Fatal(err)
		}
		if err := s.RefreshTelemetryFull("acc"); err != nil {
			t.Fatal(err)
		}
		if list, err := s.TelemetryFiles("acc", "", ""); err != nil || len(list) != 1 {
			t.Fatalf("%+v: list %v %v", c, list, err)
		}
		s.Close()
	}
}

func TestTelemetryFiles(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Date(2026, 11, 17, 22, 6, 38, 0, time.Local)
	f := &TeleFile{Game: "acc", Name: "spa-ferrari_296_gt3-3-2026.11.17-22.06.38.ld", Size: 100, Modified: start, Car: "ferrari_296_gt3",
		Track: "spa", StartedAt: start, DurationMs: 400000, Channels: 55}
	laps := []TeleLap{{N: 0, LapMs: 150000}, {N: 1, StartMs: 150000, LapMs: 138000, Complete: true}, {N: 2, StartMs: 288000, LapMs: 137500, Complete: true}}
	if err := s.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTelemetryLapNote(f.ID, 2, "new kerb line"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTelemetryReference(f.ID, 2, true); err != nil {
		t.Fatal(err)
	}
	// indexing again (the file grew) keeps the note and the reference mark
	f.Size = 200
	if err := s.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	got, err := s.TelemetryLaps(f.ID)
	if err != nil || len(got) != 3 || got[2].Note != "new kerb line" || !got[2].Reference {
		t.Fatalf("laps after re-index: %+v %v", got, err)
	}
	list, err := s.TelemetryFiles("acc", "", "")
	if err != nil || len(list) != 1 || list[0].Laps != 2 || list[0].BestMs != 137500 || list[0].Size != 200 {
		t.Fatalf("list: %+v %v", list, err)
	}

	// a second file on the same car and track takes over the reference lap
	g := &TeleFile{Game: "acc", Name: "spa-ferrari_296_gt3-4-2026.11.18-20.00.00.ld", Size: 1, Modified: start, Car: "ferrari_296_gt3",
		Track: "spa", StartedAt: start.Add(24 * time.Hour), DurationMs: 1000, Channels: 55}
	if err := s.SaveTelemetryFile(g, laps); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTelemetryReference(g.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	if fid, n, ok, _ := s.TelemetryReference("acc", "ferrari_296_gt3", "spa"); !ok || fid != g.ID || n != 1 {
		t.Fatalf("reference: %d %d %v", fid, n, ok)
	}

	// linking to a session: the setup comes from the session and follows it
	sess := &Session{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Kind: "race", StartedAt: start}
	if err := s.InsertSession(sess); err != nil {
		t.Fatal(err)
	}
	lap := &Lap{SessionID: sess.ID, LapMs: 138000, Valid: false}
	if err := s.InsertLap(lap); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkTelemetry(f.ID, sess.ID, map[int]int64{1: lap.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionSetup(sess.ID, 42, 43, "manual", true); err != nil {
		t.Fatal(err)
	}
	tf, _ := s.TelemetryFile(f.ID)
	if tf.SetupID != 42 || tf.Kind != "race" || tf.Match != TeleSession || tf.BestMs != 137500 {
		t.Fatalf("linked file: %+v (best should skip the invalid lap)", tf)
	}
	if bySetup, _ := s.TelemetryForSetup(42); len(bySetup) != 1 {
		t.Fatalf("for setup: %+v", bySetup)
	}
	got, _ = s.TelemetryLaps(f.ID)
	if got[1].Valid == nil || *got[1].Valid {
		t.Fatalf("lap 1 should be invalid: %+v", got[1])
	}
	// files with a session can't get a setup of their own
	s.SetTelemetrySetup(f.ID, 7, 8)
	if tf, _ := s.TelemetryFile(f.ID); tf.SetupID != 42 {
		t.Fatalf("setup changed on a linked file: %d", tf.SetupID)
	}
	s.SetTelemetrySetup(g.ID, 7, 8)
	if tf, _ := s.TelemetryFile(g.ID); tf.SetupID != 7 || tf.Match != TeleManual {
		t.Fatalf("manual setup: %+v", tf)
	}
}

func TestRefreshTelemetryFull(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	at := time.Now()
	f := &TeleFile{Game: "acc", Name: "a.ld", Modified: at, Car: "c", Track: "Spa", StartedAt: at}
	// two real laps of ~7 km, a 16-second "lap" after being sent back to the pits, a pit-lane lap
	laps := []TeleLap{{N: 1, LapMs: 138000, Complete: true, DistM: 7004}, {N: 2, LapMs: 16000, Complete: true, DistM: 420},
		{N: 3, LapMs: 139000, Complete: true, DistM: 6990}, {N: 4, LapMs: 160000, Complete: true, DistM: 6850}}
	g := &TeleFile{Game: "acc", Name: "b.ld", Modified: at, Car: "c", Track: "spa", StartedAt: at}
	// a file with only a short piece: judged against the track, not the file
	if err := s.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTelemetryFile(g, []TeleLap{{N: 1, LapMs: 30000, Complete: true, DistM: 1500}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshTelemetryFull("acc"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.TelemetryLaps(f.ID)
	want := []bool{true, false, true, true}
	for i, l := range got {
		if l.Complete != want[i] {
			t.Errorf("lap %d: full = %v", l.N, l.Complete)
		}
	}
	if gl, _ := s.TelemetryLaps(g.ID); gl[0].Complete {
		t.Error("the short piece in the other file should not be a full lap")
	}
	if tf, _ := s.TelemetryFile(f.ID); tf.BestMs != 138000 || tf.Laps != 3 {
		t.Errorf("best %d, laps %d", tf.BestMs, tf.Laps)
	}
}

// saveLaps stores a file whose laps have the given times; laps from data on have data (D-167).
func saveLaps(t *testing.T, s *Store, name string, start time.Time, offS, durS, data int, times ...int) *TeleFile {
	t.Helper()
	f := &TeleFile{Game: "acc", Name: name, Modified: start, Car: "c", Track: "donington", StartedAt: start.Add(time.Duration(offS) * time.Second), DurationMs: int64(durS) * 1000}
	var laps []TeleLap
	for n, ms := range times {
		l := TeleLap{N: n, LapMs: ms, Complete: n > 0}
		if n >= data {
			l.DistM, l.Turns = 3940+float64(n), 1
		}
		laps = append(laps, l)
	}
	if err := s.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRefreshTelemetrySaves(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	start := time.Date(2026, 11, 17, 20, 0, 0, 0, time.Local)
	a := saveLaps(t, s, "d-c-2-x.ld", start, 1, 578, 99, 142793, 194440)
	b := saveLaps(t, s, "d-c-59-x.ld", start, 0, 7585, 99, 142793, 194440, 91003)
	other := saveLaps(t, s, "d-c-9-y.ld", start, -1800, 1350, 99, 128665) // an earlier session
	s.SetTelemetryReference(a.ID, 1, true)
	s.SetTelemetryLapNote(a.ID, 1, "traffic")
	if err := s.RefreshTelemetrySaves("acc"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.TelemetryFiles("acc", "", "")
	if len(list) != 2 || list[0].ID != b.ID || list[1].ID != other.ID {
		t.Fatalf("visible files: %+v", list)
	}
	// the session shows each lap once; the marked copy of lap 1 (in the earlier save) is the one shown
	laps, _ := s.TelemetrySessionLaps(b.ID)
	if len(laps) != 3 || laps[1].FileID != a.ID || !laps[1].Reference || laps[1].Note != "traffic" || laps[2].FileID != b.ID {
		t.Fatalf("session laps: %+v", laps)
	}
	if fid, n, ok, _ := s.TelemetryReference("acc", "c", "donington"); !ok || fid != a.ID || n != 1 {
		t.Fatalf("reference: %d %d %v", fid, n, ok)
	}
	if n, bytes, _ := s.TelemetryDisk("acc"); n != 3 || bytes != 0 {
		t.Fatalf("disk: %d files", n)
	}
}

// Every save is a window: a later save can lack the start of the session, and each keeps data only
// for its last laps (seen on the user's files). Laps are matched by their times; no lap is lost.
func TestRefreshTelemetrySavesTrimmed(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	start := time.Date(2026, 4, 28, 18, 43, 0, 0, time.Local)
	early := saveLaps(t, s, "d-c-9-x.ld", start, 15, 808, 1, 59700, 86315, 86155, 126790, 86763, 86077)
	late := saveLaps(t, s, "d-c-11-y.ld", start, 120, 1085, 5, 42055, 86155, 126790, 86763, 86077, 87093, 85858)
	s.SetTelemetryReference(early.ID, 4, true) // 86763
	for run := 0; run < 2; run++ {             // the same result when done again
		if err := s.RefreshTelemetrySaves("acc"); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.TelemetryFiles("acc", "", ""); len(list) != 1 || list[0].ID != late.ID {
			t.Fatalf("visible files: %+v", list)
		}
		laps, _ := s.TelemetrySessionLaps(late.ID)
		// session laps 0 … 7: 0 … 5 from the earlier save (it has their data), 6 and 7 from the later one
		want := []struct {
			file int64
			n    int
		}{{early.ID, 0}, {early.ID, 1}, {early.ID, 2}, {early.ID, 3}, {early.ID, 4}, {early.ID, 5}, {late.ID, 5}, {late.ID, 6}}
		if len(laps) != len(want) {
			t.Fatalf("run %d: %d laps: %+v", run, len(laps), laps)
		}
		for i, w := range want {
			if laps[i].FileID != w.file || laps[i].N != w.n || laps[i].Lap != i {
				t.Fatalf("run %d, session lap %d: %+v, want file %d lap %d", run, i, laps[i], w.file, w.n)
			}
		}
		if !laps[4].Reference {
			t.Fatal("the reference lap stays where it was set")
		}
	}
	// the earlier save opened on its own still lists all its laps
	if own, _ := s.TelemetrySessionLaps(early.ID); len(own) != 6 {
		t.Fatalf("own laps of the earlier save: %d", len(own))
	}
	// the newest save deleted: the earlier one is shown again on its own
	s.SetTelemetryMissing(late.ID, true)
	s.RefreshTelemetrySaves("acc")
	if list, _ := s.TelemetryFiles("acc", "", ""); len(list) != 1 || list[0].ID != early.ID {
		t.Fatalf("after deleting the newest save: %+v", list)
	}
}

// One shared lap is enough when the earlier save has only one timed lap, even when the starts
// worked out from the files are minutes apart (Silverstone 2023-10-01: 168 s).
func TestRefreshTelemetrySavesOneLap(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	start := time.Date(2023, 10, 1, 10, 30, 0, 0, time.Local)
	a := saveLaps(t, s, "s-c-1-x.ld", start, 0, 170, 0, 43715, 119382)
	b := saveLaps(t, s, "s-c-7-y.ld", start, 168, 1300, 0, 43715, 119382, 118700, 118650)
	c := saveLaps(t, s, "s-c-3-z.ld", start, 2400, 400, 0, 50000, 119382, 118000) // a later session, same lap time once
	if err := s.RefreshTelemetrySaves("acc"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.TelemetryFiles("acc", "", "")
	if len(list) != 2 || list[0].ID != c.ID || list[1].ID != b.ID {
		t.Fatalf("visible files: %+v (a = %d)", list, a.ID)
	}
	laps, _ := s.TelemetryTrackLaps("acc", "", "donington")
	seen := map[int]bool{}
	for _, l := range laps {
		if l.SessionFile == b.ID && seen[l.LapMs] {
			t.Fatalf("lap %d listed twice", l.LapMs)
		}
		seen[l.LapMs] = true
	}
}

// A lap that cuts a chicane is barely shorter but much faster through it (Spa 861:16, D-167).
func TestRefreshTelemetryFullCut(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	at := time.Now()
	profile := func(bus byte) []byte {
		p := make([]byte, ProfileBins)
		for i := range p {
			p[i] = 100 // 200 km/h
		}
		p[95] = bus // the Bus Stop: 62 km/h usually
		return p
	}
	var laps []TeleLap
	for n := 1; n <= 8; n++ {
		l := TeleLap{N: n, LapMs: 136000 + n, Complete: true, DistM: 6938, Turns: 1, Profile: profile(31)}
		if n == 8 {
			l.LapMs, l.DistM, l.Profile = 132252, 6901, profile(68) // 137 km/h
		}
		laps = append(laps, l)
	}
	f := &TeleFile{Game: "acc", Name: "spa.ld", Modified: at, Car: "c", Track: "spa", StartedAt: at}
	if err := s.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshTelemetryFull("acc"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.TelemetryLaps(f.ID)
	for _, l := range got {
		if l.Complete != (l.N != 8) {
			t.Errorf("lap %d (%d ms): full = %v", l.N, l.LapMs, l.Complete)
		}
	}
}

// A file indexed again (it changed on disk) loses its lap links, so it must be linked again.
func TestTelemetryReindexRelinks(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	at := time.Now()
	f := &TeleFile{Game: "acc", Name: "a.ld", Modified: at, Car: "c", Track: "spa", StartedAt: at}
	laps := []TeleLap{{N: 1, LapMs: 138000, Complete: true}}
	s.SaveTelemetryFile(f, laps)
	sess := &Session{Game: "acc", Car: "c", Track: "spa", StartedAt: at}
	s.InsertSession(sess)
	s.LinkTelemetry(f.ID, sess.ID, map[int]int64{})
	f.Size = 99
	if err := s.SaveTelemetryFile(f, laps); err != nil {
		t.Fatal(err)
	}
	if un, _ := s.TelemetryUnlinked("acc"); len(un) != 1 {
		t.Fatalf("the re-indexed file should wait to be linked again: %+v", un)
	}
}
