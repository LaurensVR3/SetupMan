package library

// Exporting and importing setups in batches (original feature list; spec D-91). Export only reads.
// Import puts setups in SetupMan's library only (never straight into the game folder); from there
// the user can put them in the game folder as usual.

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"setupman/internal/game"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/history"
	"setupman/internal/naming"
	"setupman/internal/store"
)

// maxImportFile is the biggest single setup file accepted (ACC setups are ~3 KB).
const maxImportFile = 1 << 20

// Export writes the newest version of each setup into a ZIP, as <car>/<track>/<name><ext>
// (the game's own folder layout, so the ZIP can be unpacked into a setups folder).
func (l *Library) Export(setupIDs []int64, w io.Writer) (int, error) {
	if l.db == nil {
		return 0, ErrNoHistory
	}
	if len(setupIDs) == 0 {
		return 0, errors.New("choose at least one setup to export")
	}
	zw := zip.NewWriter(w)
	used := map[string]bool{}
	n := 0
	for _, id := range setupIDs {
		s, err := l.db.Setup(id)
		if err != nil || s == nil {
			return n, history.ErrNotFound
		}
		v, err := l.db.LatestVersion(id)
		if err != nil || v == nil {
			continue
		}
		dir := s.Car + "/" + s.Track
		if s.RelPath != "" {
			dir = path.Dir(s.RelPath) // keeps the folder's exact spelling
		}
		ext := ".json"
		if g := l.game(s.Game); g != nil {
			ext = game.Ext(g)
			if game.ReadOnlyReason(g) != "" {
				ext = ".txt"
			}
		}
		name := dir + "/" + s.Name + ext
		for i := 2; used[strings.ToLower(name)]; i++ {
			name = fmt.Sprintf("%s/%s (%d)%s", dir, s.Name, i, ext)
		}
		used[strings.ToLower(name)] = true
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: v.ObservedAt})
		if err != nil {
			return n, err
		}
		content := v.Content
		if g := l.game(s.Game); g != nil && game.ReadOnlyReason(g) != "" {
			content = settingsText(g, s.Name, v.Content) // the raw bytes are only a piece of the game's save file
		}
		if _, err := fw.Write(content); err != nil {
			return n, err
		}
		n++
	}
	return n, zw.Close()
}

// ImportFile is one uploaded file: a setup file (e.g. .json) or a ZIP of setups.
type ImportFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"` // base64 in JSON
}

// ImportItem says what happened to one setup.
type ImportItem struct {
	Name    string `json:"name"`
	SetupID int64  `json:"setupId,omitempty"`
	Result  string `json:"result"` // "imported" or "skipped"
	Reason  string `json:"reason,omitempty"`
}

// ImportResult is the summary shown after an import.
type ImportResult struct {
	Imported int          `json:"imported"`
	Skipped  int          `json:"skipped"`
	Items    []ImportItem `json:"items"`
}

