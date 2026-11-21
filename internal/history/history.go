// Package history records every setup version in the library and works out where
// setups came from (spec D-02, D-12, D-40, D-41, D-43, D-51 … D-53, D-61, D-62).
//
// Rules, in short:
//   - same file path, changed scored values → new version of that setup;
//   - a file disappears → the setup is kept, marked "no longer in the game folder";
//   - a new file that matches a setup whose file disappeared in the same scan → that setup
//     continues under the new name (renamed);
//   - otherwise a new file similar to an existing setup → a branch of it;
//   - otherwise → a new setup of its own.
//
// The game's setup folder is never touched; everything is stored in the library.
package history

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"setupman/internal/compare"
	"setupman/internal/game"
	"setupman/internal/naming"
	"setupman/internal/setup"
	"setupman/internal/store"
)

// Config holds the thresholds for linking setups.
type Config struct {
	Threshold        float64          // % similarity to link setups (D-53, default 80)
	NameThreshold    float64          // % similarity when the names match (D-61, default 65)
	EnduranceMinutes int              // stint length above which a race setup counts as Endurance (D-69)
	GameRunning      bool             // the game runs now, so a new file can be a save from the game (D-114)
	TempRules        naming.TempRules // how each author writes temperatures in names (D-146)
}

// DefaultConfig is used when nothing else is set.
var DefaultConfig = Config{Threshold: 80, NameThreshold: 65, EnduranceMinutes: naming.DefaultEnduranceMinutes}

// Parser turns stored file content back into fields (game-specific).
type Parser func(content []byte) ([]setup.Field, error)

// Result counts what a sync did.
type Result struct {
	Imported    int `json:"imported"`
	NewSetups   int `json:"newSetups"`
	NewVersions int `json:"newVersions"`
	Branches    int `json:"branches"`
	Renamed     int `json:"renamed"`
	Removed     int `json:"removed"`
	Returned    int `json:"returned"` // library-only setups whose file came back
	// NewSetupIDs are setups that appeared in this sync (new setups and branches).
	NewSetupIDs []int64 `json:"newSetupIds,omitempty"`
}

func indexByName(list []*store.Setup, name string) int {
	for i, s := range list {
		if strings.EqualFold(s.Name, name) {
			return i
		}
	}
	return -1
}

// file is one setup file from a scan, with its car and track.
type file struct {
	car, track string
	s          setup.Setup
}

func filesOf(scan *game.Scan) []file {
	var out []file
	for _, c := range scan.Cars {
		for _, t := range c.Tracks {
			for _, s := range t.Setups {
				out = append(out, file{car: c.ID, track: t.ID, s: s})
			}
		}
	}
	return out
}

func groupKey(car, track string) string { return strings.ToLower(car) + "/" + strings.ToLower(track) }

// fieldCache parses stored versions once per sync.
type fieldCache struct {
	q     store.Q
	parse Parser
	byID  map[int64][]setup.Field
}

func (c *fieldCache) latest(setupID int64) (*store.Version, []setup.Field) {
	v, err := c.q.LatestVersion(setupID)
	if err != nil || v == nil {
		return nil, nil
	}
	return v, c.fields(v)
}

func (c *fieldCache) fields(v *store.Version) []setup.Field {
	if f, ok := c.byID[v.ID]; ok {
		return f
	}
	f, err := c.parse(v.Content)
	if err != nil {
		f = nil
	}
	c.byID[v.ID] = f
	return f
}

// linkOK applies D-53 and D-61.
func linkOK(score float64, nameA, nameB string, cfg Config) bool {
	return score >= cfg.Threshold || (score >= cfg.NameThreshold && naming.SameName(nameA, nameB))
}

// Sync records one scan of a game in the library. The first sync of a game imports
// everything and rebuilds a probable history (D-40).
func Sync(st *store.Store, gameID string, scan *game.Scan, parse Parser, cfg Config) (Result, error) {
	var res Result
	err := st.Tx(func(q store.Q) error {
		imported, err := q.Imported(gameID)
		if err != nil {
			return err
		}
		if !imported {
			n, err := importAll(q, gameID, scan, parse, cfg)
			res.Imported = n
			return err
		}
		return syncChanges(q, gameID, scan, parse, cfg, &res)
	})
	return res, err
}

