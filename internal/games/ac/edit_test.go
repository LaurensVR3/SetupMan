package ac

import (
	"strings"
	"testing"

	"setupman/internal/game"
)

const iniSample = "[CAMBER_LF]\r\nVALUE=-39\r\n\r\n[FUEL]\r\nVALUE=30\r\n\r\n[CAR]\r\nMODEL=ks_ferrari_488_gt3\r\n\r\n[ABOUT]\r\nAUTHOR=someone\r\n"

func TestEditKeepsEverythingElse(t *testing.T) {
	out, err := Edit([]byte(iniSample), map[string]string{"CAMBER_LF": "-41", "FUEL": "45"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(iniSample, "VALUE=-39", "VALUE=-41", 1), "VALUE=30", "VALUE=45", 1)
	if string(out) != want {
		t.Fatalf("got\n%q\nwant\n%q", out, want)
	}
	f, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range f {
		if x.Path == "CAMBER_LF" && (x.Num != -41 || !x.Signed) {
			t.Errorf("camber after edit: %+v", x)
		}
		if x.Path == "CAR.MODEL" && x.Signed {
			t.Error("car model must not be changeable")
		}
	}
}

func TestEditRefuses(t *testing.T) {
	for name, edits := range map[string]map[string]string{
		"car":         {"CAR.MODEL": "1"},
		"author":      {"ABOUT.AUTHOR": "1"},
		"missing":     {"WING_1": "3"},
		"not a whole": {"FUEL": "30.5"},
	} {
		if _, err := Edit([]byte(iniSample), edits); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestMatchSetupByFuel(t *testing.T) {
	g := &Game{}
	a := []byte("[CAR]\nMODEL=x\n[FUEL]\nVALUE=30\n[WING_1]\nVALUE=2\n")
	b := []byte("[CAR]\nMODEL=x\n[FUEL]\nVALUE=60\n[WING_1]\nVALUE=2\n")
	c := []byte("[CAR]\nMODEL=x\n[FUEL]\nVALUE=61\n[WING_1]\nVALUE=3\n")
	tel := func(fuel float64) game.Telemetry { return game.Telemetry{Fuel: fuel} }
	if i := g.MatchSetup(tel(30.2), [][]byte{a, b}); len(i) != 1 || i[0] != 0 {
		t.Errorf("30 L: got %v, want [0]", i)
	}
	if i := g.MatchSetup(tel(60.5), [][]byte{a, b, c}); len(i) != 2 || i[0] != 1 || i[1] != 2 {
		t.Errorf("60.5 L fits two setups: got %v, want [1 2] (user picks)", i)
	}
	if i := g.MatchSetup(tel(45), [][]byte{a}); len(i) != 1 || i[0] != 0 {
		t.Errorf("only one setup: got %v, want [0]", i)
	}
}
