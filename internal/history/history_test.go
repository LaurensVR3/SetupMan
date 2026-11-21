package history

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"setupman/internal/games/acc"
	"setupman/internal/store"
)

var parse = acc.New().ParseSetup

func baseJSON(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../games/acc/testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// folder is a temporary ACC setups folder that tests change between syncs.
type folder struct {
	t    *testing.T
	root string
	base string
	when time.Time
}

func newFolder(t *testing.T) *folder {
	return &folder{t: t, root: t.TempDir(), base: baseJSON(t), when: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}
}

// write creates a setup with some values changed; each file gets a later date.
func (f *folder) write(rel string, replace ...string) {
	f.t.Helper()
	content := f.base
	for i := 0; i+1 < len(replace); i += 2 {
		if !strings.Contains(content, replace[i]) {
			f.t.Fatalf("%q not in base.json", replace[i])
		}
		content = strings.Replace(content, replace[i], replace[i+1], 1)
	}
	p := filepath.Join(f.root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.when = f.when.Add(time.Hour)
	os.Chtimes(p, f.when, f.when)
}

func (f *folder) remove(rel string) {
	os.Remove(filepath.Join(f.root, filepath.FromSlash(rel)))
}

func (f *folder) rename(from, to string) {
	os.Rename(filepath.Join(f.root, filepath.FromSlash(from)), filepath.Join(f.root, filepath.FromSlash(to)))
}

func (f *folder) sync(st *store.Store) Result {
	f.t.Helper()
	return f.syncWith(st, DefaultConfig)
}

func (f *folder) syncWith(st *store.Store, cfg Config) Result {
	f.t.Helper()
	scan, err := acc.ScanFolder(f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	res, err := Sync(st, "acc", scan, parse, cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

func openStore(t *testing.T) *store.Store {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func setupNamed(t *testing.T, st *store.Store, name string) *store.Setup {
	t.Helper()
	all, _ := st.SetupsForGame("acc")
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	t.Fatalf("setup %q not found", name)
	return nil
}

const car = "test_car_gt3/Spa/"

func TestImportBuildsProbableHistory(t *testing.T) {
	f := newFolder(t)
	f.write(car + "ABC R base.json")                                                            // oldest
	f.write(car+"ABC R tweak.json", `"brakeBias": 40`, `"brakeBias": 41`)                       // ~99% → child of base
	f.write(car+"ABC Q base.json", `"fuel": 30`, `"fuel": 8`, `"rearWing": 6`, `"rearWing": 2`) // Q branch
	f.write(car+"other.json", `"tyrePressure": [ 50, 50, 50, 50 ]`, `"tyrePressure": [ 60, 61, 62, 63 ]`,
		`"camber": [ 4, 4, 2, 2 ]`, `"camber": [ 9, 9, 9, 9 ]`, `"toe": [ 20, 20, 40, 40 ]`, `"toe": [ 1, 2, 3, 4 ]`,
		`"rideHeight": [ 2, 6, 2, 18 ]`, `"rideHeight": [ 9, 9, 9, 9 ]`, `"aRBFront": 3`, `"aRBFront": 9`,
		`"rearWing": 6`, `"rearWing": 1`, `"fuel": 30`, `"fuel": 90`) // very different

	st := openStore(t)
	res := f.sync(st)
	if res.Imported != 4 {
		t.Fatalf("imported %d, want 4", res.Imported)
	}
	base := setupNamed(t, st, "ABC R base")
	tweak := setupNamed(t, st, "ABC R tweak")
	q := setupNamed(t, st, "ABC Q base")
	if base.LinkKind != store.LinkNone || base.Session != "R" || base.Meta.Initials != "ABC" {
		t.Errorf("base: %+v", base)
	}
	bv, _ := st.LatestVersion(base.ID)
	if tweak.LinkKind != store.LinkProbable || tweak.ParentVersionID != bv.ID {
		t.Errorf("tweak should probably come from base: %+v", tweak)
	}
	if q.LinkKind != store.LinkProbable || q.Session != "Q" {
		t.Errorf("Q branch: %+v", q)
	}
	if o := setupNamed(t, st, "other"); o.LinkKind != store.LinkNone {
		t.Errorf("unrelated setup linked: %+v", o)
	}
	if res := f.sync(st); !reflect.DeepEqual(res, Result{}) {
		t.Errorf("second sync without changes did something: %+v", res)
	}
}

func TestVersionBranchRenameRemove(t *testing.T) {
	f := newFolder(t)
	f.write(car + "race.json")
	st := openStore(t)
	f.sync(st)
	race := setupNamed(t, st, "race")

	// Same file, changed value → new version with a readable change.
	f.write(car+"race.json", `"rearWing": 6`, `"rearWing": 7`)
	if res := f.sync(st); res.NewVersions != 1 {
		t.Fatalf("expected a new version: %+v", res)
	}
	vs, _ := st.Versions(race.ID)
	if len(vs) != 2 || vs[1].Seq != 2 {
		t.Fatalf("versions: %+v", vs)
	}

	// Only a not-scored value changes (tyre set, D-52) → no new version, content refreshed.
	f.write(car+"race.json", `"rearWing": 6`, `"rearWing": 7`, `"tyreSet": 1,`, `"tyreSet": 5,`)
	if res := f.sync(st); res.NewVersions != 0 {
		t.Errorf("tyre set alone made a version: %+v", res)
	}
	latest, _ := st.LatestVersion(race.ID)
	if !strings.Contains(string(latest.Content), `"tyreSet": 5`) {
		t.Error("stored content not refreshed")
	}

	// Save as a new name while the old file stays → branch (D-51).
	f.write(car+"quali.json", `"rearWing": 6`, `"rearWing": 3`, `"fuel": 30`, `"fuel": 8`)
	if res := f.sync(st); res.Branches != 1 {
		t.Fatalf("expected a branch: %+v", res)
	}
	quali := setupNamed(t, st, "quali")
	if quali.LinkKind != store.LinkObserved || quali.ParentVersionID != latest.ID {
		t.Errorf("quali should branch from race v2: %+v", quali)
	}

	// Rename (old file gone in the same scan) → same setup continues (D-51).
	f.rename(car+"race.json", car+"ABC R 2327.json")
	if res := f.sync(st); res.Renamed != 1 || res.Branches != 0 || res.NewSetups != 0 {
		t.Fatalf("expected a rename: %+v", res)
	}
	renamed, _ := st.Setup(race.ID)
	if renamed.Name != "ABC R 2327" || renamed.RelPath != car+"ABC R 2327.json" || renamed.Meta.Air != 23 || renamed.Meta.Track != 27 {
		t.Errorf("renamed setup: %+v", renamed)
	}

	// File deleted → setup stays, marked as no longer in the folder.
	f.remove(car + "quali.json")
	if res := f.sync(st); res.Removed != 1 {
		t.Fatalf("expected a removal: %+v", res)
	}
	q2, _ := st.Setup(quali.ID)
	if q2 == nil || q2.RelPath != "" {
		t.Errorf("removed setup: %+v", q2)
	}
	if v, _ := st.Versions(quali.ID); len(v) != 1 {
		t.Error("history of removed setup lost")
	}

	ev, _ := st.Events(false, 50)
	kinds := map[string]bool{}
	for _, e := range ev {
		kinds[e.Kind] = true
	}
	for _, k := range []string{"imported", "new-version", "branch", "renamed", "removed"} {
		if !kinds[k] {
			t.Errorf("no %q event", k)
		}
	}
}

func TestReattachAndFresh(t *testing.T) {
	f := newFolder(t)
	f.write(car + "race.json")
	f.write(car+"quali.json", `"fuel": 30`, `"fuel": 8`)
	st := openStore(t)
	f.sync(st)
	race := setupNamed(t, st, "race")

	// The file disappears (e.g. moved to the library) and later comes back under the same name
	// (restore, or copied back): same setup again, no duplicate.
	f.remove(car + "race.json")
	f.sync(st)
	f.write(car + "race.json")
	if res := f.sync(st); res.Returned != 1 || res.NewSetups+res.Branches != 0 {
		t.Fatalf("expected the setup to come back: %+v", res)
	}
	back, _ := st.Setup(race.ID)
	if back.RelPath != car+"race.json" {
		t.Errorf("not re-attached: %+v", back)
	}
	all, _ := st.SetupsForGame("acc")
	if len(all) != 2 {
		t.Errorf("duplicates created: %d setups", len(all))
	}

	// In a managed folder, a new save from the game is marked fresh; elsewhere it is not.
	st.SetManagedTrack("acc", "test_car_gt3", "spa")
	f.write("test_car_gt3/Monza/other.json")
	f.write(car+"copied in.json", `"rearWing": 6`, `"rearWing": 3`)
	f.sync(st) // game closed: copied in, not saved in the game (D-114)
	if s := setupNamed(t, st, "copied in"); s.Fresh {
		t.Error("file copied in while the game is closed should not be fresh")
	}
	f.write(car+"new from game.json", `"rearWing": 6`, `"rearWing": 9`)
	f.syncWith(st, Config{Threshold: 80, NameThreshold: 65, GameRunning: true})
	if s := setupNamed(t, st, "new from game"); !s.Fresh {
		t.Error("save in managed folder should be fresh")
	}
	if s := setupNamed(t, st, "other"); s.Fresh {
		t.Error("save in unmanaged folder should not be fresh")
	}
}

func TestRebuildKeepsManualLinks(t *testing.T) {
	f := newFolder(t)
	f.write(car + "a.json")
	f.write(car+"b.json", `"brakeBias": 40`, `"brakeBias": 41`)
	f.write(car+"c.json", `"brakeBias": 40`, `"brakeBias": 42`)
	st := openStore(t)
	f.sync(st)
	a, b, c := setupNamed(t, st, "a"), setupNamed(t, st, "b"), setupNamed(t, st, "c")
	av, _ := st.LatestVersion(a.ID)
	bv, _ := st.LatestVersion(b.ID)

	// Preview at 99.9%: nothing links; nothing is saved.
	p, err := Rebuild(st, "acc", parse, Config{Threshold: 99.9, NameThreshold: 99.9}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Summary.Families != 3 || p.Summary.Linked != 0 || p.Summary.Changed != 2 {
		t.Errorf("preview: %+v", p.Summary)
	}
	if b2, _ := st.Setup(b.ID); b2.ParentVersionID == 0 {
		t.Error("preview changed the library")
	}

	// User links c to a by hand; a rebuild must keep it.
	if err := Relink(st, c.ID, av.ID, parse); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(st, "acc", parse, Config{Threshold: 99.9, NameThreshold: 99.9}, true); err != nil {
		t.Fatal(err)
	}
	c2, _ := st.Setup(c.ID)
	b2, _ := st.Setup(b.ID)
	if c2.LinkKind != store.LinkManual || c2.ParentVersionID != av.ID {
		t.Errorf("manual link lost: %+v", c2)
	}
	if b2.ParentVersionID != 0 || b2.LinkKind != store.LinkNone {
		t.Errorf("b should be unlinked at 99.9%%: %+v", b2)
	}

	// Cycles are refused: a cannot come from b if b came from a.
	Relink(st, b.ID, av.ID, parse)
	if err := Relink(st, a.ID, bv.ID, parse); err == nil {
		t.Error("cycle accepted")
	}

	d, err := SetupDetail(st, c.ID, parse)
	if err != nil {
		t.Fatal(err)
	}
	if d.Parent == nil || d.Parent.SetupID != a.ID || d.Family == nil || d.Family.SetupID != a.ID {
		t.Errorf("detail: parent=%+v family=%+v", d.Parent, d.Family)
	}
	for _, o := range d.Options {
		if o.SetupID == c.ID {
			t.Error("a setup can't be its own parent option")
		}
	}

	note, session := "TC shortcuts 2 and 5", "Endurance"
	s, err := UpdateInfo(st, c.ID, Info{Note: &note, Session: &session}, 45)
	if err != nil || s.Note != note || !s.SessionManual || s.Session != "Endurance" {
		t.Errorf("update info: %+v %v", s, err)
	}
	bad := "Sprint"
	if _, err := UpdateInfo(st, c.ID, Info{Session: &bad}, 45); err == nil {
		t.Error("unknown session accepted")
	}

	// Editing race length/stops re-guesses the session once the override is removed (D-69).
	auto := "auto"
	m := s.Meta
	m.RaceMinutes, m.Stops = 60, 0
	if s, _ = UpdateInfo(st, c.ID, Info{Meta: &m, Session: &auto}, 45); s.Session != "Endurance" || s.SessionManual {
		t.Errorf("60 min, no stop, limit 45 → Endurance: %+v", s)
	}
	m.Stops = 1
	if s, _ = UpdateInfo(st, c.ID, Info{Meta: &m}, 45); s.Session != "R" {
		t.Errorf("60 min, 1 stop → 30 min stints → R: %+v", s)
	}

	// The per-game setting changes the guess for setups not set by hand.
	if err := RefreshSessions(st, "acc", Config{EnduranceMinutes: 20}); err != nil {
		t.Fatal(err)
	}
	if s2, _ := st.Setup(c.ID); s2.Session != "Endurance" {
		t.Errorf("limit 20 min → 30 min stints are Endurance: %+v", s2)
	}
}

// TestDumpImport imports the local dump (not in git) and checks the counts match the experiment.
func TestDumpImport(t *testing.T) {
	root := filepath.Join("..", "..", "SampleSetups", "Assetto Corsa Competizione")
	if _, err := os.Stat(root); err != nil {
		t.Skip("dump not present")
	}
	scan, err := acc.ScanFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	res, err := Sync(st, "acc", scan, parse, DefaultConfig)
	n := 0
	for _, c := range scan.Cars {
		for _, tr := range c.Tracks {
			n += len(tr.Setups)
		}
	}
	if err != nil || res.Imported != n || n == 0 {
		t.Fatalf("import: %+v %v", res, err)
	}
	p, err := Rebuild(st, "acc", parse, DefaultConfig, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("dump at 80%% + name match 65%%: %+v", p.Summary)
	if p.Summary.Changed != 0 {
		t.Errorf("rebuild with the same settings should change nothing: %+v", p.Summary)
	}
	for _, g := range p.Groups {
		if g.Car == "ferrari_296_gt3" && g.Track == "hungaroring" {
			for _, r := range g.Roots {
				if r.Name == "LA 296 HUN Q" && (len(r.Children) != 1 || r.Children[0].Name != "LA 296 HUN R") {
					t.Errorf("LA Q/R should be one family via name match: %+v", r)
				}
			}
		}
	}
}
