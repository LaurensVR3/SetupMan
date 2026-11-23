// Package folderbackup makes "ACC setups folder backups": a ZIP with every file of a game's
// setups folder (exact bytes and file dates) and restores a folder to exactly that state
// (spec D-74, D-81 … D-83). These backups contain only the game folder; SetupMan's own
// library is backed up separately (Full SetupMan backups, Phase 7).
package folderbackup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"setupman/internal/lastopened"
)

// Reasons for a backup, shown to the user.
const (
	ReasonBeforeManaging = "Before SetupMan started managing this folder"
	ReasonManual         = "Created by you"
	ReasonFullBackup     = "Part of a full SetupMan backup"
	ReasonTelemetryLaps  = "Before setting telemetry laps to 99"
	reasonBeforeRestore  = "Automatic, before restoring the backup of %s"
)

// FileEntry is one file in a backup.
type FileEntry struct {
	Path     string    `json:"path"` // relative, with "/" separators
	Size     int64     `json:"size"`
	SHA256   string    `json:"sha256"`
	Modified time.Time `json:"modified"`
}

// Manifest describes a backup; it is stored inside the ZIP as manifest.json.
type Manifest struct {
	Version   int         `json:"version"`
	Game      string      `json:"game"`
	Folder    string      `json:"folder"` // absolute path of the setups folder
	CreatedAt time.Time   `json:"createdAt"`
	Reason    string      `json:"reason"`
	Files     []FileEntry `json:"files"`
	Dirs      []string    `json:"dirs"` // every folder (so empty folders come back too)
}

