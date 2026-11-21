package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"setupman/internal/naming"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSetupsVersionsEvents(t *testing.T) {
	s := open(t)
	a := &Setup{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Name: "LA 296 SPA Q", RelPath: "ferrari_296_gt3/Spa/LA 296 SPA Q.json",
		Session: "Q", Meta: naming.Parse("LA 296 SPA Q")}
	if err := s.InsertSetup(a); err != nil || a.ID == 0 {
		t.Fatalf("insert setup: %v", err)
	}
	v1 := &Version{SetupID: a.ID, Fingerprint: "f1", Content: []byte("{1}"), FileModified: time.Now(), Source: "import"}
	v2 := &Version{SetupID: a.ID, Fingerprint: "f2", Content: []byte("{2}"), FileModified: time.Now(), Source: "change"}
	if err := s.InsertVersion(v1); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertVersion(v2); err != nil {
		t.Fatal(err)
	}
	if v1.Seq != 1 || v2.Seq != 2 {
		t.Errorf("seq = %d, %d", v1.Seq, v2.Seq)
	}
	latest, _ := s.LatestVersion(a.ID)
	if latest == nil || latest.ID != v2.ID || string(latest.Content) != "{2}" {
		t.Errorf("latest = %+v", latest)
	}

	// Path lookup is case-insensitive (Windows).
	got, err := s.SetupByPath("acc", "FERRARI_296_GT3/spa/la 296 spa q.json")
	if err != nil || got == nil || got.ID != a.ID || got.Meta.Initials != "LA" {
		t.Fatalf("by path: %+v %v", got, err)
	}

	// Branch with a parent version; then the file disappears.
	b := &Setup{Game: "acc", Car: "ferrari_296_gt3", Track: "spa", Name: "LA 296 SPA R", RelPath: "ferrari_296_gt3/Spa/LA 296 SPA R.json",
		ParentVersionID: v1.ID, LinkKind: LinkProbable, LinkScore: 71}
	if err := s.InsertSetup(b); err != nil {
		t.Fatal(err)
	}
	b.RelPath, b.Note = "", "TC shortcuts 2 and 5"
	if err := s.UpdateSetup(b); err != nil {
		t.Fatal(err)
	}
	b2, _ := s.Setup(b.ID)
	if b2.RelPath != "" || b2.ParentVersionID != v1.ID || b2.LinkKind != LinkProbable || b2.Note != "TC shortcuts 2 and 5" {
		t.Errorf("updated setup: %+v", b2)
	}
	list, _ := s.SetupsForTrack("acc", "Ferrari_296_GT3", "SPA")
	if len(list) != 2 {
		t.Errorf("setups for track: %d", len(list))
	}

	// Events.
	s.InsertEvent(Event{Game: "acc", Kind: "new-setup", SetupID: a.ID, Details: map[string]any{"name": a.Name}})
	s.InsertEvent(Event{Game: "acc", Kind: "branch", SetupID: b.ID})
	n, _ := s.CountUnseen()
	ev, _ := s.Events(true, 10)
	if n != 2 || len(ev) != 2 || ev[0].Kind != "branch" || ev[1].Details["name"] != a.Name {
		t.Errorf("events: %d %+v", n, ev)
	}
	s.MarkAllSeen()
	if n, _ := s.CountUnseen(); n != 0 {
		t.Error("not marked seen")
	}
}

func TestSchemaV2(t *testing.T) {
	s := open(t)
	a := &Setup{Game: "acc", Car: "c", Track: "t", Name: "x", Fresh: true}
	s.InsertSetup(a)
	if got, _ := s.Setup(a.ID); !got.Fresh {
		t.Error("fresh flag lost")
	}
	s.SetManagedTrack("acc", "Ferrari_296_GT3", "Spa")
	if ok, _ := s.IsManagedTrack("acc", "ferrari_296_gt3", "spa"); !ok {
		t.Error("managed track (case-insensitive) not found")
	}
	if ok, _ := s.IsManagedTrack("acc", "ferrari_296_gt3", "monza"); ok {
		t.Error("unmanaged track reported managed")
	}
	s.AddPendingOp(PendingOp{Game: "acc", Kind: "unstage", Payload: map[string]any{"setupId": 3}})
	ops, _ := s.PendingOps()
	if len(ops) != 1 || ops[0].Kind != "unstage" || ops[0].Payload["setupId"] != float64(3) {
		t.Fatalf("pending ops: %+v", ops)
	}
	s.DeletePendingOp(ops[0].ID)
	if ops, _ := s.PendingOps(); len(ops) != 0 {
		t.Error("op not deleted")
	}
}

// A library made by Phase 2 (schema v1) is upgraded without losing anything.
func TestUpgradeFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateV1(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO setups (game, car, track, name, created_at) VALUES ('acc','c','t','old',1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, _ := s.SetupsForGame("acc")
	if len(list) != 1 || list[0].Name != "old" || list[0].Fresh {
		t.Errorf("after upgrade: %+v", list)
	}
	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != schemaVersion {
		t.Errorf("schema version %d, want %d", v, schemaVersion)
	}
}

func TestTxRollback(t *testing.T) {
	s := open(t)
	err := s.Tx(func(q Q) error {
		q.InsertSetup(&Setup{Game: "acc", Car: "c", Track: "t", Name: "x"})
		return errFake
	})
	if err != errFake {
		t.Fatal(err)
	}
	if list, _ := s.SetupsForGame("acc"); len(list) != 0 {
		t.Error("rolled-back insert is visible")
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "fake" }

var errFake = fakeErr{}

func TestFileDatabaseAndImported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Imported("acc"); ok {
		t.Error("not imported yet")
	}
	s.SetImported("acc")
	s.Close()
	s, err = Open(path) // reopen: schema already there, data kept
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if ok, _ := s.Imported("acc"); !ok {
		t.Error("imported flag lost")
	}
}

// Tyre pressures per lap (D-124) are stored and read back; laps without them stay without.
func TestLapPressures(t *testing.T) {
	s := open(t)
	ses := &Session{Game: "acc", Car: "c", Track: "t", Kind: "race"}
	if err := s.InsertSession(ses); err != nil {
		t.Fatal(err)
	}
	s.InsertLap(&Lap{SessionID: ses.ID, LapMs: 100000, Valid: true, Pressures: [4]float64{27.1, 27.2, 26.5, 26.6}, HasPressures: true, Wet: true})
	s.InsertLap(&Lap{SessionID: ses.ID, LapMs: 101000, Valid: true})
	laps, err := s.LapsForSession(ses.ID)
	if err != nil || len(laps) != 2 {
		t.Fatalf("laps %v %v", laps, err)
	}
	if !laps[0].HasPressures || laps[0].Pressures[2] != 26.5 || !laps[0].Wet {
		t.Errorf("first lap: %+v", laps[0])
	}
	if laps[1].HasPressures || laps[1].Wet {
		t.Errorf("second lap: %+v", laps[1])
	}
}
