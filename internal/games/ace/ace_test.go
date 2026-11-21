package ace

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// A tiny protobuf writer for test files.
type pb []byte

func (p pb) tag(field, wire int) pb { return binary.AppendUvarint(p, uint64(field<<3|wire)) }
func (p pb) f32(field int, v float32) pb {
	return binary.LittleEndian.AppendUint32(p.tag(field, 5), math.Float32bits(v))
}
func (p pb) msg(field int, m pb) pb {
	return append(binary.AppendUvarint(p.tag(field, 2), uint64(len(m))), m...)
}
func (p pb) str(field int, s string) pb { return p.msg(field, pb(s)) }

func testFile() []byte {
	var arb pb
	arb = binary.LittleEndian.AppendUint32(arb, math.Float32bits(62200))
	arb = binary.LittleEndian.AppendUint32(arb, math.Float32bits(57400))
	f := pb{}.msg(1, pb{}.msg(1, arb).f32(2, 15).msg(3, pb{}.f32(1, 61.2).f32(2, 100)))
	for i := 0; i < 4; i++ {
		f = f.msg(2, pb{}.f32(1, 300000))
	}
	for i := 0; i < 4; i++ {
		f = f.msg(3, pb{}.f32(1, 14).f32(3, 43))
	}
	for _, p := range []float32{25.3, 26.4, 24.4, 25.4} {
		f = f.msg(4, pb{}.f32(1, p).f32(2, -3))
	}
	f = f.msg(5, pb{}.f32(1, 5).f32(2, 3)).msg(6, pb{}.f32(2, 50).f32(5, 4)).msg(7, pb{}.f32(1, 14))
	return f.str(9, "ks_ferrari_296_gt3_preset_mech_1")
}

func TestParse(t *testing.T) {
	fields, err := Parse(testFile())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]string{}
	for _, f := range fields {
		by[f.Path] = f.Label + "|" + f.Value + "|" + f.Unit
	}
	for path, want := range map[string]string{
		"1.1[0]":   "Anti-roll bar front|62200|N/m",
		"1.3.1":    "Brake bias|61.2|%",
		"4[0].1":   "Tyre pressure FL|25.3|psi",
		"4[3].1":   "Tyre pressure RR|25.4|psi",
		"4[1].2":   "Camber FR|-3|°",
		"4[2].3":   "Toe RL|0|°", // left out of the file because it is 0
		"3[0].3":   "Rebound FL|43|",
		"5.1":      "TC|5|",
		"6.5":      "Rear wing|4|",
		"7.1":      "Fuel|14|L",
		"9":        "Car preset|ks_ferrari_296_gt3_preset_mech_1|",
		"1.4.3":    "Differential preload|0|Nm",
		"2[2].2.2": "Bump stop rate RL|0|N/m",
	} {
		if by[path] != want {
			t.Errorf("%s = %q, want %q", path, by[path], want)
		}
	}
	if _, err := Parse([]byte("[ABS]\nVALUE=1\n")); err == nil {
		t.Error("an INI file was accepted")
	}
}

func TestCarInfo(t *testing.T) {
	for name, class := range map[string]string{"Ferrari 296 GT3": "GT3", "Mazda MX-5 ND Cup": "Cup", "Porsche 718 Cayman GT4 Clubsport": "GT4", "KTM X Bow GT2": "GT2", "Alfa Romeo Giulia": "Cars"} {
		if c := carInfo(name); c.Class != class || c.Name != name {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

// The rig's AC EVO setups, when the git-ignored sample folder is present: every file reads, and
// all have the same settings.
func TestSamples(t *testing.T) {
	root := filepath.Join("..", "..", "..", "SampleSetups", "Assetto Corsa Evo", "Car Setups")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no sample setups")
	}
	s, err := (&Game{}).Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Ignored) > 0 {
		t.Errorf("ignored: %v", s.Ignored)
	}
	n := 0
	for _, c := range s.Cars {
		for _, tr := range c.Tracks {
			for _, st := range tr.Setups {
				n++
				for _, f := range st.Fields {
					if f.Section == "Other" {
						t.Errorf("%s: unknown field %s = %s", st.RelPath, f.Path, f.Value)
					}
				}
			}
		}
	}
	if n < 10 {
		t.Errorf("only %d setups read", n)
	}
}