// Summary is a backup in a list.
type Summary struct {
	ID        string    `json:"id"`
	Game      string    `json:"game"`
	Folder    string    `json:"folder"`
	CreatedAt time.Time `json:"createdAt"`
	Reason    string    `json:"reason"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
}

// Plan says what a restore would do.
type Plan struct {
	Folder string `json:"folder"` // the folder that is restored: the game's setups folder now
	// BackupFolder is the folder the backup was made of. It differs from Folder when the setups
	// folder setting changed since, or when the backup came from another PC (D-138).
	BackupFolder string `json:"backupFolder"`
	OtherFolder  bool   `json:"otherFolder"`
	Change       int    `json:"change"` // files that exist but differ (content, date or name case)
	Remove       int    `json:"remove"` // files added since the backup
	Return       int    `json:"return"` // files that are gone now and come back
	Same         int    `json:"same"`
}

// Result is the outcome of a restore.
type Result struct {
	Plan
	SafetyBackup string `json:"safetyBackup"` // id of the automatic backup made just before
}

// Store keeps backups in a folder, one subfolder per game.
type Store struct{ Dir string }

// ErrNotFound is returned for an unknown backup.
var ErrNotFound = errors.New("backup not found")

func (s *Store) gameDir(game string) string { return filepath.Join(s.Dir, safe(game)) }

// GameDir is the folder that holds one game's folder backups.
func (s *Store) GameDir(game string) string { return s.gameDir(game) }

// Path is where a backup's ZIP file is kept.
func (s *Store) Path(game, id string) string { return filepath.Join(s.gameDir(game), safe(id)+".zip") }

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
}

// Create backs up a folder. The ZIP is read back and checked before Create returns.
func (s *Store) Create(game, folder, reason string) (Summary, error) {
	folder, err := filepath.Abs(folder)
	if err != nil {
		return Summary{}, err
	}
	m, err := scanFolder(folder)
	if err != nil {
		return Summary{}, err
	}
	m.Version, m.Game, m.Folder, m.CreatedAt, m.Reason = 1, game, folder, time.Now(), reason

	dir := s.gameDir(game)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Summary{}, err
	}
	id := m.CreatedAt.Format("20060102-150405")
	for i := 2; fileExists(filepath.Join(dir, id+".zip")); i++ {
		id = fmt.Sprintf("%s-%d", m.CreatedAt.Format("20060102-150405"), i)
	}
	final := filepath.Join(dir, id+".zip")
	tmp := final + ".tmp"
	if err := writeZip(tmp, folder, m); err != nil {
		os.Remove(tmp)
		return Summary{}, err
	}
	if _, err := readAndVerify(tmp); err != nil {
		os.Remove(tmp)
		return Summary{}, fmt.Errorf("the backup could not be verified: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return Summary{}, err
	}
	return summarize(id, m), nil
}

// List returns the backups of a game, newest first.
func (s *Store) List(game string) ([]Summary, error) {
	entries, err := os.ReadDir(s.gameDir(game))
	if errors.Is(err, fs.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".zip")
		m, err := readManifest(filepath.Join(s.gameDir(game), e.Name()))
		if err != nil {
			continue // unreadable backups are not offered
		}
		out = append(out, summarize(id, m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) zipPath(game, id string) (string, error) {
	if id == "" || id != safe(id) {
		return "", ErrNotFound
	}
	p := filepath.Join(s.gameDir(game), id+".zip")
	if !fileExists(p) {
		return "", ErrNotFound
	}
	return p, nil
}

// PlanRestore compares a backup with folder (the game's setups folder now) as it is now.
func (s *Store) PlanRestore(game, id, folder string) (Plan, error) {
	p, err := s.zipPath(game, id)
	if err != nil {
		return Plan{}, err
	}
	m, err := readManifest(p)
	if err != nil {
		return Plan{}, err
	}
	if err := checkManifest(m, game); err != nil {
		return Plan{}, err
	}
	target, err := restoreTarget(folder)
	if err != nil {
		return Plan{}, err
	}
	cur, err := scanFolder(target)
	if err != nil {
		return Plan{}, fmt.Errorf("the folder %s can't be read: %w", target, err)
	}
	return diff(m, cur, target), nil
}

// restoreTarget checks the folder a backup is restored into: always the game's setups folder in
// use now, never the folder written inside the backup (D-138).
func restoreTarget(folder string) (string, error) {
	if folder == "" {
		return "", errors.New("this game has no setups folder; choose one in Settings → Games first")
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("the folder %s no longer exists", abs)
	}
	return abs, nil
}

// checkManifest refuses a backup of another game, or one whose file or folder names would point
// outside the setups folder (a damaged or hand-made backup, D-138).
func checkManifest(m *Manifest, game string) error {
	if m.Game != "" && m.Game != game {
		return fmt.Errorf("this backup belongs to another game (%s)", m.Game)
	}
	for _, f := range m.Files {
		if !safeRel(f.Path) {
			return fmt.Errorf("this backup contains an invalid file name (%s) and was not used", f.Path)
		}
	}
	for _, d := range m.Dirs {
		if !safeRel(d) {
			return fmt.Errorf("this backup contains an invalid folder name (%s) and was not used", d)
		}
	}
	return nil
}

// safeRel reports whether p is a plain relative path inside a folder ("car/track/name.json").
func safeRel(p string) bool {
	if p == "" || strings.ContainsAny(p, `\:`) || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	return true
}

func diff(backup, cur *Manifest, target string) Plan {
	pl := Plan{Folder: target, BackupFolder: backup.Folder,
		OtherFolder: !strings.EqualFold(filepath.Clean(backup.Folder), filepath.Clean(target))}
	byLower := map[string]FileEntry{}
	for _, f := range cur.Files {
		byLower[strings.ToLower(f.Path)] = f
	}
	inBackup := map[string]bool{}
	for _, f := range backup.Files {
		inBackup[strings.ToLower(f.Path)] = true
		c, ok := byLower[strings.ToLower(f.Path)]
		switch {
		case !ok:
			pl.Return++
		case c.Path != f.Path || c.SHA256 != f.SHA256 || !sameTime(c.Modified, f.Modified):
			pl.Change++
		default:
			pl.Same++
		}
	}
	for _, f := range cur.Files {
		if !inBackup[strings.ToLower(f.Path)] {
			pl.Remove++
		}
	}
	return pl
}

// Restore returns folder (the game's setups folder now) to exactly the state of the backup. It
// first verifies the backup, then makes an automatic backup of the current folder, then restores
// and finally checks the whole folder against the backup. The folder written inside the backup is
// only shown to the user; it is never written to (D-138).
func (s *Store) Restore(game, id, folder string) (Result, error) {
	p, err := s.zipPath(game, id)
	if err != nil {
		return Result{}, err
	}
	m, err := readAndVerify(p) // never touch the folder with a damaged backup
	if err != nil {
		return Result{}, fmt.Errorf("this backup is damaged and was not restored: %w", err)
	}
	if err := checkManifest(m, game); err != nil {
		return Result{}, err
	}
	target, err := restoreTarget(folder)
	if err != nil {
		return Result{}, err
	}
	cur, err := scanFolder(target)
	if err != nil {
		return Result{}, err
	}
	res := Result{Plan: diff(m, cur, target)}

	safety, err := s.Create(game, target, fmt.Sprintf(reasonBeforeRestore, m.CreatedAt.Format("2 Jan 2006, 15:04")))
	if err != nil {
		return Result{}, fmt.Errorf("could not create the safety backup, nothing was restored: %w", err)
	}
	res.SafetyBackup = safety.ID

	if err := restoreInto(p, m, cur, target); err != nil {
		return res, fmt.Errorf("restore stopped: %w (the safety backup %s holds the folder as it was)", err, safety.ID)
	}
	after, err := scanFolder(target)
	if err != nil {
		return res, err
	}
	if d := diff(m, after, target); d.Change+d.Remove+d.Return != 0 {
		return res, fmt.Errorf("after restoring, the folder still differs from the backup (%d changed, %d extra, %d missing); the safety backup %s holds the folder as it was",
			d.Change, d.Remove, d.Return, safety.ID)
	}
	return res, nil
}

func restoreInto(zipPath string, m *Manifest, cur *Manifest, root string) error {
	want := map[string]FileEntry{}
	for _, f := range m.Files {
		want[strings.ToLower(f.Path)] = f
	}
	// 1. Remove files that are not in the backup, or only differ in upper/lower case
	//    (Windows treats those as the same file; they are written again below).
	for _, f := range cur.Files {
		w, ok := want[strings.ToLower(f.Path)]
		if !ok || w.Path != f.Path {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(f.Path))); err != nil {
				return err
			}
		}
	}
	// 2. Remove folders that are not in the backup (deepest first, only when empty).
	wantDir := map[string]string{}
	for _, d := range m.Dirs {
		wantDir[strings.ToLower(d)] = d
	}
	dirs := append([]string(nil), cur.Dirs...)
	sort.Slice(dirs, func(i, j int) bool { return strings.Count(dirs[i], "/") > strings.Count(dirs[j], "/") })
	for _, d := range dirs {
		if _, ok := wantDir[strings.ToLower(d)]; !ok {
			os.Remove(filepath.Join(root, filepath.FromSlash(d))) // fails harmlessly if not empty
		}
	}
	// 3. Folders: create missing ones and fix upper/lower case of existing ones.
	all := append([]string(nil), m.Dirs...)
	sort.Slice(all, func(i, j int) bool { return strings.Count(all[i], "/") < strings.Count(all[j], "/") })
	for _, d := range all {
		if err := ensureDir(root, d); err != nil {
			return err
		}
	}
	// 4. Files: write the ones that are missing or different, then set their dates.
	have := map[string]FileEntry{}
	if now, err := scanFolder(root); err == nil {
		for _, f := range now.Files {
			have[f.Path] = f
		}
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, zf := range zr.File {
		entries[zf.Name] = zf
	}
	for _, f := range m.Files {
		target := filepath.Join(root, filepath.FromSlash(f.Path))
		if h, ok := have[f.Path]; !ok || h.SHA256 != f.SHA256 {
			zf := entries["files/"+f.Path]
			if zf == nil {
				return fmt.Errorf("%s is missing from the backup", f.Path)
			}
			if err := writeFromZip(zf, target); err != nil {
				return err
			}
		}
		if err := os.Chtimes(target, f.Modified, f.Modified); err != nil {
			return err
		}
	}
	return nil
}

// ensureDir makes sure folder rel exists with exactly this upper/lower case.
func ensureDir(root, rel string) error {
	target := filepath.Join(root, filepath.FromSlash(rel))
	parent, name := filepath.Split(target)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			if e.Name() == name {
				return nil
			}
			// Same folder, other case: rename via a temporary name (Windows ignores case-only renames).
			tmp := filepath.Join(parent, ".setupman-case-"+name)
			if err := os.Rename(filepath.Join(parent, e.Name()), tmp); err != nil {
				return err
			}
			return os.Rename(tmp, target)
		}
	}
	return os.Mkdir(target, 0o755)
}

func writeFromZip(zf *zip.File, target string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := filepath.Join(filepath.Dir(target), ".setupman-restore-"+filepath.Base(target))
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target) // replaces an existing file
}

// ---------- reading and writing ----------

func scanFolder(folder string) (*Manifest, error) {
	m := &Manifest{Files: []FileEntry{}, Dirs: []string{}}
	err := filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(folder, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			m.Dirs = append(m.Dirs, rel)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sum, err := hashFile(p)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, FileEntry{Path: rel, Size: info.Size(), SHA256: sum, Modified: info.ModTime()})
		return nil
	})
	return m, err
}

func hashFile(p string) (string, error) {
	f, err := lastopened.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeZip(dst, folder string, m *Manifest) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	mj, _ := json.MarshalIndent(m, "", "  ")
	w, err := zw.Create("manifest.json")
	if err == nil {
		_, err = w.Write(mj)
	}
	for _, f := range m.Files {
		if err != nil {
			break
		}
		hdr := &zip.FileHeader{Name: "files/" + f.Path, Method: zip.Deflate, Modified: f.Modified}
		var w io.Writer
		if w, err = zw.CreateHeader(hdr); err != nil {
			break
		}
		var in *os.File
		if in, err = lastopened.Open(filepath.Join(folder, filepath.FromSlash(f.Path))); err != nil {
			break
		}
		_, err = io.Copy(w, in)
		in.Close()
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func readManifest(zipPath string) (*Manifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name == "manifest.json" {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			var m Manifest
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				return nil, err
			}
			return &m, nil
		}
	}
	return nil, errors.New("no manifest in backup")
}

// readAndVerify checks every file in the ZIP against the manifest (size and SHA-256).
func readAndVerify(zipPath string) (*Manifest, error) {
	m, err := readManifest(zipPath)
	if err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, zf := range zr.File {
		entries[zf.Name] = zf
	}
	for _, f := range m.Files {
		zf := entries[path.Join("files", f.Path)]
		if zf == nil {
			return nil, fmt.Errorf("%s is missing", f.Path)
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, err := io.Copy(h, rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return nil, fmt.Errorf("%s does not match", f.Path)
		}
	}
	return m, nil
}

func summarize(id string, m *Manifest) Summary {
	s := Summary{ID: id, Game: m.Game, Folder: m.Folder, CreatedAt: m.CreatedAt, Reason: m.Reason, Files: len(m.Files)}
	for _, f := range m.Files {
		s.Bytes += f.Size
	}
	return s
}

// sameTime compares file dates to the millisecond: SetupMan's library keeps dates in milliseconds,
// so a setup it put back in the folder may differ by less than that from the original (D-138).
// A restore still writes the exact dates from the backup.
func sameTime(a, b time.Time) bool { return a.UnixMilli() == b.UnixMilli() }

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