// importAll adds every current file, oldest first, linking each to its most similar older
// setup as a probable parent.
func importAll(q store.Q, gameID string, scan *game.Scan, parse Parser, cfg Config) (int, error) {
	files := filesOf(scan)
	sort.SliceStable(files, func(i, j int) bool { return files[i].s.Modified.Before(files[j].s.Modified) })
	type done struct {
		setupID, versionID int64
		name               string
		fields             []setup.Field
	}
	byGroup := map[string][]done{}
	for _, f := range files {
		s := newSetup(gameID, f, cfg)
		best, bestScore := -1, 0.0
		for i, d := range byGroup[groupKey(f.car, f.track)] {
			sc := compare.Compare(d.fields, f.s.Fields).Score
			if linkOK(sc, d.name, f.s.Name, cfg) && sc > bestScore {
				best, bestScore = i, sc
			}
		}
		if best >= 0 {
			d := byGroup[groupKey(f.car, f.track)][best]
			s.ParentVersionID, s.LinkKind, s.LinkScore = d.versionID, store.LinkProbable, bestScore
		}
		if err := q.InsertSetup(s); err != nil {
			return 0, err
		}
		v := &store.Version{SetupID: s.ID, Fingerprint: f.s.Fingerprint, Content: f.s.Raw, FileModified: f.s.Modified, Source: "import"}
		if err := q.InsertVersion(v); err != nil {
			return 0, err
		}
		s.DiskVersionID = v.ID
		if err := q.UpdateSetup(s); err != nil {
			return 0, err
		}
		byGroup[groupKey(f.car, f.track)] = append(byGroup[groupKey(f.car, f.track)], done{s.ID, v.ID, f.s.Name, f.s.Fields})
	}
	if err := q.SetImported(gameID); err != nil {
		return 0, err
	}
	if len(files) > 0 {
		err := q.InsertEvent(store.Event{Game: gameID, Kind: "imported", Details: map[string]any{"count": len(files)}})
		return len(files), err
	}
	return 0, nil
}

func newSetup(gameID string, f file, cfg Config) *store.Setup {
	m := naming.ParseWith(f.s.Name, cfg.TempRules)
	return &store.Setup{Game: gameID, Car: f.car, Track: f.track, Name: f.s.Name, RelPath: f.s.RelPath,
		Session: naming.GuessSession(m, cfg.EnduranceMinutes), Meta: m, LinkKind: store.LinkNone}
}

