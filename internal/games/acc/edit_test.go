package acc

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setupman/internal/compare"
)

func fieldValue(t *testing.T, content []byte, path string) string {
	t.Helper()
	s, err := Parse(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fields {
		if f.Path == path {
			return f.Value
		}
	}
	t.Fatalf("no field %s", path)
	return ""
}

func TestEdit(t *testing.T) {
	base, _ := os.ReadFile("testdata/base.json")
	out, err := Edit(base, map[string]string{
		"advancedSetup.mechanicalBalance.brakeBias":               "43",
		"basicSetup.tyres.tyrePressure[2]":                        "47",
		"basicSetup.strategy.pitStrategy#1.fuelToAdd":             "60",
		"basicSetup.strategy.pitStrategy#1.tyres.tyrePressure[0]": "44",
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"advancedSetup.mechanicalBalance.brakeBias":               "43",
		"basicSetup.tyres.tyrePressure[2]":                        "47",
		"basicSetup.tyres.tyrePressure[1]":                        "50", // untouched
		"basicSetup.strategy.pitStrategy#1.fuelToAdd":             "60",
		"basicSetup.strategy.pitStrategy#1.tyres.tyrePressure[0]": "44",
		"basicSetup.strategy.fuelPerLap":                          "3.0", // float text kept exactly
	} {
		if got := fieldValue(t, out, path); got != want {
			t.Errorf("%s = %s, want %s", path, got, want)
		}
	}

	for _, bad := range []map[string]string{
		{"basicSetup.strategy.fuelPerLap": "4"},      // calculated by the game
		{"advancedSetup.aeroBalance.rearWing": "-1"}, // negative
		{"advancedSetup.aeroBalance.rearWing": "x"},  // not a number
		{"advancedSetup.nothing": "1"},               // unknown
		{"basicSetup.tyres.tyrePressure[9]": "1"},    // out of range
		{"basicSetup.strategy.pitStrategy#5.fuelToAdd": "1"},
	} {
		if _, err := Edit(base, bad); err == nil {
			t.Errorf("edit %v accepted", bad)
		}
	}
}

// Without edits, every setup in the dump comes back with the same values (and, for files
// ACC wrote, byte-identical layout).
func TestEditRoundTripDump(t *testing.T) {
	root := filepath.Join("..", "..", "..", "SampleSetups", "Assetto Corsa Competizione")
	if _, err := os.Stat(root); err != nil {
		t.Skip("dump not present")
	}
	identical, n := 0, 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		in, _ := os.ReadFile(p)
		if _, perr := Parse(bytes.NewReader(in)); perr != nil {
			return nil // not a readable setup (e.g. an empty file); the scan lists it as ignored
		}
		out, err := Edit(in, nil)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		a, _ := Parse(bytes.NewReader(in))
		b, _ := Parse(bytes.NewReader(out))
		if compare.Fingerprint(a.Fields) != compare.Fingerprint(b.Fields) || len(a.Fields) != len(b.Fields) {
			t.Errorf("%s: values changed by writing", p)
		}
		norm := func(x []byte) string { return strings.ReplaceAll(strings.TrimRight(string(x), "\r\n"), "\r\n", "\n") }
		if norm(in) == norm(out) {
			identical++
		}
		n++
		return nil
	})
	t.Logf("%d of %d files written back byte-identical (apart from line endings)", identical, n)
}
