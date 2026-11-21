package acc

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"
)

// Every ACC car SetupMan knows has real values (D-141).
func TestCarUnitsCoverEveryCar(t *testing.T) {
	if len(carUnits) < 50 {
		t.Fatalf("only %d cars in carunits.json", len(carUnits))
	}
	for id := range cars {
		if _, ok := carUnits[id]; !ok {
			t.Errorf("no real values for %s", id)
		}
	}
}

func parseAs(t *testing.T, car string) map[string]Field {
	t.Helper()
	raw, err := os.ReadFile("testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"test_car_gt3"`), []byte(`"`+car+`"`), 1)
	s, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Field{}
	for _, f := range s.Fields {
		out[f.Path] = f
	}
	return out
}

// Values as Race Element's Ferrari 296 GT3 conversion gives them (fixture: camber 4 4 2 2, caster 10,
// tyre pressure 50, ride height 2 6 2 18).
func TestCarUnitsFerrari296(t *testing.T) {
	f := parseAs(t, "ferrari_296_gt3")
	want := map[string]struct {
		v    float64
		unit string
	}{
		"basicSetup.alignment.camber[0]":          {-3.6, "°"},   // -4 + 0.1 × 4
		"basicSetup.alignment.camber[2]":          {-3.3, "°"},   // -3.5 + 0.1 × 2
		"basicSetup.alignment.casterLF":           {10.3, "°"},   // table[10]
		"basicSetup.tyres.tyrePressure[0]":        {25.3, "psi"}, // 20.3 + 0.1 × 50
		"advancedSetup.aeroBalance.rideHeight[0]": {52, "mm"},    // 50 + 2 (front)
		"advancedSetup.aeroBalance.rideHeight[2]": {52, "mm"},    // 50 + 2 (rear)
	}
	for path, w := range want {
		got, ok := f[path]
		if !ok {
			t.Errorf("%s missing", path)
			continue
		}
		if math.Abs(got.RealValue()-w.v) > 1e-9 || got.Unit != w.unit || !got.Converted() {
			t.Errorf("%s = %v %q (converted %v), want %v %q", path, got.RealValue(), got.Unit, got.Converted(), w.v, w.unit)
		}
	}
	// Dampers are shown in clicks by the game itself: no conversion.
	for p, fl := range f {
		if strings.Contains(p, "dampers") && fl.Converted() {
			t.Errorf("%s converted: %+v", p, fl)
		}
	}
}

// GT4-type tyres start at 17.0 psi, not 20.3.
func TestCarUnitsGT4Pressure(t *testing.T) {
	f := parseAs(t, "bmw_m4_gt4")
	if got := f["basicSetup.tyres.tyrePressure[0]"].RealValue(); math.Abs(got-22.0) > 1e-9 {
		t.Errorf("BMW M4 GT4 pressure for 50 clicks = %v, want 22.0", got)
	}
	// An unknown car keeps the clicks (and the GT3 pressure scale, which is the same for every GT3).
	u := parseAs(t, "test_car_gt3")
	if u["basicSetup.alignment.camber[0]"].Converted() {
		t.Error("unknown car got converted camber")
	}
}

// Every car number maps to a car SetupMan knows (D-142).
func TestCarModelsKnown(t *testing.T) {
	for n, id := range carModels {
		if _, ok := cars[id]; !ok {
			t.Errorf("car number %d → %q is not in the car list", n, id)
		}
	}
	if CarByModel(32) != "ferrari_296_gt3" || CarByModel(999) != "" {
		t.Error("lookup")
	}
}

// Checked on the rig (D-151): the Porsche 718 GT4's ride height starts at 111 mm front and 99 mm
// rear (front 0 and rear 6 clicks showed 111 and 105 mm in ACC 1.10).
func TestCarUnitsPorscheGT4RideHeight(t *testing.T) {
	f := parseAs(t, "porsche_718_cayman_gt4_mr") // base.json: ride height clicks 2 (front), 2 (rear)
	if got := f["advancedSetup.aeroBalance.rideHeight[0]"].RealValue(); got != 113 {
		t.Errorf("front ride height for 2 clicks = %v, want 113", got)
	}
	if got := f["advancedSetup.aeroBalance.rideHeight[2]"].RealValue(); got != 101 {
		t.Errorf("rear ride height for 2 clicks = %v, want 101", got)
	}
	// The mm depend on the track's BoP, so the setup page says they can be a few mm off (D-158).
	if f["advancedSetup.aeroBalance.rideHeight[0]"].Note == "" || f["basicSetup.alignment.camber[0]"].Note != "" {
		t.Error("only ride heights carry the BoP note")
	}
}

// ACC's Aero tab shows the rear box left of the front box (rig 2026-11-21): the rear column comes first.
func TestAeroRearFirst(t *testing.T) {
	raw, _ := os.ReadFile("testdata/base.json")
	s, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fields {
		if f.Section == "Aero" && f.Col != "" {
			if f.Col != "Rear" {
				t.Errorf("first Aero column = %q (%s), want Rear", f.Col, f.Path)
			}
			return
		}
	}
	t.Fatal("no Aero field with a column")
}
