package acc

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/game"
)

func baseJSON(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hashTree returns a hash per file, to prove the scan changed nothing.
func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func findTrack(res *game.Scan, car, track string) *game.Track {
	for i := range res.Cars {
		if res.Cars[i].ID == car {
			for j := range res.Cars[i].Tracks {
				if res.Cars[i].Tracks[j].ID == track {
					return &res.Cars[i].Tracks[j]
				}
			}
		}
	}
	return nil
}

func TestScanFolder(t *testing.T) {
	root := t.TempDir()
	base := baseJSON(t)
	write(t, root, "test_car_gt3/Spa/q.json", base)
	write(t, root, "test_car_gt3/Spa/q copy.json", base)
	write(t, root, "test_car_gt3/Spa/r.json", strings.Replace(base, `"brakeBias": 40`, `"brakeBias": 42`, 1))
	write(t, root, "test_car_gt3/Monza/m.json", base)
	write(t, root, "test_car_gt3/Spa/notes.pdf", "pdf")
	write(t, root, "test_car_gt3/Spa/broken.json", `{"carName":`)
	write(t, root, "test_car_gt3/Spa/old/x.json", base)
	write(t, root, "test_car_gt3/loose.json", base)
	write(t, root, "readme.txt", "hi")
	write(t, root, "ferrari_296_gt3/Spa/wrong.json", base) // carName says test_car_gt3
	os.MkdirAll(filepath.Join(root, "test_car_gt3", "Empty"), 0o755)
	os.MkdirAll(filepath.Join(root, "empty_car", "Spa"), 0o755)

	before := hashTree(t, root)
	res, err := ScanFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	after := hashTree(t, root)
	if len(before) != len(after) {
		t.Fatalf("file count changed: %d -> %d", len(before), len(after))
	}
	for p, h := range before {
		if after[p] != h {
			t.Errorf("file changed by scan: %s", p)
		}
	}

	if len(res.Cars) != 2 {
		t.Fatalf("got %d cars, want 2 (empty car skipped): %+v", len(res.Cars), res.Cars)
	}
	// Sorted by readable name: "Ferrari 296 GT3" before "Test Car GT3".
	if res.Cars[0].Name != "Ferrari 296 GT3" || res.Cars[0].Class != ClassGT3 || res.Cars[0].Brand != "Ferrari" {
		t.Errorf("known car names wrong: %+v", res.Cars[0])
	}
	if res.Cars[1].Name != "Test Car GT3" || res.Cars[1].Class != ClassOther {
		t.Errorf("unknown car fallback wrong: %+v", res.Cars[1])
	}
	if len(res.Cars[1].Tracks) != 2 {
		t.Errorf("empty track folder should be skipped: %+v", res.Cars[1].Tracks)
	}

	spa := findTrack(res, "test_car_gt3", "spa")
	if spa == nil || spa.Name != "Spa-Francorchamps" || spa.Folder != "Spa" || len(spa.Setups) != 3 {
		t.Fatalf("spa track wrong: %+v", spa)
	}
	if spa.Setups[0].Name != "q" || spa.Setups[1].Name != "q copy" || spa.Setups[2].Name != "r" {
		t.Errorf("setups not sorted by name")
	}
	if len(spa.Pairs) != 3 || !spa.Pairs[0].Identical || spa.Pairs[1].Clicks != 2 {
		t.Errorf("pairs wrong: %+v", spa.Pairs)
	}

	ignored := map[string]bool{}
	for _, i := range res.Ignored {
		ignored[i.RelPath] = true
	}
	for _, want := range []string{"test_car_gt3/Spa/notes.pdf", "test_car_gt3/Spa/broken.json", "test_car_gt3/Spa/old/", "test_car_gt3/loose.json", "readme.txt"} {
		if !ignored[want] {
			t.Errorf("%s should be ignored; ignored = %+v", want, res.Ignored)
		}
	}
	if len(res.Ignored) != 5 {
		t.Errorf("got %d ignored, want 5: %+v", len(res.Ignored), res.Ignored)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].RelPath != "ferrari_296_gt3/Spa/wrong.json" {
		t.Errorf("warnings = %+v", res.Warnings)
	}
}

func TestScanFolderEmptyGivesHint(t *testing.T) {
	res, err := ScanFolder(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if res.Hint == "" || len(res.Cars) != 0 {
		t.Errorf("expected a hint for an empty folder: %+v", res)
	}
}

func TestScanFolderMissing(t *testing.T) {
	if _, err := ScanFolder(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected an error for a missing folder")
	}
}

// TestDump runs against the local ACC sample setups when they exist (they are not in git).
func TestDump(t *testing.T) {
	root := filepath.Join("..", "..", "..", "SampleSetups", "Assetto Corsa Competizione")
	if _, err := os.Stat(root); err != nil {
		t.Skip("ACC Setups dump not present")
	}
	before := hashTree(t, root)
	res, err := ScanFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	after := hashTree(t, root)
	for p, h := range before {
		if after[p] != h {
			t.Errorf("file changed by scan: %s", p)
		}
	}
	setups, tracks, identical := 0, 0, 0
	for _, c := range res.Cars {
		if c.Class == ClassOther {
			t.Errorf("car %q has no name table entry", c.ID)
		}
		for _, tr := range c.Tracks {
			tracks++
			setups += len(tr.Setups)
			if _, known := tracksTable[tr.ID]; !known {
				t.Errorf("track %q has no name table entry", tr.ID)
			}
			for _, p := range tr.Pairs {
				if p.Identical {
					identical++
				}
			}
		}
	}
	t.Logf("cars=%d tracks=%d setups=%d identicalPairs=%d ignored=%d warnings=%d",
		len(res.Cars), tracks, setups, identical, len(res.Ignored), len(res.Warnings))
	// The sample setups come from the rig and may change, so only a rough count is checked.
	if setups < 100 {
		t.Errorf("got %d setups, expected several hundred", setups)
	}
	// Every ignored file has a reason, and no readable .json setup is ignored.
	for _, ig := range res.Ignored {
		if ig.Reason == "" {
			t.Errorf("ignored without a reason: %s", ig.RelPath)
		}
		if strings.HasSuffix(ig.RelPath, ".json") && !strings.HasPrefix(ig.Reason, "invalid JSON") {
			t.Errorf("setup ignored: %+v", ig)
		}
	}
}
