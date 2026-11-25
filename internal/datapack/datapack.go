// Package datapack is the file format of a data pack (spec D-201): one ordinary ZIP file with
// setups, laps and telemetry that one SetupMan exports and another imports.
//
// Inside the ZIP:
//
//	pack.json                 the manifest (Manifest): who made it, what is inside
//	setups/<n>/<v><ext>       setup files, exact bytes (version v of setup n, oldest first)
//	telemetry/<n>.ld, .ldx    the chosen laps cut out of the game's MoTeC log (motec.Cut)
//	extra/<n>.smx.gz          the same laps of SetupMan's extra log (telemetry.ExtraLog.Cut)
//
// The manifest names the sender only by initials (D-135): no folder paths, computer name or
// settings. This package only reads and writes the ZIP; the library decides what goes in and
// where imported data goes.
package datapack

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"setupman/internal/naming"
)

const (
	Kind         = "setupman-data-pack"
	Format       = 1 // raised when an older SetupMan can no longer read a pack
	ManifestName = "pack.json"
	Ext          = ".zip"
)

// Limits for opening a pack someone sent: a pack is a few MB per lap, so these leave lots of room.
const (
	maxEntries   = 20000
	maxManifest  = 32 << 20  // 32 MB
	maxFile      = 512 << 20 // one file inside the pack
	maxTotal     = 4 << 30   // everything inside, unpacked
	maxNameBytes = 200
)

// Manifest is pack.json.
type Manifest struct {
	Kind     string    `json:"kind"`
	Format   int       `json:"format"`
	ID       string    `json:"id"` // random, so importing the same pack twice is recognised
	Created  time.Time `json:"created"`
	By       string    `json:"by"`  // the sender's initials (the External badge)
	App      string    `json:"app"` // SetupMan version that made it
	Setups   []Setup   `json:"setups,omitempty"`
	Sessions []Session `json:"sessions,omitempty"`
}

// Setup is one setup with one or more of its versions.
type Setup struct {
	// Key stays the same for this setup in every pack its owner makes, so a newer pack adds a new
	// version to the setup the receiver already has instead of a copy.
	Key      string      `json:"key"`
	Game     string      `json:"game"`
	Car      string      `json:"car"`
	Track    string      `json:"track"`
	Name     string      `json:"name"`
	Session  string      `json:"session,omitempty"` // "Q", "R", "Endurance" or ""
	Meta     naming.Meta `json:"meta"`
	Note     string      `json:"note,omitempty"`
	Laps     *LapSummary `json:"laps,omitempty"` // the sender's lap times on the newest version
	Versions []Version   `json:"versions"`       // oldest first; the last one is the newest
}

// Version is one stored version of a setup.
type Version struct {
	Key   string    `json:"key"`  // the same version in another pack has the same key
	File  string    `json:"file"` // path inside the pack
	Saved time.Time `json:"saved"`
	Note  string    `json:"note,omitempty"` // the change note
}

// LapSummary is the sender's lap times on a setup version.
type LapSummary struct {
	Laps       int     `json:"laps"`
	Clean      int     `json:"clean"`
	BestMs     int     `json:"bestMs,omitempty"`
	AvgMs      int     `json:"avgMs,omitempty"`
	FuelPerLap float64 `json:"fuelPerLap,omitempty"`
}

// Session is part of one driving session: its chosen laps and their telemetry.
type Session struct {
	Key     string    `json:"key"`
	Game    string    `json:"game"`
	Car     string    `json:"car"`
	Track   string    `json:"track"`
	Kind    string    `json:"kind,omitempty"` // practice, qualifying, race … as SetupMan recorded it
	Started time.Time `json:"started"`
	Laps    []Lap     `json:"laps"`
	Logs    []Log     `json:"logs,omitempty"`
}

// Lap is one lap with SetupMan's data about it.
type Lap struct {
	N      int     `json:"n"` // lap number in the session, as the sender's pages show it
	TimeMs int     `json:"timeMs"`
	Valid  bool    `json:"valid"`
	Full   bool    `json:"full"` // a full lap (D-163), not an out-lap, in-lap or cut lap
	Fuel   float64 `json:"fuel,omitempty"`
	Air    float64 `json:"air,omitempty"`
	Road   float64 `json:"road,omitempty"`
	Setup  string  `json:"setup,omitempty"`  // Setup.Key of the setup driven (stints, D-177)
	SetupV string  `json:"setupV,omitempty"` // Version.Key
	Note   string  `json:"note,omitempty"`
	Log    int     `json:"log,omitempty"`    // 1-based index into Session.Logs; 0 = no telemetry
	LogLap int     `json:"logLap,omitempty"` // the lap's number inside that log (motec.Lap.N)
}