// RefreshSessions re-reads the details of every setup name the user hasn't edited by hand (e.g.
// after a change of how an author writes temperatures, D-146) and re-guesses the session, e.g.
// after the endurance setting changed (D-69). Name fields are re-read unless edited by hand.
func RefreshSessions(st *store.Store, gameID string, cfg Config) error {
	return st.Tx(func(q store.Q) error {
		all, err := q.SetupsForGame(gameID)
		if err != nil {
			return err
		}
		for i := range all {
			s := &all[i]
			changed := false
			if !s.MetaManual {
				if m := naming.ParseWith(s.Name, cfg.TempRules); !reflect.DeepEqual(m, s.Meta) {
					s.Meta, changed = m, true
				}
			}
			if !s.SessionManual {
				if g := naming.GuessSession(s.Meta, cfg.EnduranceMinutes); g != s.Session {
					s.Session, changed = g, true
				}
			}
			if changed {
				if err := q.UpdateSetup(s); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func syncChanges(q store.Q, gameID string, scan *game.Scan, parse Parser, cfg Config, res *Result) error {
	cache := &fieldCache{q: q, parse: parse, byID: map[int64][]setup.Field{}}
	all, err := q.SetupsForGame(gameID)
	if err != nil {
		return err
	}
	byPath := map[string]*store.Setup{}
	for i := range all {
		if all[i].RelPath != "" {
			byPath[strings.ToLower(all[i].RelPath)] = &all[i]
		}
	}

	matched := map[int64]bool{}
	var newFiles []file
	for _, f := range filesOf(scan) {
		s := byPath[strings.ToLower(f.s.RelPath)]
		if s == nil {
			newFiles = append(newFiles, f)
			continue
		}
		matched[s.ID] = true
		if err := addVersionIfChanged(q, cache, s, f, res); err != nil {
			return err
		}
	}

	// Setups whose file disappeared in this scan: candidates for a rename.
	vanished := map[string][]*store.Setup{}
	for i := range all {
		s := &all[i]
		if s.RelPath != "" && !matched[s.ID] {
			vanished[groupKey(s.Car, s.Track)] = append(vanished[groupKey(s.Car, s.Track)], s)
		}
	}

	// Setups that only exist in the library (moved there, or removed earlier).
	libOnly := map[string][]*store.Setup{}
	for i := range all {
		if s := &all[i]; s.RelPath == "" {
			libOnly[groupKey(s.Car, s.Track)] = append(libOnly[groupKey(s.Car, s.Track)], s)
		}
	}

	sort.SliceStable(newFiles, func(i, j int) bool { return newFiles[i].s.Modified.Before(newFiles[j].s.Modified) })
	for _, f := range newFiles {
		key := groupKey(f.car, f.track)

		// 0. Re-attach: a file with the name of a library-only setup is that setup coming back
		//    (a restored backup, or a file copied back by hand). Never a duplicate.
		if i := indexByName(libOnly[key], f.s.Name); i >= 0 {
			s := libOnly[key][i]
			libOnly[key] = append(libOnly[key][:i], libOnly[key][i+1:]...)
			s.RelPath, s.Name, s.Fresh = f.s.RelPath, f.s.Name, false
			if err := q.UpdateSetup(s); err != nil {
				return err
			}
			if err := addVersionIfChanged(q, cache, s, f, res); err != nil {
				return err
			}
			res.Returned++
			if err := q.InsertEvent(store.Event{Game: gameID, Kind: "returned", SetupID: s.ID,
				Details: map[string]any{"name": s.Name}}); err != nil {
				return err
			}
			continue
		}

		// 1. Continuation: the old file is gone (D-51).
		bi, bScore := -1, 0.0
		for i, s := range vanished[key] {
			_, fields := cache.latest(s.ID)
			if sc := compare.Compare(fields, f.s.Fields).Score; linkOK(sc, s.Name, f.s.Name, cfg) && sc > bScore {
				bi, bScore = i, sc
			}
		}
		if bi >= 0 {
			s := vanished[key][bi]
			vanished[key] = append(vanished[key][:bi], vanished[key][bi+1:]...)
			old := s.Name
			s.RelPath, s.Name = f.s.RelPath, f.s.Name
			if !s.MetaManual {
				s.Meta = naming.ParseWith(f.s.Name, cfg.TempRules)
			}
			if !s.SessionManual {
				s.Session = naming.GuessSession(s.Meta, cfg.EnduranceMinutes)
			}
			if err := q.UpdateSetup(s); err != nil {
				return err
			}
			if err := addVersionIfChanged(q, cache, s, f, res); err != nil {
				return err
			}
			res.Renamed++
			if err := q.InsertEvent(store.Event{Game: gameID, Kind: "renamed", SetupID: s.ID,
				Details: map[string]any{"from": old, "to": f.s.Name}}); err != nil {
				return err
			}
			continue
		}

		// 2. Branch of the most similar setup, or 3. a new setup.
		s := newSetup(gameID, f, cfg)
		group, err := q.SetupsForTrack(gameID, f.car, f.track)
		if err != nil {
			return err
		}
		var parent *store.Setup
		var parentVersion int64
		pScore := 0.0
		for i := range group {
			v, fields := cache.latest(group[i].ID)
			if v == nil {
				continue
			}
			if sc := compare.Compare(fields, f.s.Fields).Score; linkOK(sc, group[i].Name, f.s.Name, cfg) && sc > pScore {
				parent, parentVersion, pScore = &group[i], v.ID, sc
			}
		}
		if parent != nil {
			s.ParentVersionID, s.LinkKind, s.LinkScore = parentVersion, store.LinkObserved, pScore
		}
		// A new file in a managed folder that SetupMan didn't put there, while the game runs, is a
		// save from the game: mark it so the user can keep it or move it to the library (D-80).
		// Files copied in while the game is closed are not (D-114).
		if managed, err := q.IsManagedTrack(gameID, f.car, f.track); err == nil && managed && cfg.GameRunning {
			s.Fresh = true
		}
		if err := q.InsertSetup(s); err != nil {
			return err
		}
		v := &store.Version{SetupID: s.ID, Fingerprint: f.s.Fingerprint, Content: f.s.Raw, FileModified: f.s.Modified, Source: "change"}
		if err := q.InsertVersion(v); err != nil {
			return err
		}
		s.DiskVersionID = v.ID
		if err := q.UpdateSetup(s); err != nil {
			return err
		}
		e := store.Event{Game: gameID, SetupID: s.ID, VersionID: v.ID, Details: map[string]any{"name": s.Name}}
		res.NewSetupIDs = append(res.NewSetupIDs, s.ID)
		if parent != nil {
			res.Branches++
			e.Kind = "branch"
			e.Details["parent"] = parent.Name
			e.Details["score"] = pScore
		} else {
			res.NewSetups++
			e.Kind = "new-setup"
		}
		if err := q.InsertEvent(e); err != nil {
			return err
		}
	}

	// Files that are really gone: keep the setup and its history.
	for _, list := range vanished {
		for _, s := range list {
			s.RelPath, s.DiskVersionID = "", 0
			if err := q.UpdateSetup(s); err != nil {
				return err
			}
			res.Removed++
			if err := q.InsertEvent(store.Event{Game: gameID, Kind: "removed", SetupID: s.ID,
				Details: map[string]any{"name": s.Name}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// addVersionIfChanged adds a version when scored values changed (D-52); otherwise it
// only refreshes the stored content.
//
// The file is compared with the version that is in the game folder (DiskVersionID), not with
// the newest version: a change made in SetupMan's library (not yet in the folder) must not be
// undone by the older file on disk.
func addVersionIfChanged(q store.Q, cache *fieldCache, s *store.Setup, f file, res *Result) error {
	latest, latestFields := cache.latest(s.ID)
	disk := latest
	if s.DiskVersionID != 0 && (latest == nil || s.DiskVersionID != latest.ID) {
		if dv, err := q.Version(s.DiskVersionID); err == nil && dv != nil {
			disk = dv
		}
	}
	setDisk := func(id int64) error {
		if s.DiskVersionID == id {
			return nil
		}
		s.DiskVersionID = id
		return q.UpdateSetup(s)
	}
	switch {
	case disk != nil && disk.Fingerprint == f.s.Fingerprint: // file unchanged (scored values)
		if string(disk.Content) != string(f.s.Raw) {
			if err := q.UpdateVersionContent(disk.ID, f.s.Raw, f.s.Modified); err != nil {
				return err
			}
		}
		return setDisk(disk.ID)
	case latest != nil && latest.Fingerprint == f.s.Fingerprint: // the folder now has the newest version
		return setDisk(latest.ID)
	}
	// The file itself changed (saved in the game): a new version.
	v := &store.Version{SetupID: s.ID, Fingerprint: f.s.Fingerprint, Content: f.s.Raw, FileModified: f.s.Modified, Source: "change"}
	if err := q.InsertVersion(v); err != nil {
		return err
	}
	if err := setDisk(v.ID); err != nil {
		return err
	}
	res.NewVersions++
	prev := latestFields
	if disk != nil {
		prev = cache.fields(disk)
	}
	return q.InsertEvent(store.Event{Game: s.Game, Kind: "new-version", SetupID: s.ID, VersionID: v.ID,
		Details: map[string]any{"name": s.Name, "seq": v.Seq, "changes": Summary(compare.Compare(prev, f.s.Fields), 5)}})
}

// Summary describes the scored differences of a comparison, e.g. "TC -1, Rear wing +1".
func Summary(r compare.Result, max int) string {
	var parts []string
	for _, d := range r.Diffs {
		if d.Excluded {
			continue
		}
		if len(parts) == max {
			parts = append(parts, "…")
			break
		}
		if d.Delta != "" {
			parts = append(parts, d.Label+" "+d.Delta)
		} else {
			parts = append(parts, fmt.Sprintf("%s %s → %s", d.Label, orDash(d.A), orDash(d.B)))
		}
	}
	return strings.Join(parts, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}
