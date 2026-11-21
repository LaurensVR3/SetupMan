package acc

import (
	"os"
	"strings"
	"testing"
)

func loadBase(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func field(t *testing.T, s *Setup, path string) Field {
	t.Helper()
	for _, f := range s.Fields {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("field %q not found", path)
	return Field{}
}

func TestParseInGameOrder(t *testing.T) {
	s, err := Parse(strings.NewReader(loadBase(t)))
	if err != nil {
		t.Fatal(err)
	}
	if s.CarName != "test_car_gt3" {
		t.Errorf("CarName = %q", s.CarName)
	}
	// The game's order (D-144): Tyres first, the settings the game doesn't show last.
	if s.Fields[0].Path != "basicSetup.tyres.tyrePressure[0]" {
		t.Errorf("first field = %q, want the front-left tyre pressure", s.Fields[0].Path)
	}
	if last := s.Fields[len(s.Fields)-1].Path; last != "trackBopType" {
		t.Errorf("last field = %q, want trackBopType", last)
	}

	f := field(t, s, "basicSetup.tyres.tyrePressure[0]")
	if f.Label != "psi FL" || f.Row != "psi" || f.Col != "Left front" || f.Section != "Tyres" || f.Kind != "int" || f.Num != 50 {
		t.Errorf("tyre pressure field wrong: %+v", f)
	}
	if f := field(t, s, "advancedSetup.aeroBalance.brakeDuct[1]"); f.Label != "Brake ducts rear" || f.Section != "Aero" || f.Col != "Rear" {
		t.Errorf("brake duct label = %q", f.Label)
	}
	if f := field(t, s, "basicSetup.strategy.fuelPerLap"); f.Kind != "float" {
		t.Errorf("fuelPerLap kind = %q, want float", f.Kind)
	}
	if f := field(t, s, "basicSetup.strategy.pitStrategy#1.tyres.tyrePressure[3]"); f.Label != "Pitstop strategy 1: Right rear" || f.Row != "Right rear" || f.Group != "Pitstop strategy" || f.Section != "Fuel & strategy" {
		t.Errorf("pit strategy field wrong: %+v", f)
	}
}

func TestExcludedFields(t *testing.T) {
	s, err := Parse(strings.NewReader(loadBase(t)))
	if err != nil {
		t.Fatal(err)
	}
	excluded := []string{
		"carName", "trackBopType", "basicSetup.electronics.telemetryLaps",
		"basicSetup.alignment.staticCamber[2]", "basicSetup.alignment.toeOutLinear[0]",
		"advancedSetup.aeroBalance.rodLength[3]",
	}
	for _, p := range excluded {
		if !field(t, s, p).Excluded {
			t.Errorf("%s should be excluded", p)
		}
	}
	for _, p := range []string{"advancedSetup.mechanicalBalance.brakeBias", "basicSetup.strategy.fuel", "basicSetup.strategy.pitStrategy#1.fuelToAdd"} {
		if field(t, s, p).Excluded {
			t.Errorf("%s should be included", p)
		}
	}
}

func TestPitStrategyPaddingIsDropped(t *testing.T) {
	base := loadBase(t)
	entry := `{ "fuelToAdd": 10, "tyres": { "tyreCompound": 0, "tyrePressure": [ 45, 45, 45, 45 ] }, "tyreSet": 2, "frontBrakePadCompound": 1, "rearBrakePadCompound": 1 }`
	other := strings.Replace(entry, `"fuelToAdd": 10`, `"fuelToAdd": 50`, 1)
	padded := strings.Replace(base, `"pitStrategy": [`, `"pitStrategy": [`+entry+`,`+other+`,`+other+`,`+other+`,`, 1)

	s, err := Parse(strings.NewReader(padded))
	if err != nil {
		t.Fatal(err)
	}
	// Distinct entries: entry (first) and other. The trailing original entry equals the first.
	count := 0
	for _, f := range s.Fields {
		if strings.HasSuffix(f.Path, ".fuelToAdd") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("got %d distinct pit strategies, want 2", count)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"broken":   `{"carName": "x", `,
		"array":    `[1,2]`,
		"no car":   `{"basicSetup": {}}`,
		"no basic": `{"carName": "x"}`,
		"trailing": `{"carName": "x", "basicSetup": {}} {}`,
	}
	for name, in := range cases {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestStripIndexes(t *testing.T) {
	if got := stripIndexes("a.b[2]"); got != "a.b" {
		t.Errorf("got %q", got)
	}
	if got := stripIndexes("x.pit#12.tyres.p[3]"); got != "x.pit.tyres.p" {
		t.Errorf("got %q", got)
	}
}
