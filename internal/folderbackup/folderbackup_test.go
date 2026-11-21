package folderbackup

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

type entry struct {
	path    string // case-sensitive
	isDir   bool
	content string
	mod     int64 // 100 ns units
}

// tree captures everything about a folder: names (with case), folders, bytes and dates.
func tree(t *testing.T, root string) []entry {
	t.Helper()
	var out []entry
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		e := entry{path: filepath.ToSlash(rel), isDir: d.IsDir()}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			info, _ := d.Info()
			e.content, e.mod = string(b), info.ModTime().UnixNano()/100
		}
		out = append(out, e)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func write(t *testing.T, root, rel, content string, mod time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, mod, mod)
}

func sample(t *testing.T) string {
	root := filepath.Join(t.TempDir(), "Setups")
	d := time.Date(2023, 7, 17, 22, 27, 13, 123456700, time.Local)
	write(t, root, "ferrari_296_gt3/Spa/quali.json", `{"q":1}`, d)
	write(t, root, "ferrari_296_gt3/Spa/race.json", `{"r":1}`, d.Add(time.Hour))
	write(t, root, "ferrari_296_gt3/Monza/m.json", `{"m":1}`, d.Add(2*time.Hour))
	write(t, root, "ferrari_488_gt3_evo/Spa/Untitled+Document.pdf", "%PDF", d.Add(3*time.Hour))
	os.MkdirAll(filepath.Join(root, "alpine_a110_gt4", "Barcelona"), 0o755) // empty folders
	return root
}

func TestBackupAndExactRestore(t *testing.T) {
	root := sample(t)
	original := tree(t, root)
	s := &Store{Dir: filepath.Join(t.TempDir(), "backups")}

	b, err := s.Create("acc", root, ReasonBeforeManaging)
	if err != nil {
		t.Fatal(err)
	}
	if b.Files != 4 || b.Reason != ReasonBeforeManaging {
		t.Fatalf("summary: %+v", b)
	}

	// "2355923 changes": edit, add, delete, rename, new folders, other dates, other case.
	now := time.Now()
	write(t, root, "ferrari_296_gt3/Spa/quali.json", `{"q":999}`, now)    // edited
	write(t, root, "ferrari_296_gt3/Spa/new save.json", `{"n":1}`, now)   // added
	os.Remove(filepath.Join(root, "ferrari_296_gt3", "Monza", "m.json"))  // deleted
	os.Rename(filepath.Join(root, "ferrari_296_gt3", "Spa", "race.json"), // renamed
		filepath.Join(root, "ferrari_296_gt3", "Spa", "ABC R 2327.json"))
	os.Chtimes(filepath.Join(root, "ferrari_488_gt3_evo", "Spa", "Untitled+Document.pdf"), now, now) // only the date
	write(t, root, "bmw_m4_gt3/Zolder/z.json", `{"z":1}`, now)                                       // new car folder
	os.Remove(filepath.Join(root, "alpine_a110_gt4", "Barcelona"))                                   // empty folder gone
	os.Rename(filepath.Join(root, "ferrari_296_gt3", "Monza"), filepath.Join(root, "ferrari_296_gt3", "tmpx"))
	os.Rename(filepath.Join(root, "ferrari_296_gt3", "tmpx"), filepath.Join(root, "ferrari_296_gt3", "monza")) // folder case

	plan, err := s.PlanRestore("acc", b.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	// quali (content) + pdf (date) changed; race + m come back; new save, ABC R 2327, z go away.
	if plan.Change != 2 || plan.Return != 2 || plan.Remove != 3 || plan.Same != 0 {
		t.Errorf("plan: %+v", plan)
	}
	changed := tree(t, root)

	res, err := s.Restore("acc", b.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree(t, root); !reflect.DeepEqual(got, original) {
		t.Fatalf("folder not identical after restore:\n got  %+v\n want %+v", got, original)
	}

	// The automatic safety backup holds the changed folder, and restoring it brings that back exactly.
	list, _ := s.List("acc")
	if len(list) != 2 || list[0].ID != res.SafetyBackup {
		t.Fatalf("list: %+v", list)
	}
	if _, err := s.Restore("acc", res.SafetyBackup, root); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, root); !reflect.DeepEqual(got, changed) {
		t.Fatalf("restoring the safety backup did not bring the changed folder back:\n got  %+v\n want %+v", got, changed)
	}
}

func TestDamagedBackupIsNotRestored(t *testing.T) {
	root := sample(t)
	s := &Store{Dir: filepath.Join(t.TempDir(), "backups")}
	b, err := s.Create("acc", root, ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	// Replace the ZIP with one whose file content no longer matches the manifest.
	p, _ := s.zipPath("acc", b.ID)
	m, _ := readManifest(p)
	out, _ := os.Create(p)
	zw := zip.NewWriter(out)
	w, _ := zw.Create("manifest.json")
	w.Write(mustJSON(m))
	for _, f := range m.Files {
		w, _ := zw.Create("files/" + f.Path)
		w.Write([]byte("tampered"))
	}
	zw.Close()
	out.Close()

	write(t, root, "ferrari_296_gt3/Spa/quali.json", `{"changed":1}`, time.Now())
	before := tree(t, root)
	if _, err := s.Restore("acc", b.ID, root); err == nil {
		t.Fatal("damaged backup was restored")
	}
	if !reflect.DeepEqual(tree(t, root), before) {
		t.Error("folder changed although the backup was damaged")
	}
}

// TestDumpCopyRoundTrip copies the local sample setups (only reading them) and does a full
// backup → many changes → restore cycle on the copy.
func TestDumpCopyRoundTrip(t *testing.T) {
	src := filepath.Join("..", "..", "SampleSetups", "Assetto Corsa Competizione")
	if _, err := os.Stat(src); err != nil {
		t.Skip("dump not present")
	}
	root := filepath.Join(t.TempDir(), "Setups")
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(src, p)
		dst := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, _ := os.ReadFile(p)
		info, _ := d.Info()
		os.WriteFile(dst, b, 0o644)
		return os.Chtimes(dst, info.ModTime(), info.ModTime())
	})
	original := tree(t, root)
	s := &Store{Dir: filepath.Join(t.TempDir(), "backups")}
	b, err := s.Create("acc", root, ReasonBeforeManaging)
	if err != nil {
		t.Fatal(err)
	}
	// Remove all but one setup of every track folder, add saves, edit one.
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() && filepath.Ext(p) == ".json" {
			if n%3 != 0 {
				os.Remove(p)
			} else {
				os.WriteFile(p, []byte(`{"edited":true}`), 0o644)
			}
			n++
		}
		return nil
	})
	write(t, root, "ferrari_296_gt3/Spa/ABC R 1.10.4 2327 TC25.json", `{}`, time.Now())
	if _, err := s.Restore("acc", b.ID, root); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, root); !reflect.DeepEqual(got, original) {
		t.Fatalf("dump copy not identical after restore (%d vs %d entries)", len(got), len(original))
	}
	t.Logf("restored %d entries exactly (%d files in backup)", len(original), b.Files)
}

