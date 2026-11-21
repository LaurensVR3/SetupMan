package ac

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = "[ABS]\r\nVALUE=1\r\n\r\n[CAMBER_LF]\r\nVALUE=-38\r\n\r\n[CAR]\r\nMODEL=ks_mazda_mx5_cup\r\n\r\n" +
	"[FUEL]\r\nVALUE=10\r\n\r\n[PRESSURE_LF]\r\nVALUE=17\r\n\r\n[WING_1]\r\nVALUE=3\r\n\r\n[BRAKE_DUCT_RR]\r\nVALUE=2\r\n\r\n" +
	"[ABOUT]\r\nAUTHOR=Someone\r\n\r\n[__EXT_PATCH]\r\nVERSION=0.2.6\r\n"

func TestParse(t *testing.T) {
	fields, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]string{}
	for _, f := range fields {
		by[f.Path] = f.Label + "|" + f.Section + "|" + f.Value + "|" + f.Unit
		if (f.Path == "CAR.MODEL" || f.Path == "ABOUT.AUTHOR" || f.Path == "__EXT_PATCH.VERSION") != f.Excluded {
			t.Errorf("%s: excluded = %v", f.Path, f.Excluded)
		}
	}
	for path, want := range map[string]string{
		"PRESSURE_LF":   "Tyre pressure FL|Tyres|17|psi",
		"CAMBER_LF":     "Camber FL|Alignment|-38|",
		"FUEL":          "Fuel|Strategy|10|L",
		"WING_1":        "Wing 1|Aero balance|3|",
		"ABS":           "ABS|Electronics|1|",
		"BRAKE_DUCT_RR": "Brake duct RR|Other|2|",
		"ABOUT.AUTHOR":  "Author|Car|Someone|",
		"CAR.MODEL":     "Car|Car|ks_mazda_mx5_cup|",
	} {
		if by[path] != want {
			t.Errorf("%s = %q, want %q", path, by[path], want)
		}
	}
	if carModel(fields) != "ks_mazda_mx5_cup" {
		t.Error("car model not found")
	}
	if _, err := Parse([]byte("{\"carName\": \"x\"}")); err == nil {
		t.Error("JSON accepted as an AC setup")
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ks_mazda_mx5_cup", "generic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "q.ini"), []byte(sample), 0o644)
	os.WriteFile(filepath.Join(dir, "q.sp"), []byte("[PRESET_0]\r\nFUEL=0\r\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Last.ini"), []byte(sample), 0o644) // AC's copy of the loaded setup (D-157)
	g := &Game{}
	g.once.Do(func() {}) // no installed game in tests: names come from the folders
	s, err := g.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cars) != 1 || s.Cars[0].Name != "Mazda Mx5 Cup" || s.Cars[0].Tracks[0].Name != "Any track" {
		t.Fatalf("unexpected scan: %+v", s.Cars)
	}
	if len(s.Cars[0].Tracks[0].Setups) != 1 || len(s.Ignored) != 2 {
		t.Fatalf("setups %d, ignored %v", len(s.Cars[0].Tracks[0].Setups), s.Ignored)
	}
}

// The rig's AC setups, when the git-ignored sample folder is present.
func TestSamples(t *testing.T) {
	root := filepath.Join("..", "..", "..", "SampleSetups", "Assetto Corsa", "setups")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no sample setups")
	}
	g := &Game{}
	g.once.Do(func() {})
	s, err := g.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range s.Cars {
		n += c.SetupCount()
	}
	for _, i := range s.Ignored {
		if filepath.Ext(i.RelPath) != ".sp" && filepath.Base(i.RelPath) != "last.ini" {
			t.Errorf("ignored %s: %s", i.RelPath, i.Reason)
		}
	}
	if n < 30 { // 38 real setups; the other cars only have AC's last.ini (D-157)
		t.Errorf("only %d setups read", n)
	}
	t.Logf("%d cars, %d setups", len(s.Cars), n)
}

func TestTrackOnly(t *testing.T) {
	for in, want := range map[string]string{"Lime Rock Park - South Chicane": "Lime Rock Park", "Road Atlanta Grand Prix": "Road Atlanta Grand Prix", "GP Explorer - Circuit Bugatti": "GP Explorer"} {
		if got := trackOnly(in); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}