// Log is one cut telemetry file of a session (one run of consecutive laps).
type Log struct {
	LD    string `json:"ld"`              // path inside the pack
	LDX   string `json:"ldx,omitempty"`   // MoTeC i2's lap markers
	Extra string `json:"extra,omitempty"` // SetupMan's extra log of the same laps
}

// NewID returns a random key for a pack, setup, version or session.
func NewID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Writer writes a pack.
type Writer struct {
	z     *zip.Writer
	names map[string]bool
}

// NewWriter starts a pack on w.
func NewWriter(w io.Writer) *Writer { return &Writer{z: zip.NewWriter(w), names: map[string]bool{}} }

// Create adds a file to the pack. Already compressed files (.gz) are stored as they are.
func (w *Writer) Create(name string) (io.Writer, error) {
	if err := checkName(name); err != nil || name == ManifestName {
		return nil, fmt.Errorf("datapack: bad file name %q", name)
	}
	if w.names[name] {
		return nil, fmt.Errorf("datapack: %s twice", name)
	}
	w.names[name] = true
	method := zip.Deflate
	if strings.HasSuffix(name, ".gz") {
		method = zip.Store
	}
	return w.z.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
}

// Close writes the manifest and finishes the ZIP. Every file the manifest names must be in it.
func (w *Writer) Close(m Manifest) error {
	m.Kind, m.Format = Kind, Format
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.Created.IsZero() {
		m.Created = time.Now()
	}
	for _, f := range m.files() {
		if !w.names[f] {
			return fmt.Errorf("datapack: %s is named but not in the pack", f)
		}
	}
	fw, err := w.z.Create(ManifestName)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(fw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	return w.z.Close()
}

// files lists every file the manifest names.
func (m Manifest) files() []string {
	var out []string
	for _, s := range m.Setups {
		for _, v := range s.Versions {
			out = append(out, v.File)
		}
	}
	for _, s := range m.Sessions {
		for _, l := range s.Logs {
			for _, f := range []string{l.LD, l.LDX, l.Extra} {
				if f != "" {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// Errors for packs that can't be opened, in words for the import window.
var (
	ErrNotPack = errors.New("this file is not a SetupMan data pack")
	ErrNewer   = errors.New("this data pack was made by a newer SetupMan; update SetupMan first")
	ErrDamaged = errors.New("this data pack is damaged")
)

// Pack is an opened pack.
type Pack struct {
	Manifest Manifest
	files    map[string]*zip.File
}

// Open reads a pack's table of contents and manifest, and checks that everything it names is
// there, has a safe name and isn't unreasonably large.
func Open(r io.ReaderAt, size int64) (*Pack, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, ErrNotPack
	}
	if len(z.File) > maxEntries {
		return nil, ErrDamaged
	}
	p := &Pack{files: map[string]*zip.File{}}
	var total uint64
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "/") {
			continue // a folder entry
		}
		if checkName(f.Name) != nil || p.files[f.Name] != nil || f.UncompressedSize64 > maxFile {
			return nil, ErrDamaged
		}
		total += f.UncompressedSize64
		p.files[f.Name] = f
	}
	if total > maxTotal {
		return nil, ErrDamaged
	}
	mf := p.files[ManifestName]
	if mf == nil {
		return nil, ErrNotPack
	}
	data, err := p.read(mf, maxManifest)
	if err != nil {
		return nil, ErrDamaged
	}
	var head struct {
		Kind   string `json:"kind"`
		Format int    `json:"format"`
	}
	if json.Unmarshal(data, &head) != nil || head.Kind != Kind {
		return nil, ErrNotPack
	}
	if head.Format > Format {
		return nil, ErrNewer
	}
	if json.Unmarshal(data, &p.Manifest) != nil {
		return nil, ErrDamaged
	}
	for _, f := range p.Manifest.files() {
		if p.files[f] == nil {
			return nil, ErrDamaged
		}
	}
	for _, s := range p.Manifest.Sessions {
		for _, l := range s.Laps {
			if l.Log < 0 || l.Log > len(s.Logs) {
				return nil, ErrDamaged
			}
		}
	}
	return p, nil
}

// ReadFile returns a file of the pack (a name from the manifest).
func (p *Pack) ReadFile(name string) ([]byte, error) {
	f := p.files[name]
	if f == nil {
		return nil, fmt.Errorf("datapack: %s is not in the pack", name)
	}
	return p.read(f, maxFile)
}

// read unpacks a file, never more than limit bytes (whatever its header claims).
func (p *Pack) read(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrDamaged
	}
	return data, nil
}

// checkName allows only plain relative names inside the pack: no drive letters, no backslashes,
// no "..", nothing that could point outside the folder the import unpacks into.
func checkName(name string) error {
	if name == "" || len(name) > maxNameBytes || strings.ContainsAny(name, "\\:*?\"<>|\x00") ||
		strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return ErrDamaged
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || part == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return ErrDamaged
		}
	}
	return nil
}
