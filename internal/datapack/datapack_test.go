package datapack

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func samplePack(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	add := func(name, content string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(f, content)
	}
	add("setups/1/1.json", `{"carName":"ferrari_296_gt3"}`)
	add("setups/1/2.json", `{"carName":"ferrari_296_gt3","v":2}`)
	add("telemetry/1.ld", "ld bytes")
	add("telemetry/1.ldx", "<LDXFile/>")
	add("extra/1.smx.gz", "gz bytes")
	m := Manifest{By: "ABC", App: "0.25.0",
		Setups: []Setup{{Key: "s1", Game: "acc", Car: "ferrari_296_gt3", Track: "imola", Name: "ABC R 3040",
			Versions: []Version{{Key: "v1", File: "setups/1/1.json"}, {Key: "v2", File: "setups/1/2.json"}}}},
		Sessions: []Session{{Key: "x1", Game: "acc", Car: "ferrari_296_gt3", Track: "imola",
			Laps: []Lap{{N: 12, TimeMs: 100308, Valid: true, Full: true, Setup: "s1", SetupV: "v2", LogLap: 1}},
			Log:  &Log{LD: "telemetry/1.ld", LDX: "telemetry/1.ldx", Extra: "extra/1.smx.gz"}}}}
	if err := w.Close(m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	data := samplePack(t)
	p, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	m := p.Manifest
	if m.Kind != Kind || m.Format != Format || len(m.ID) != 24 || m.By != "ABC" || m.Created.IsZero() {
		t.Fatalf("manifest: %+v", m)
	}
	if len(m.Setups) != 1 || len(m.Setups[0].Versions) != 2 || m.Sessions[0].Laps[0].TimeMs != 100308 {
		t.Fatalf("contents: %+v", m)
	}
	if b, err := p.ReadFile("setups/1/2.json"); err != nil || !strings.Contains(string(b), `"v":2`) {
		t.Fatalf("setup file: %q %v", b, err)
	}
	if _, err := p.ReadFile("../library.db"); err == nil {
		t.Fatal("a file that isn't in the pack")
	}
}

func TestWriterChecks(t *testing.T) {
	w := NewWriter(io.Discard)
	for _, bad := range []string{"../x", "/x", `a\b`, "C:/x", "a//b", "a/./b", "pack.json", "a/b.", ""} {
		if _, err := w.Create(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	w.Create("setups/1/1.json")
	if _, err := w.Create("setups/1/1.json"); err == nil {
		t.Error("the same name twice")
	}
	m := Manifest{Setups: []Setup{{Versions: []Version{{File: "setups/9/9.json"}}}}}
	if err := w.Close(m); err == nil {
		t.Error("the manifest names a file that isn't there")
	}
}

// rawZip writes files into a ZIP as they are, without the writer's checks (a pack someone made by hand).
func rawZip(files map[string]string) []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for n, c := range files {
		f, _ := z.Create(n)
		io.WriteString(f, c)
	}
	z.Close()
	return buf.Bytes()
}

func TestOpenRefuses(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"not a zip", []byte("hello"), ErrNotPack},
		{"setup zip without manifest", rawZip(map[string]string{"car/track/a.json": "{}"}), ErrNotPack},
		{"other json", rawZip(map[string]string{"pack.json": `{"kind":"something"}`}), ErrNotPack},
		{"newer", rawZip(map[string]string{"pack.json": `{"kind":"setupman-data-pack","format":99}`}), ErrNewer},
		{"path outside", rawZip(map[string]string{"pack.json": `{"kind":"setupman-data-pack","format":1}`, "../../evil.exe": "x"}), ErrDamaged},
		{"missing file", rawZip(map[string]string{"pack.json": `{"kind":"setupman-data-pack","format":1,"setups":[{"versions":[{"file":"setups/1/1.json"}]}]}`}), ErrDamaged},
		{"lap in a log that isn't there", rawZip(map[string]string{"pack.json": `{"kind":"setupman-data-pack","format":1,"sessions":[{"laps":[{"logLap":3}]}]}`}), ErrDamaged},
	}
	for _, c := range cases {
		if _, err := Open(bytes.NewReader(c.data), int64(len(c.data))); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}
