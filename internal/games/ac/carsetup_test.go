package ac

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The key example from Assetto Server Manager's own test.
func TestAcdKey(t *testing.T) {
	if got := acdKey("rss_formula_rss_4"); got != "247-76-61-254-174-237-57-53" {
		t.Errorf("key = %s", got)
	}
}

// acdBuild makes a data.acd the way the game stores it (the reverse of acdFile), for tests.
func acdBuild(folder string, files map[string]string) []byte {
	key := acdKey(folder)
	var out []byte
	put := func(v int32) { out = binary.LittleEndian.AppendUint32(out, uint32(v)) }
	put(-1111)
	put(0)
	for name, text := range files {
		put(int32(len(name)))
		out = append(out, name...)
		put(int32(len(text)))
		x := 0
		for _, c := range []byte(text) {
			out = append(out, c+key[x%len(key)], 0, 0, 0)
			x++
		}
	}
	return out
}

const testSetupINI = `[CAMBER_LF]
SHOW_CLICKS=0
MIN=-40
MAX=0
STEP=1
[TOE_OUT_LF]
SHOW_CLICKS=2
MIN=-0.4
MAX=0.4
STEP=0.05
[ARB_FRONT]
SHOW_CLICKS=1
MIN=0
MAX=10
STEP=5000
[FRONT_BIAS]
SHOW_CLICKS=0
MIN=50
MAX=70
STEP=1
[GEAR_1]
RATIOS=gears1.rto
[DISPLAY_METHOD]
SHOW_DIGIT=1
`

func TestAcdRoundTrip(t *testing.T) {
	data := acdBuild("ks_test_car", map[string]string{"car.ini": "[INFO]\nSCREEN_NAME=Test\n", "setup.ini": testSetupINI})
	got, err := acdFile(data, "ks_test_car", "setup.ini")
	if err != nil || string(got) != testSetupINI {
		t.Fatalf("setup.ini = %q, %v", got, err)
	}
	if _, err := acdFile(data, "ks_test_car", "missing.ini"); err == nil {
		t.Error("missing file found")
	}
}

// Stored numbers become what the game shows, per SHOW_CLICKS mode; read from an installed car.
func TestRealValuesFromInstalledCar(t *testing.T) {
	content := filepath.Join(t.TempDir(), "content")
	car := filepath.Join(content, "cars", "ks_test_car")
	os.MkdirAll(car, 0o755)
	os.WriteFile(filepath.Join(car, "data.acd"), acdBuild("ks_test_car", map[string]string{"setup.ini": testSetupINI}), 0o644)

	g := New()
	g.once.Do(func() {})
	g.content = content
	fields, err := g.ParseSetup([]byte("[CAR]\nMODEL=ks_test_car\n[CAMBER_LF]\nVALUE=-27\n[TOE_OUT_LF]\nVALUE=3\n[ARB_FRONT]\nVALUE=4\n[FRONT_BIAS]\nVALUE=62\n[GEAR_1]\nVALUE=2\n[PRESSURE_LF]\nVALUE=26\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		v    float64
		unit string
	}{
		"CAMBER_LF":   {-2.7, "°"}, // tenths of a degree
		"TOE_OUT_LF":  {-0.25, ""}, // MIN + 3 × STEP
		"ARB_FRONT":   {20000, ""}, // 4 × STEP
		"FRONT_BIAS":  {62, "%"},   // the value itself
		"PRESSURE_LF": {26, "psi"}, // as before
	}
	byPath := map[string]int{}
	for i, f := range fields {
		byPath[f.Path] = i
	}
	for p, w := range want {
		f := fields[byPath[p]]
		if math.Abs(f.RealValue()-w.v) > 1e-9 || f.Unit != w.unit {
			t.Errorf("%s = %v %q, want %v %q", p, f.RealValue(), f.Unit, w.v, w.unit)
		}
	}
	if gear := fields[byPath["GEAR_1"]]; gear.Converted() {
		t.Errorf("gear ratio converted: %+v", gear)
	}
}

// Without the installed game the values stay as stored.
func TestRealValuesWithoutGame(t *testing.T) {
	g := New()
	g.once.Do(func() {})
	g.content = ""
	fields, err := g.ParseSetup([]byte("[CAR]\nMODEL=ks_test_car\n[CAMBER_LF]\nVALUE=-27\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.Path == "CAMBER_LF" && f.Converted() {
			t.Errorf("converted without the game: %+v", f)
		}
	}
}
