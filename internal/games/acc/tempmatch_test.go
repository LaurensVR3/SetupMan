package acc

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"setupman/internal/game"
)

// pressures reads a setup's cold and first pit-strategy tyre pressures (psi).
func pressures(t *testing.T, content []byte) (cold, pit [4]float64) {
	t.Helper()
	s, err := Parse(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	real := map[string]float64{}
	for _, f := range s.Fields {
		real[f.Path] = f.RealValue()
	}
	for w := 0; w < 4; w++ {
		cold[w] = real[fmt.Sprintf("basicSetup.tyres.tyrePressure[%d]", w)]
		pit[w] = real[fmt.Sprintf(pitPressurePath, w)]
	}
	return
}

// The rig case of 2026-11-23 (D-181): the setup was made for 27 °C air; at 29.5 °C the driver set
// every pressure 0.1 psi lower (and less fuel). Without the temperatures nothing fits; with them,
// that setup is recognised.
func TestMatchSetupTemps(t *testing.T) {
	base, err := os.ReadFile("testdata/base.json")
	if err != nil {
		t.Fatal(err)
	}
	other, err := Edit(base, map[string]string{
		"basicSetup.tyres.tyrePressure[0]": "60", "basicSetup.tyres.tyrePressure[1]": "55",
		"basicSetup.tyres.tyrePressure[2]": "57", "basicSetup.tyres.tyrePressure[3]": "53",
	})
	if err != nil {
		t.Fatal(err)
	}
	cold, pit := pressures(t, base)
	var carCold, carPit [4]float64
	for w := 0; w < 4; w++ {
		carCold[w], carPit[w] = cold[w]-0.1, pit[w]-0.1
	}
	tl := game.Telemetry{Fuel: 99, TC: -1, ABS: -1, Map: -1, PitMenuKnown: true, PitPressures: carPit,
		PressuresKnown: true, Pressures: carCold, TempsKnown: true, AirTemp: 29.5, RoadTemp: 34}
	g := New()
	cands := [][]byte{other, base}
	if got := g.MatchSetupTemps(tl, cands, []float64{0, 0}); len(got) == 1 && got[0] == 1 {
		t.Fatalf("without temperatures the shifted pressures should not decide: %v", got)
	}
	if got := g.MatchSetupTemps(tl, cands, []float64{27, 27}); fmt.Sprint(got) != "[1]" {
		t.Errorf("with temperatures: got %v, want [1]", got)
	}
	// 6 °C warmer is over the limit (0.5 psi): a 0.7 psi lower set doesn't fit by the rule
	for w := 0; w < 4; w++ {
		tl.PitPressures[w], tl.Pressures[w] = pit[w]-0.7, cold[w]-0.7
	}
	tl.AirTemp = 33
	if got := g.MatchSetupTemps(tl, cands, []float64{27, 27}); fmt.Sprint(got) == "[1]" {
		t.Errorf("0.7 psi shift should not fit: got %v", got)
	}
}

func TestTempShift(t *testing.T) {
	tl := game.Telemetry{TempsKnown: true, AirTemp: 30}
	for _, c := range []struct{ setup, want float64 }{{27, -0.3}, {30, 0}, {35, 0.5}, {20, -0.5}, {0, 0}} {
		if got := tempShift(tl, c.setup); got-c.want > 0.001 || c.want-got > 0.001 {
			t.Errorf("setup %v °C: shift %.2f, want %.2f", c.setup, got, c.want)
		}
	}
}