// Import adds setups to the library for one car and track. Files inside a ZIP that sit in
// <car>/<track>/ folders of a different track are skipped with a reason, so nothing ends up in the
// wrong place. A setup for another car, or one that is already in the library, is skipped too.
func (l *Library) Import(gameID, car, track string, files []ImportFile) (ImportResult, error) {
	res := ImportResult{Items: []ImportItem{}}
	if l.db == nil {
		return res, ErrNoHistory
	}
	g := l.game(gameID)
	if g == nil {
		return res, errors.New("unknown game")
	}
	if r := game.ReadOnlyReason(g); r != "" {
		return res, errors.New(r)
	}
	ext := game.Ext(g)
	if len(files) == 0 {
		return res, fmt.Errorf("choose one or more setup files (%s) or a .zip", ext)
	}
	type item struct {
		name string
		dir  string // folder path inside a ZIP ("" for loose files)
		data []byte
	}
	var items []item
	for _, f := range files {
		switch strings.ToLower(path.Ext(f.Name)) {
		case ShareExt: // a shared setup carries its own details (D-94)
			it, err := l.ImportShared(f.Data, car, track)
			if err != nil {
				it = ImportItem{Name: f.Name, Result: "skipped", Reason: err.Error()}
			}
			if it.Result == "imported" {
				res.Imported++
			}
			res.Items = append(res.Items, it)
		case ext:
			items = append(items, item{name: f.Name, data: f.Data})
		case ".zip":
			zr, err := zip.NewReader(bytes.NewReader(f.Data), int64(len(f.Data)))
			if err != nil {
				res.Items = append(res.Items, ImportItem{Name: f.Name, Result: "skipped", Reason: "not a readable ZIP file"})
				continue
			}
			for _, zf := range zr.File {
				if zf.FileInfo().IsDir() || !strings.EqualFold(path.Ext(zf.Name), ext) {
					continue
				}
				if zf.UncompressedSize64 > maxImportFile {
					res.Items = append(res.Items, ImportItem{Name: zf.Name, Result: "skipped", Reason: "file too big for a setup"})
					continue
				}
				rc, err := zf.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(io.LimitReader(rc, maxImportFile+1))
				rc.Close()
				if err != nil {
					continue
				}
				items = append(items, item{name: path.Base(zf.Name), dir: path.Dir(zf.Name), data: data})
			}
		default:
			res.Items = append(res.Items, ImportItem{Name: f.Name, Result: "skipped", Reason: "only " + ext + " and .setupman setups and .zip files can be imported"})
		}
	}

	existing, err := l.db.SetupsForTrack(gameID, car, track)
	if err != nil {
		return res, err
	}
	have := map[string]string{} // fingerprint → setup name
	for _, s := range existing {
		if v, _ := l.db.LatestVersion(s.ID); v != nil {
			have[v.Fingerprint] = s.Name
		}
	}
	names := map[string]bool{}
	for _, s := range existing {
		names[strings.ToLower(s.Name)] = true
	}
	cfg := l.historyConfig(gameID)
	for _, it := range items {
		// The name becomes a file name in the game folder later: make it one Windows accepts (D-138).
		name := cleanName(strings.TrimSuffix(it.name, path.Ext(it.name)), "Imported setup")
		skip := func(reason string) {
			res.Items = append(res.Items, ImportItem{Name: name, Result: "skipped", Reason: reason})
		}
		if it.dir != "" && it.dir != "." {
			parts := strings.Split(it.dir, "/")
			if len(parts) >= 2 && !strings.EqualFold(parts[len(parts)-1], track) {
				skip("it belongs to another track (" + parts[len(parts)-1] + ")")
				continue
			}
		}
		fields, err := g.ParseSetup(it.data)
		if err != nil {
			skip("not a setup file of this game")
			continue
		}
		fp := compare.Fingerprint(fields)
		if other, ok := have[fp]; ok {
			skip("already in your library as " + other)
			continue
		}
		fileCar := ""
		for _, f := range fields {
			if f.Path == "carName" {
				fileCar = f.Value
			}
		}
		if fileCar != "" && !strings.EqualFold(fileCar, car) {
			skip("it is a setup for another car (" + fileCar + ")")
			continue
		}
		final := name
		for i := 2; names[strings.ToLower(final)]; i++ {
			final = fmt.Sprintf("%s (%d)", name, i)
		}
		m := naming.ParseWith(final, cfg.TempRules)
		s := &store.Setup{Game: gameID, Car: car, Track: track, Name: final, Meta: m,
			Session: naming.GuessSession(m, cfg.EnduranceMinutes)}
		if err := l.db.InsertSetup(s); err != nil {
			return res, err
		}
		v := &store.Version{SetupID: s.ID, Fingerprint: fp, Content: it.data, Source: "imported",
			ObservedAt: time.Now(), Note: "Imported from " + it.name}
		if err := l.db.InsertVersion(v); err != nil {
			return res, err
		}
		l.db.InsertEvent(store.Event{Game: gameID, Kind: "imported-file", SetupID: s.ID, VersionID: v.ID,
			Details: map[string]any{"name": final}})
		have[fp], names[strings.ToLower(final)] = final, true
		res.Imported++
		res.Items = append(res.Items, ImportItem{Name: final, SetupID: s.ID, Result: "imported"})
	}
	for _, it := range res.Items {
		if it.Result == "skipped" {
			res.Skipped++
		}
	}
	if res.Imported > 0 {
		l.publish(Event{Type: "history-changed"})
	}
	return res, nil
}

// settingsText lists a setup's settings as readable text, for games whose setups are not
// separate files (AC Rally).
func settingsText(g game.Game, name string, content []byte) []byte {
	fields, err := g.ParseSetup(content)
	if err != nil {
		return []byte(name + ": the settings could not be read\r\n")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s – %s\r\n", name, g.Name())
	section := ""
	for _, f := range fields {
		if f.Section != section {
			section = f.Section
			fmt.Fprintf(&b, "\r\n[%s]\r\n", section)
		}
		v := f.Value
		if f.Unit != "" {
			v += " " + f.Unit
		}
		fmt.Fprintf(&b, "%s: %s\r\n", f.Label, v)
	}
	return []byte(b.String())
}
