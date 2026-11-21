package acr

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A tiny writer for test save files.
type w []byte

func (b w) i32(v int32) w { return binary.LittleEndian.AppendUint32(b, uint32(v)) }
func (b w) i64(v int64) w { return binary.LittleEndian.AppendUint64(b, uint64(v)) }
func (b w) str(s string) w {
	if s == "" {
		return b.i32(0)
	}
	return append(b.i32(int32(len(s)+1)), append([]byte(s), 0)...)
}
func (b w) list(ss ...string) w {
	b = b.i32(int32(len(ss)))
	for _, s := range ss {
		b = b.str(s)
	}
	return b
}

func record(name, track string, pressure string) w {
	saved := time.Date(2025, 12, 7, 11, 25, 18, 0, time.UTC)
	ticks := saved.UnixNano()/100 + 621355968000000000
	b := w{}.i32(0x12)
	b = append(b, 0xff)
	b = b.i32(1).str(name).i32(0).i64(ticks).str("0.2.2.73152").str("HyundaiI20NRally2").str(track)
	b = b.list("Tarmac").list("Clear").list("SingleStage_Tag", "Soft_Tag")
	b = b.i32(3).str("Wheels.FrontLeft.TyrePressure").str(pressure).
		str("Differentials.Front.LSDPreload").str("85.000000").
		str("Brakes.FrontLeft.Disc").str("(Discs:Brembo_09_355x32)")
	return b
}

func saveFile(recs ...w) []byte {
	body := w{}.i32(1).str("HyundaiI20NRally2").i32(int32(len(recs)))
	for _, r := range recs {
		body = append(body, r...)
	}
	b := append(w("GVAS"), make([]byte, 40)...)
	b = b.str("/Script/dmengine.SaveGameSlot").str("CarSetupsSaveGameData").i64(int64(len(body)))
	return append(b, body...)
}

func TestReadSave(t *testing.T) {
	recs, err := ReadSave(saveFile(record("YT", "Munster", "32.000000"), record("Wet", "Munster", "28.500000")))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Name != "YT" || recs[1].Track != "Munster" || recs[0].Saved.Year() != 2025 {
		t.Fatalf("unexpected: %+v", recs)
	}
	// Each record can be read back on its own (that is what the library stores).
	r, err := ReadRecord(recs[1].Raw)
	if err != nil || r.Name != "Wet" {
		t.Fatalf("record: %+v %v", r, err)
	}
	by := map[string]string{}
	for _, f := range fields(r) {
		by[f.Path] = f.Label + "|" + f.Section + "|" + f.Value
	}
	for path, want := range map[string]string{
		"Wheels.FrontLeft.TyrePressure":  "Tyre pressure FL|Tyres|28.5",
		"Differentials.Front.LSDPreload": "LSD preload front|Drivetrain|85",
		"Brakes.FrontLeft.Disc":          "Disc FL|Brakes|Brembo 09 355x32",
		"Info.Surface":                   "Surface|Stage|Tarmac",
		"Info.Tags":                      "Tags|Stage|Single Stage, Soft",
	} {
		if by[path] != want {
			t.Errorf("%s = %q, want %q", path, by[path], want)
		}
	}
	if _, err := ReadSave([]byte("GVAS nothing here")); err == nil {
		t.Error("broken file accepted")
	}
}

func TestScanAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, SaveFile), saveFile(record("YT", "Munster", "32"), record("YT", "Munster", "30")), 0o644)
	g := New()
	s, err := g.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cars) != 1 || s.Cars[0].Name != "Hyundai I20 N Rally2" || s.Cars[0].Class != "Rally2" {
		t.Fatalf("cars: %d, first %q class %q", len(s.Cars), s.Cars[0].Name, s.Cars[0].Class)
	}
	st := s.Cars[0].Tracks[0].Setups
	if len(st) != 2 || st[0].Name != "YT" || st[1].Name != "YT (2)" {
		t.Fatalf("setups: %v %v", st[0].Name, st[1].Name)
	}
	if g.ReadOnlyReason() == "" {
		t.Error("AC Rally must be read-only")
	}
}

func TestWords(t *testing.T) {
	for in, want := range map[string]string{"LSDPreload": "LSD preload", "TyrePressure": "Tyre pressure", "ARBStiffness": "ARB stiffness", "ABS": "ABS"} {
		if got := words(in); got != want {
			t.Errorf("%s → %q, want %q", in, got, want)
		}
	}
}

func TestSample(t *testing.T) {
	p := filepath.Join("..", "..", "..", "SampleSetups", "Assetto Corsa Rally", SaveFile)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skip("no sample save")
	}
	recs, err := ReadSave(b)
	if err != nil || len(recs) == 0 {
		t.Fatalf("%v (%d setups)", err, len(recs))
	}
	if len(recs[0].Values) != 48 {
		t.Errorf("%d values", len(recs[0].Values))
	}
}