func TestUnknownBackup(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.PlanRestore("acc", "../../etc", t.TempDir()); err != ErrNotFound {
		t.Errorf("got %v", err)
	}
	if list, err := s.List("acc"); err != nil || len(list) != 0 {
		t.Errorf("empty list: %v %v", list, err)
	}
}

// A backup is always restored into the setups folder in use now, never into the folder written
// inside it (D-138): e.g. a backup from another PC, or from before the folder setting changed.
func TestRestoreGoesIntoCurrentFolder(t *testing.T) {
	old := sample(t)
	s := &Store{Dir: filepath.Join(t.TempDir(), "backups")}
	b, err := s.Create("acc", old, ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	write(t, old, "ferrari_296_gt3/Spa/later.json", `{"l":1}`, time.Now())
	oldBefore := tree(t, old)

	cur := filepath.Join(t.TempDir(), "Setups")
	os.MkdirAll(cur, 0o755)
	plan, err := s.PlanRestore("acc", b.ID, cur)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OtherFolder || plan.Folder != cur || plan.Return != 4 {
		t.Errorf("plan: %+v", plan)
	}
	if _, err := s.Restore("acc", b.ID, cur); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tree(t, old), oldBefore) {
		t.Error("the folder named inside the backup was changed")
	}
	if _, err := os.Stat(filepath.Join(cur, "ferrari_296_gt3", "Spa", "quali.json")); err != nil {
		t.Errorf("not restored into the current folder: %v", err)
	}
}

// A backup whose names point outside the folder, or that belongs to another game, is refused.
func TestRestoreRefusesBadBackups(t *testing.T) {
	root := sample(t)
	s := &Store{Dir: filepath.Join(t.TempDir(), "backups")}
	b, err := s.Create("acc", root, ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the ZIP with a file name that climbs out of the folder (valid hashes).
	p, _ := s.zipPath("acc", b.ID)
	m, _ := readManifest(p)
	m.Files = []FileEntry{{Path: "../evil.json", Size: 2, SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", Modified: time.Now()}}
	out, _ := os.Create(p)
	zw := zip.NewWriter(out)
	w, _ := zw.Create("manifest.json")
	w.Write(mustJSON(m))
	w, _ = zw.Create("../evil.json")
	w.Write([]byte("{}"))
	zw.Close()
	out.Close()
	before := tree(t, root)
	if _, err := s.Restore("acc", b.ID, root); err == nil {
		t.Fatal("a backup with ../ in a file name was restored")
	}
	if _, err := s.PlanRestore("acc", b.ID, root); err == nil {
		t.Error("a backup with ../ in a file name was offered")
	}
	if !reflect.DeepEqual(tree(t, root), before) {
		t.Error("folder changed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evil.json")); err == nil {
		t.Error("file written outside the folder")
	}

	// Another game's backup.
	m2, _ := readManifest(p)
	if err := checkManifest(&Manifest{Game: "ac", Files: m2.Files[:0]}, "acc"); err == nil {
		t.Error("another game's backup was accepted")
	}
	for _, bad := range []string{"", "/abs.json", `a\b.json`, "C:x.json", "a/../b.json", "./a.json", "a//b.json"} {
		if safeRel(bad) {
			t.Errorf("safeRel(%q) = true", bad)
		}
	}
	if !safeRel("ferrari_296_gt3/Spa/ABC R 2327.json") {
		t.Error("a normal name was refused")
	}
}
