// Package fullbackup makes "Full SetupMan backups" (spec D-82, D-92): one ZIP with SetupMan's
// library (setups, every version, history, notes, lap times), its settings, and an ACC setups
// folder backup of every game. Restoring puts the library and settings back when SetupMan
// starts (the library can't be replaced while it is open). The game folders are never changed
// by this restore: their backups are added to the ACC setups folder backups, where the user can
// restore them exactly.
package fullbackup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reasons shown in the list.
const (
	ReasonManual        = "Created by you"
	ReasonBeforeRestore = "Automatic, before restoring a full backup"
)

const (
	libraryFile  = "library.db"
	settingsFile = "settings.json"
	pendingFile  = "restore-pending.zip"
	manifestName = "manifest.json"
	// TelemetryDir is the folder (in the data folder) with SetupMan's extra telemetry logs (D-162).
	TelemetryDir = "telemetry"
)

// ErrNotFound is returned for an unknown backup.
var ErrNotFound = errors.New("backup not found")

// Manifest is stored inside every full backup.
type Manifest struct {
	Kind      string            `json:"kind"` // always "setupman-full-backup"
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"createdAt"`
	Reason    string            `json:"reason"`
	Files     map[string]string `json:"files"`   // name in the ZIP → SHA-256
	Folders   []FolderRef       `json:"folders"` // ACC setups folder backups inside
}

// FolderRef is one game folder backup inside a full backup (folders/<game>/<id>.zip).
type FolderRef struct {
	Game string `json:"game"`
	ID   string `json:"id"`
}

// Summary is one backup in the list.
type Summary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Reason    string    `json:"reason"`
	Bytes     int64     `json:"bytes"`
	Games     []string  `json:"games"`
}

// Store keeps full backups in <data folder>/full-backups.
type Store struct {
	DataDir string // %APPDATA%\SetupMan
}

func (s *Store) dir() string { return filepath.Join(s.DataDir, "full-backups") }

// Dir is the folder that holds the full backups.
func (s *Store) Dir() string { return s.dir() }

// Path is where a backup's ZIP is kept ("" for an invalid id).
func (s *Store) Path(id string) string {
	if !validID(id) {
		return ""
	}
	return filepath.Join(s.dir(), id+".zip")
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Snapshotter writes a consistent copy of the library database to a file.
type Snapshotter interface{ BackupTo(path string) error }

// Create makes a full backup. folderZips maps "game/id" to the ZIP of a folder backup.
func (s *Store) Create(reason string, db Snapshotter, folderZips map[FolderRef]string) (Summary, error) {
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return Summary{}, err
	}
	tmpDB := filepath.Join(s.dir(), fmt.Sprintf(".library-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmpDB)
	if err := db.BackupTo(tmpDB); err != nil {
		return Summary{}, fmt.Errorf("copying the library: %w", err)
	}
	files := map[string]string{libraryFile: tmpDB}
	if _, err := os.Stat(filepath.Join(s.DataDir, settingsFile)); err == nil {
		files[settingsFile] = filepath.Join(s.DataDir, settingsFile)
	}
	// SetupMan's own extra telemetry logs (D-162). The game's telemetry files are never included (D-160).
	if logs, _ := filepath.Glob(filepath.Join(s.DataDir, TelemetryDir, "*.smx.gz")); len(logs) > 0 {
		for _, p := range logs {
			files[TelemetryDir+"/"+filepath.Base(p)] = p
		}
	}
	// telemetry from imported data packs (D-201): SetupMan's own copies, in telemetry/packs/<pack>/
	if packs, _ := filepath.Glob(filepath.Join(s.DataDir, TelemetryDir, "packs", "*", "*.ld*")); len(packs) > 0 {
		for _, p := range packs {
			files[TelemetryDir+"/packs/"+filepath.Base(filepath.Dir(p))+"/"+filepath.Base(p)] = p
		}
	}
	m := Manifest{Kind: "setupman-full-backup", Version: 1, CreatedAt: time.Now(), Reason: reason, Files: map[string]string{}}
	for ref, p := range folderZips {
		files[folderEntry(ref)] = p
		m.Folders = append(m.Folders, ref)
	}
	sort.Slice(m.Folders, func(i, j int) bool { return m.Folders[i].Game < m.Folders[j].Game })

	id := m.CreatedAt.Format("2006-01-02_150405")
	for i := 2; fileExists(s.Path(id)); i++ {
		id = fmt.Sprintf("%s_%d", m.CreatedAt.Format("2006-01-02_150405"), i)
	}
	tmpZip := s.Path(id) + ".tmp"
	if err := writeZip(tmpZip, files, &m); err != nil {
		os.Remove(tmpZip)
		return Summary{}, err
	}
	if _, err := verify(tmpZip); err != nil {
		os.Remove(tmpZip)
		return Summary{}, fmt.Errorf("the backup failed verification: %w", err)
	}
	if err := os.Rename(tmpZip, s.Path(id)); err != nil {
		os.Remove(tmpZip)
		return Summary{}, err
	}
	return s.summary(id)
}

func folderEntry(ref FolderRef) string { return "folders/" + ref.Game + "/" + ref.ID + ".zip" }

func writeZip(dst string, files map[string]string, m *Manifest) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		data, err := os.ReadFile(files[n])
		if err != nil {
			zw.Close()
			f.Close()
			return err
		}
		sum := sha256.Sum256(data)
		m.Files[n] = hex.EncodeToString(sum[:])
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: m.CreatedAt})
		if err == nil {
			_, err = w.Write(data)
		}
		if err != nil {
			zw.Close()
			f.Close()
			return err
		}
	}
	mj, _ := json.MarshalIndent(m, "", "  ")
	w, err := zw.Create(manifestName)
	if err == nil {
		_, err = w.Write(mj)
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// verify reads a backup back and checks every file against its manifest.
func verify(p string) (*Manifest, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, errors.New("not a readable ZIP file")
	}
	defer zr.Close()
	return verifyReader(&zr.Reader)
}

func verifyReader(zr *zip.Reader) (*Manifest, error) {
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	mf := byName[manifestName]
	if mf == nil {
		return nil, errors.New("this is not a full SetupMan backup (no manifest)")
	}
	var m Manifest
	if err := readJSON(mf, &m); err != nil || m.Kind != "setupman-full-backup" {
		return nil, errors.New("this is not a full SetupMan backup")
	}
	if m.Version > 1 {
		return nil, errors.New("this backup was made by a newer SetupMan; update SetupMan first")
	}
	if _, ok := m.Files[libraryFile]; !ok {
		return nil, errors.New("the backup has no library")
	}
	for name, want := range m.Files {
		f := byName[name]
		if f == nil {
			return nil, fmt.Errorf("%s is missing", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, rc)
		rc.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return nil, fmt.Errorf("%s is damaged", name)
		}
	}
	return &m, nil
}

func readJSON(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(v)
}

func (s *Store) summary(id string) (Summary, error) {
	p := s.Path(id)
	st, err := os.Stat(p)
	if err != nil {
		return Summary{}, ErrNotFound
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		return Summary{}, err
	}
	defer zr.Close()
	var m Manifest
	for _, f := range zr.File {
		if f.Name == manifestName {
			if err := readJSON(f, &m); err != nil {
				return Summary{}, err
			}
		}
	}
	sum := Summary{ID: id, CreatedAt: m.CreatedAt, Reason: m.Reason, Bytes: st.Size(), Games: []string{}}
	for _, f := range m.Folders {
		sum.Games = append(sum.Games, f.Game)
	}
	return sum, nil
}

// List returns the backups, newest first.
func (s *Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.dir())
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	} else if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".zip")
		if e.IsDir() || !ok || !validID(id) {
			continue
		}
		if sum, err := s.summary(id); err == nil {
			out = append(out, sum)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// AddFile stores a full backup the user uploaded (e.g. from another PC), after checking it.
func (s *Store) AddFile(data []byte) (Summary, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Summary{}, errors.New("not a readable ZIP file")
	}
	m, err := verifyReader(zr)
	if err != nil {
		return Summary{}, err
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return Summary{}, err
	}
	base := "file_" + m.CreatedAt.Format("2006-01-02_150405")
	id := base
	for i := 2; fileExists(s.Path(id)); i++ {
		id = fmt.Sprintf("%s_%d", base, i)
	}
	if err := os.WriteFile(s.Path(id), data, 0o644); err != nil {
		return Summary{}, err
	}
	return s.summary(id)
}

// ScheduleRestore marks a backup to be restored the next time SetupMan starts.
func (s *Store) ScheduleRestore(id string) error {
	p := s.Path(id)
	if p == "" || !fileExists(p) {
		return ErrNotFound
	}
	if _, err := verify(p); err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.DataDir, pendingFile), data, 0o644)
}

// Pending reports whether a restore waits for the next start.
func (s *Store) Pending() bool { return fileExists(filepath.Join(s.DataDir, pendingFile)) }

// ApplyPending restores a scheduled backup. It must run before the library is opened.
// safety makes a full backup of the current state first (it opens and closes the library itself).
// folderDir is where ACC setups folder backups live (<data>/backups); the folder backups inside
// the full backup are added there (existing ones are kept). It returns false when nothing waits.
func (s *Store) ApplyPending(safety func() error, folderDir string) (bool, error) {
	pending := filepath.Join(s.DataDir, pendingFile)
	if !fileExists(pending) {
		return false, nil
	}
	if _, err := verify(pending); err != nil {
		os.Rename(pending, pending+".failed")
		return true, err
	}
	if fileExists(filepath.Join(s.DataDir, libraryFile)) {
		if err := safety(); err != nil {
			return true, fmt.Errorf("could not back up the current state first, nothing was restored: %w", err)
		}
	}
	if err := s.extractAll(pending, folderDir); err != nil {
		return true, err
	}
	return true, os.Remove(pending)
}

// extractAll puts the library, settings and folder backups of a full backup in place.
func (s *Store) extractAll(pending, folderDir string) error {
	zr, err := zip.OpenReader(pending)
	if err != nil {
		return err
	}
	defer zr.Close() // closed before the pending file is removed (Windows)
	// The old library's write-ahead files belong to the old database: remove them first.
	os.Remove(filepath.Join(s.DataDir, libraryFile+"-wal"))
	os.Remove(filepath.Join(s.DataDir, libraryFile+"-shm"))
	for _, f := range zr.File {
		var dst string
		switch {
		case f.Name == libraryFile || f.Name == settingsFile:
			dst = filepath.Join(s.DataDir, f.Name)
		case strings.HasPrefix(f.Name, "folders/") && strings.HasSuffix(f.Name, ".zip"):
			parts := strings.Split(f.Name, "/")
			if len(parts) != 3 || !validID(parts[1]) || !validID(strings.TrimSuffix(parts[2], ".zip")) {
				continue
			}
			dst = filepath.Join(folderDir, parts[1], path.Base(f.Name))
			if fileExists(dst) {
				continue // already there
			}
		case strings.HasPrefix(f.Name, TelemetryDir+"/packs/"):
			parts := strings.Split(f.Name, "/")
			name := ""
			if len(parts) == 4 {
				name = parts[3]
			}
			ext := path.Ext(name)
			if len(parts) != 4 || !validID(parts[2]) || (ext != ".ld" && ext != ".ldx") || !validID(strings.ReplaceAll(strings.TrimSuffix(name, ext), ".", "_")) {
				continue
			}
			dst = filepath.Join(s.DataDir, TelemetryDir, "packs", parts[2], name)
			if fileExists(dst) {
				continue
			}
		case strings.HasPrefix(f.Name, TelemetryDir+"/") && strings.HasSuffix(f.Name, ".smx.gz"):
			base := strings.TrimSuffix(path.Base(f.Name), ".smx.gz")
			if path.Dir(f.Name) != TelemetryDir || !validID(base) {
				continue
			}
			dst = filepath.Join(s.DataDir, TelemetryDir, base+".smx.gz")
			if fileExists(dst) {
				continue // already there (logs never change once a session is over)
			}
		default:
			continue
		}
		if err := extract(f, dst); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dst + ".restoring"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
