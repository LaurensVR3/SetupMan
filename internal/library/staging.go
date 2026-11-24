package library

// Staging: SetupMan decides which setups are in the game folder (spec D-06, D-73 … D-83).
// This is the only code that writes to a game folder. Rules:
//   - only for games where the user switched management on (which made a backup first);
//   - an existing file of another setup is never overwritten (a number is added instead);
//   - a file is only removed after its exact content is safely in the library;
//   - nothing changes while the player is driving: changes are queued and run afterwards.

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/folderbackup"
	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/lastopened"
	"setupman/internal/settings"
	"setupman/internal/store"
)

// ErrQueued means the change will run as soon as the player stops driving.
var ErrQueued = errors.New("queued: this change will be applied when you stop driving")

// ErrNotManaged means the user hasn't let SetupMan manage this game's folder.
var ErrNotManaged = errors.New("this requires folder management for this game (Settings → Games)")

// StageResult tells the UI what happened.
type StageResult struct {
	Queued      bool   `json:"queued"`
	GameRunning bool   `json:"gameRunning"` // show "ACC may need its setup screen reopened" (D-79)
	Message     string `json:"message,omitempty"`
}

// ManageResult is the outcome of switching management on.
type ManageResult struct {
	Managed  bool     `json:"managed"`
	BackupID string   `json:"backupId,omitempty"`
	Checked  int      `json:"checked"`            // files verified byte for byte
	Problems []string `json:"problems,omitempty"` // why it could not be switched on
}

// ---------- management on/off ----------

// SetManaged switches management of a game's folder on or off. Switching on first scans,
// then makes a folder backup, then checks that every file is in the library byte for byte
// (D-74). Switching off leaves the folder exactly as it is.
func (l *Library) SetManaged(gameID string, on bool) (ManageResult, error) {
	g := l.game(gameID)
	if g == nil || l.db == nil || l.Backups == nil {
		return ManageResult{}, errors.New("This is not available for this game.")
	}
	if !on {
		return ManageResult{}, l.saveGameSetting(gameID, func(gs *settings.Game) { gs.Managed, gs.ManagedFolder = false, "" })
	}
	if r := game.ReadOnlyReason(g); r != "" {
		return ManageResult{}, errors.New(r)
	}
	if l.managed(gameID) {
		return ManageResult{Managed: true}, nil // already on: no second "before managing" backup
	}
	if l.Live() == game.LiveDriving {
		return ManageResult{}, errors.New("folder management can't be enabled while driving; enable it from the pits or the menu")
	}
	l.mu.RLock()
	folder := l.states[gameID].folder
	l.mu.RUnlock()
	if folder == "" {
		return ManageResult{}, errors.New("this game has no setups folder yet")
	}

	l.Rescan(gameID) // everything on disk is now in the library

	l.folderMu.Lock()
	defer l.folderMu.Unlock()
	b, err := l.Backups.Create(gameID, folder, folderbackup.ReasonBeforeManaging)
	if err != nil {
		return ManageResult{}, fmt.Errorf("the backup could not be made, nothing was changed: %w", err)
	}
	res := ManageResult{BackupID: b.ID}
	scan, err := g.Scan(folder)
	if err != nil {
		return res, err
	}
	for _, c := range scan.Cars {
		for _, t := range c.Tracks {
			for _, s := range t.Setups {
				res.Checked++
				ls, err := l.db.SetupByPath(gameID, s.RelPath)
				if err != nil || ls == nil {
					res.Problems = append(res.Problems, s.RelPath+" is not stored in SetupMan yet")
					continue
				}
				v, err := l.db.LatestVersion(ls.ID)
				if ls.DiskVersionID != 0 {
					v, err = l.db.Version(ls.DiskVersionID) // the version that should be in the file
				}
				if err != nil || v == nil || !bytes.Equal(v.Content, s.Raw) {
					res.Problems = append(res.Problems, s.RelPath+" differs from SetupMan's copy")
				}
			}
		}
	}
	if len(res.Problems) > 0 {
		return res, nil // management stays off; the UI explains why
	}
	if err := l.saveGameSetting(gameID, func(gs *settings.Game) { gs.Managed, gs.ManagedFolder = true, folder }); err != nil {
		return res, err
	}
	res.Managed = true
	return res, nil
}

func (l *Library) saveGameSetting(gameID string, change func(*settings.Game)) error {
	st := l.store.Load()
	gs := st.GameOrDefault(gameID)
	change(&gs)
	st.Games[gameID] = gs
	if err := l.store.Save(st); err != nil {
		return err
	}
	l.mu.Lock()
	l.settings = st
	l.mu.Unlock()
	l.publish(Event{Type: "games-changed"})
	return nil
}

// A read-only game (e.g. AC Rally: all setups in one save file) is never managed, whatever the settings say.
func (l *Library) managed(gameID string) bool {
	if g := l.game(gameID); g == nil || game.ReadOnlyReason(g) != "" {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.managedLocked(l.game(gameID))
}

// managedLocked is managed for callers that hold l.mu: on, and switched on for the folder in use (D-115).
func (l *Library) managedLocked(g game.Game) bool {
	s := l.states[g.ID()]
	return game.ReadOnlyReason(g) == "" && s != nil && l.settings.GameOrDefault(g.ID()).ManagedFor(s.folder)
}

// migrateManagedFolders: settings from before D-115 have no managedFolder. Take it from the
// "before managing" backup, which shows which folder was checked; without one, management is off.
func (l *Library) migrateManagedFolders() {
	if l.Backups == nil {
		return
	}
	st := l.store.Load()
	changed := false
	for id, gs := range st.Games {
		if !gs.Managed || gs.ManagedFolder != "" {
			continue
		}
		gs.Managed = false
		if list, err := l.Backups.List(id); err == nil {
			for _, b := range list { // newest first
				if b.Reason == folderbackup.ReasonBeforeManaging {
					gs.Managed, gs.ManagedFolder = true, b.Folder
					break
				}
			}
		}
		st.Games[id] = gs
		changed = true
	}
	if changed {
		if err := l.store.Save(st); err != nil {
			log.Printf("settings: %v", err)
		}
	}
}

// ---------- staging operations ----------

// Stage puts a setup in the game folder: its latest version, or versionID if given (an
// older version is first recorded as a new "restored" version).
func (l *Library) Stage(setupID, versionID int64) (StageResult, error) {
	return l.run(setupGameOrEmpty(l, setupID), "stage", map[string]any{"setupId": setupID, "versionId": versionID})
}

// Unstage moves a setup to the library: its file is removed from the game folder once its
// exact content is in the library.
func (l *Library) Unstage(setupID int64) (StageResult, error) {
	return l.run(setupGameOrEmpty(l, setupID), "unstage", map[string]any{"setupId": setupID})
}

// KeepOnly makes a car/track folder hold exactly these setups (D-75, D-76).
func (l *Library) KeepOnly(gameID, car, track string, setupIDs []int64) (StageResult, error) {
	ids := make([]any, len(setupIDs))
	for i, id := range setupIDs {
		ids[i] = float64(id)
	}
	return l.run(gameID, "keep-only", map[string]any{"car": car, "track": track, "setupIds": ids})
}

// Keep marks a new in-game save as reviewed: it stays in the game folder (D-80).
func (l *Library) Keep(setupID int64) error {
	if l.db == nil {
		return ErrNoHistory
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return history.ErrNotFound
	}
	s.Fresh = false
	if err := l.db.UpdateSetup(s); err != nil {
		return err
	}
	l.publish(Event{Type: "history-changed"})
	return nil
}

func setupGameOrEmpty(l *Library, id int64) string {
	g, _ := l.setupGame(id)
	return g
}

// run carries out a staging operation now, or queues it while driving.
func (l *Library) run(gameID, kind string, payload map[string]any) (StageResult, error) {
	if l.db == nil || gameID == "" {
		return StageResult{}, history.ErrNotFound
	}
	if !l.managed(gameID) {
		return StageResult{}, ErrNotManaged
	}
	if l.Live() == game.LiveDriving {
		if err := l.db.AddPendingOp(store.PendingOp{Game: gameID, Kind: kind, Payload: payload}); err != nil {
			return StageResult{}, err
		}
		log.Printf("queued while driving: %s %v", kind, payload)
		l.publish(Event{Type: "history-changed"})
		return StageResult{Queued: true, Message: ErrQueued.Error()}, nil
	}
	if err := l.apply(gameID, kind, payload); err != nil {
		return StageResult{}, err
	}
	return StageResult{GameRunning: l.Live() != game.LiveUnknown}, nil
}

// QueuedCount returns how many staging changes wait for the player to stop driving.
func (l *Library) QueuedCount() int {
	if l.db == nil {
		return 0
	}
	ops, _ := l.db.PendingOps()
	return len(ops)
}

func (l *Library) runPendingOps() {
	if l.db == nil {
		return
	}
	ops, err := l.db.PendingOps()
	if err != nil || len(ops) == 0 {
		return
	}
	for _, op := range ops {
		if l.Live() == game.LiveDriving {
			return
		}
		if err := l.apply(op.Game, op.Kind, op.Payload); err != nil {
			// Tell the user in Recent changes: they were told it would happen (D-138).
			log.Printf("queued %s: %v", op.Kind, err)
			e := store.Event{Game: op.Game, Kind: "queued-failed", Details: map[string]any{"change": op.Kind, "error": err.Error()}}
			if id := int64Of(op.Payload["setupId"]); id != 0 {
				e.SetupID = id
				if s, _ := l.db.Setup(id); s != nil {
					e.Details["name"] = s.Name
				}
			}
			_ = l.db.InsertEvent(e)
			l.publish(Event{Type: "history-changed"})
		}
		l.db.DeletePendingOp(op.ID)
	}
}

// ManyResult is what happened to one setup in a bulk show / hide.
type ManyResult struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

// StageMany puts several setups (their newest versions) in the game folder: one folder lock and
// one rescan per game, instead of one rescan per setup (D-117, D-138).
func (l *Library) StageMany(ids []int64) (StageResult, []ManyResult, error) {
	return l.runMany("stage", ids)
}

// UnstageMany takes several setups out of the game folder, like Unstage, in one go.
func (l *Library) UnstageMany(ids []int64) (StageResult, []ManyResult, error) {
	return l.runMany("unstage", ids)
}

func (l *Library) runMany(kind string, ids []int64) (StageResult, []ManyResult, error) {
	res, out := StageResult{}, []ManyResult{}
	if l.db == nil {
		return res, out, ErrNoHistory
	}
	byGame := map[string][]int64{}
	var order []string
	names := map[int64]string{}
	for _, id := range ids {
		s, err := l.db.Setup(id)
		if err != nil || s == nil || !s.DeletedAt.IsZero() {
			out = append(out, ManyResult{ID: id, Error: history.ErrNotFound.Error()})
			continue
		}
		names[id] = s.Name
		if !l.managed(s.Game) {
			out = append(out, ManyResult{ID: id, Name: s.Name, Error: ErrNotManaged.Error()})
			continue
		}
		if _, ok := byGame[s.Game]; !ok {
			order = append(order, s.Game)
		}
		byGame[s.Game] = append(byGame[s.Game], id)
	}
	for _, gameID := range order {
		gids := byGame[gameID]
		if l.Live() == game.LiveDriving {
			for _, id := range gids {
				r := ManyResult{ID: id, Name: names[id]}
				if err := l.db.AddPendingOp(store.PendingOp{Game: gameID, Kind: kind, Payload: map[string]any{"setupId": float64(id), "versionId": float64(0)}}); err != nil {
					r.Error = err.Error()
				}
				out = append(out, r)
			}
			res.Queued = true
			continue
		}
		func() {
			l.folderMu.Lock()
			defer l.folderMu.Unlock()
			for _, id := range gids {
				r := ManyResult{ID: id, Name: names[id]}
				var err error
				if kind == "stage" {
					err = l.stageLocked(gameID, id, 0)
				} else {
					err = l.unstageLocked(gameID, id)
				}
				if err != nil {
					r.Error = err.Error()
				}
				out = append(out, r)
			}
		}()
		l.Rescan(gameID) // the scan confirms the result and refreshes the pages
	}
	if res.Queued {
		res.Message = ErrQueued.Error()
	} else {
		res.GameRunning = l.Live() != game.LiveUnknown
	}
	l.publish(Event{Type: "history-changed"})
	return res, out, nil
}

// unstageMany takes several setups out of the game folder under one folder lock, then rescans
// once. It returns the setups that failed, with the reason. The caller checks driving.
func (l *Library) unstageMany(gameID string, ids []int64) map[int64]error {
	failed := map[int64]error{}
	func() {
		l.folderMu.Lock()
		defer l.folderMu.Unlock()
		for _, id := range ids {
			if err := l.unstageLocked(gameID, id); err != nil {
				failed[id] = err
			}
		}
	}()
	l.Rescan(gameID) // the scan confirms the result and refreshes the pages
	l.publish(Event{Type: "history-changed"})
	return failed
}

// apply does the actual work under the folder lock, then rescans.
func (l *Library) apply(gameID, kind string, p map[string]any) error {
	err := func() error {
		l.folderMu.Lock()
		defer l.folderMu.Unlock()
		switch kind {
		case "stage":
			return l.stageLocked(gameID, int64Of(p["setupId"]), int64Of(p["versionId"]))
		case "unstage":
			return l.unstageLocked(gameID, int64Of(p["setupId"]))
		case "keep-only":
			return l.keepOnlyLocked(gameID, fmt.Sprint(p["car"]), fmt.Sprint(p["track"]), p["setupIds"])
		}
		return fmt.Errorf("unknown change %q", kind)
	}()
	l.Rescan(gameID) // the scan confirms the result and refreshes the pages
	l.publish(Event{Type: "history-changed"})
	return err
}

func int64Of(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// gameFolder returns the game's setups folder and the on-disk folder name of a track.
func (l *Library) gameFolder(gameID, car, track string) (root, trackFolder string, err error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	st := l.states[gameID]
	if st == nil || st.folder == "" {
		return "", "", errors.New("this game has no setups folder")
	}
	trackFolder = track
	if st.scan != nil {
		for _, c := range st.scan.Cars {
			if strings.EqualFold(c.ID, car) {
				for _, t := range c.Tracks {
					if t.ID == strings.ToLower(track) {
						trackFolder = t.Folder
					}
				}
			}
		}
	}
	return st.folder, trackFolder, nil
}

func (l *Library) stageLocked(gameID string, setupID, versionID int64) error {
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil || s.Game != gameID {
		return history.ErrNotFound
	}
	root, trackFolder, err := l.gameFolder(gameID, s.Car, s.Track)
	if err != nil {
		return err
	}
	rel := s.RelPath
	if rel == "" { // not in the folder yet: pick a free name (D-77)
		rel = freeName(root, s.Car, trackFolder, cleanName(s.Name, "Setup"), game.Ext(l.game(gameID)))
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if !isInside(root, target) {
		return errors.New("SetupMan only changes files inside the setups folder.")
	}
	// The file may hold a save from the game that SetupMan hasn't read yet (it reads new saves a
	// few seconds after they appear). Store it first, and don't overwrite it unasked (D-138).
	if s.RelPath != "" {
		fresh, err := l.secureFile(gameID, s, target)
		if err != nil {
			return err
		}
		if fresh {
			return fmt.Errorf("%s was just saved in %s with other values. SetupMan stored that save as a new version and did not change the file; check the setup's History and try again", s.Name, game.Short(l.game(gameID)))
		}
	}

	latest, err := l.db.LatestVersion(s.ID)
	if err != nil || latest == nil {
		return errors.New("this setup has no stored version")
	}
	v := latest
	if versionID != 0 && versionID != latest.ID {
		old, err := l.db.Version(versionID)
		if err != nil || old == nil || old.SetupID != s.ID {
			return history.ErrNotFound
		}
		// Putting an older version back: record it as the newest version, marked "restored".
		v = &store.Version{SetupID: s.ID, Fingerprint: old.Fingerprint, Content: old.Content,
			FileModified: time.Now(), Source: "restored", Note: fmt.Sprintf("Version %d put back in the game folder", old.Seq)}
		if err := l.db.InsertVersion(v); err != nil {
			return err
		}
	}

	prev := *s
	s.RelPath, s.Fresh, s.DiskVersionID = rel, false, v.ID
	s.Name = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if err := l.db.UpdateSetup(s); err != nil {
		return err
	}
	if err := writeFileAtomic(target, v.Content); err != nil {
		_ = l.db.UpdateSetup(&prev) // the folder is as it was
		return err
	}
	// Keep the date the setup was saved, so lists and game-version estimates stay right (D-103).
	if !v.FileModified.IsZero() {
		_ = os.Chtimes(target, v.FileModified, v.FileModified)
	}
	l.noteOwnTouch(target) // not opened by the game until this time changes (D-147)
	_ = l.db.SetManagedTrack(gameID, s.Car, s.Track)
	return l.db.InsertEvent(store.Event{Game: gameID, Kind: "staged", SetupID: s.ID, VersionID: v.ID,
		Details: map[string]any{"name": s.Name, "seq": v.Seq}})
}

func (l *Library) unstageLocked(gameID string, setupID int64) error {
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil || s.Game != gameID {
		return history.ErrNotFound
	}
	if s.RelPath == "" {
		return nil // already library only
	}
	root, _, err := l.gameFolder(gameID, s.Car, s.Track)
	if err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(s.RelPath))
	if !isInside(root, target) {
		return errors.New("SetupMan only changes files inside the setups folder.")
	}
	// Make sure the library holds exactly this content before the file goes.
	if _, err := l.secureFile(gameID, s, target); err != nil {
		return err
	}
	// The file goes first: if Windows can't remove it (e.g. the game or a virus scanner holds it),
	// the library still says it is in the folder, so the next scan doesn't add it again (D-138).
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s could not be removed from the game folder: %w", s.RelPath, err)
	}
	name := s.Name
	s.RelPath, s.Fresh, s.DiskVersionID = "", false, 0
	if err := l.db.UpdateSetup(s); err != nil {
		return err // the next scan records the file as gone; its content is stored
	}
	_ = l.db.SetManagedTrack(gameID, s.Car, s.Track)
	return l.db.InsertEvent(store.Event{Game: gameID, Kind: "moved-to-library", SetupID: s.ID,
		Details: map[string]any{"name": name}})
}

// secureFile makes sure the exact bytes of a setup's file in the game folder are stored in the
// library, before SetupMan removes or overwrites that file. It compares the file with the version
// that should be in the folder (a newer edit may exist only in the library). newVersion is true
// when the file held values SetupMan had not read yet (e.g. saved in the game seconds ago); they
// are stored as a new version. A missing file is fine (nothing to keep).
func (l *Library) secureFile(gameID string, s *store.Setup, target string) (newVersion bool, err error) {
	raw, err := lastopened.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	disk, _ := l.db.LatestVersion(s.ID)
	if s.DiskVersionID != 0 {
		if dv, _ := l.db.Version(s.DiskVersionID); dv != nil {
			disk = dv
		}
	}
	storedID := int64(0)
	if disk != nil && bytes.Equal(disk.Content, raw) {
		storedID = disk.ID
	} else {
		fields, perr := l.game(gameID).ParseSetup(raw)
		if perr != nil {
			return false, fmt.Errorf("%s can't be read, so it was left in the game folder as it is: %w", s.RelPath, perr)
		}
		fp := compare.Fingerprint(fields)
		modified := time.Now()
		if info, err := os.Stat(target); err == nil {
			modified = info.ModTime()
		}
		if disk != nil && disk.Fingerprint == fp {
			if err := l.db.UpdateVersionContent(disk.ID, raw, modified); err != nil {
				return false, err
			}
			storedID = disk.ID
		} else {
			nv := &store.Version{SetupID: s.ID, Fingerprint: fp, Content: raw, FileModified: modified, Source: "change"}
			if err := l.db.InsertVersion(nv); err != nil {
				return false, err
			}
			storedID, newVersion = nv.ID, true
			s.DiskVersionID = nv.ID
			if err := l.db.UpdateSetup(s); err != nil {
				return false, err
			}
		}
	}
	check, _ := l.db.Version(storedID)
	if check == nil || !bytes.Equal(check.Content, raw) {
		return false, fmt.Errorf("%s could not be stored safely, so it was left in the game folder", s.RelPath)
	}
	return newVersion, nil
}

func (l *Library) keepOnlyLocked(gameID, car, track string, raw any) error {
	keep := map[int64]bool{}
	if list, ok := raw.([]any); ok {
		for _, v := range list {
			keep[int64Of(v)] = true
		}
	}
	setups, err := l.db.SetupsForTrack(gameID, car, track)
	if err != nil {
		return err
	}
	if err := l.db.SetManagedTrack(gameID, car, track); err != nil {
		return err
	}
	for _, s := range setups {
		switch {
		case keep[s.ID] && s.RelPath == "":
			if err := l.stageLocked(gameID, s.ID, 0); err != nil {
				return err
			}
		case !keep[s.ID] && s.RelPath != "":
			if err := l.unstageLocked(gameID, s.ID); err != nil {
				return err
			}
		case keep[s.ID] && s.Fresh:
			s.Fresh = false
			l.db.UpdateSetup(&s)
		}
	}
	return nil
}

// tidyOnGameClose moves new in-game saves that weren't kept to the library, for games
// where the user switched this on (D-80).
func (l *Library) tidyOnGameClose() {
	if l.db == nil {
		return
	}
	for _, g := range l.games {
		gs := l.Settings().GameOrDefault(g.ID())
		if !l.managed(g.ID()) || !gs.TidyOnGameClose {
			continue
		}
		all, err := l.db.SetupsForGame(g.ID())
		if err != nil {
			continue
		}
		var ids []int64
		for _, s := range all {
			if s.Fresh && s.RelPath != "" {
				ids = append(ids, s.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		// One folder lock and one rescan for all of them (D-138).
		for id, err := range l.unstageMany(g.ID(), ids) {
			log.Printf("tidy setup %d: %v", id, err)
		}
	}
}

// ---------- folder backups ----------

// CreateBackup makes an ACC setups folder backup of a game (D-82).
func (l *Library) CreateBackup(gameID string) (folderbackup.Summary, error) {
	return l.createBackup(gameID, folderbackup.ReasonManual)
}

// BackupAllFolders makes an ACC setups folder backup of every game that has a folder
// (used by full SetupMan backups, D-82).
func (l *Library) BackupAllFolders() ([]folderbackup.Summary, error) {
	var out []folderbackup.Summary
	for _, g := range l.games {
		if sum, err := l.createBackup(g.ID(), folderbackup.ReasonFullBackup); err == nil {
			out = append(out, sum)
		} else if l.folderOf(g.ID()) != "" {
			return out, err
		}
	}
	return out, nil
}

func (l *Library) folderOf(gameID string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if st := l.states[gameID]; st != nil {
		return st.folder
	}
	return ""
}

func (l *Library) createBackup(gameID, reason string) (folderbackup.Summary, error) {
	l.mu.RLock()
	st := l.states[gameID]
	folder := ""
	if st != nil {
		folder = st.folder
	}
	l.mu.RUnlock()
	if folder == "" || l.Backups == nil {
		return folderbackup.Summary{}, errors.New("this game has no setups folder")
	}
	l.folderMu.Lock()
	defer l.folderMu.Unlock()
	return l.Backups.Create(gameID, folder, reason)
}

// PlanRestore says what restoring a backup into the game's setups folder would change (D-81, D-138).
func (l *Library) PlanRestore(gameID, id string) (folderbackup.Plan, error) {
	if l.Backups == nil {
		return folderbackup.Plan{}, errors.New("This is not available for this game.")
	}
	return l.Backups.PlanRestore(gameID, id, l.folderOf(gameID))
}

// RestoreBackup returns the game's setups folder (the one in use now) to exactly the state of a
// backup (D-81, D-138).
func (l *Library) RestoreBackup(gameID, id string) (folderbackup.Result, error) {
	if l.Backups == nil {
		return folderbackup.Result{}, errors.New("This is not available for this game.")
	}
	if l.Live() == game.LiveDriving {
		return folderbackup.Result{}, errors.New("backups can't be restored while driving; restore from the pits or the menu")
	}
	res, err := func() (folderbackup.Result, error) {
		l.folderMu.Lock()
		defer l.folderMu.Unlock()
		return l.Backups.Restore(gameID, id, l.folderOf(gameID))
	}()
	l.Rescan(gameID)
	return res, err
}

// ---------- helpers ----------

// freeName returns "<car>/<track>/<name><ext>", or "<name> (2)<ext>" etc. when taken (D-77).
func freeName(root, car, trackFolder, name, ext string) string {
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d)", name, i)
		}
		rel := car + "/" + trackFolder + "/" + n + ext
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
			return rel
		}
	}
}

func isInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// writeFileAtomic writes via a temporary file, so the game never sees a half-written setup.
func writeFileAtomic(target string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(target), ".setupman-"+filepath.Base(target))
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// renameFile renames a setup's file in the game folder (same car/track folder) and updates the
// library record; the record is put back if the rename fails. Used by RenameSetup (D-89).
func (l *Library) renameFile(s *store.Setup, newName string) error {
	l.folderMu.Lock()
	defer l.folderMu.Unlock()
	root, _, err := l.gameFolder(s.Game, s.Car, s.Track)
	if err != nil {
		return err
	}
	from := filepath.Join(root, filepath.FromSlash(s.RelPath))
	ext := path.Ext(s.RelPath)
	// Same car and track folder as now (keeps the folders' exact spelling).
	var rel string
	caseOnly := strings.EqualFold(newName, s.Name)
	if caseOnly {
		// Only upper/lower case changes: Windows sees the same file, so it is not "taken" (D-138).
		rel = path.Join(path.Dir(s.RelPath), newName+ext)
	} else {
		rel = freeName(root, path.Dir(path.Dir(s.RelPath)), path.Base(path.Dir(s.RelPath)), newName, ext)
	}
	to := filepath.Join(root, filepath.FromSlash(rel))
	if !isInside(root, from) || !isInside(root, to) {
		return errors.New("SetupMan only changes files inside the setups folder.")
	}
	prevRel, prevName := s.RelPath, s.Name
	s.RelPath = rel
	s.Name = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if err := l.db.UpdateSetup(s); err != nil {
		return err
	}
	if err := renameOnDisk(from, to, caseOnly); err != nil {
		s.RelPath, s.Name = prevRel, prevName
		l.db.UpdateSetup(s)
		return err
	}
	return nil
}

// renameOnDisk renames a file; a change of upper/lower case only goes via a temporary name,
// because Windows may otherwise keep the old spelling.
func renameOnDisk(from, to string, caseOnly bool) error {
	if !caseOnly {
		return os.Rename(from, to)
	}
	tmp := filepath.Join(filepath.Dir(from), ".setupman-rename-"+filepath.Base(to))
	if err := os.Rename(from, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, to); err != nil {
		_ = os.Rename(tmp, from)
		return err
	}
	return nil
}

// ---------- telemetry laps (D-176) ----------

// telemetryLapsField is the setup value that sets how many laps the game keeps in its telemetry
// files. Only ACC has one; 99 is the most the game allows.
var telemetryLapsField = map[string]string{"acc": "basicSetup.electronics.telemetryLaps"}

const maxTelemetryLaps = "99"

// TeleLapsPlan is one setup in the game folder whose telemetry laps aren't 99 yet.
type TeleLapsPlan struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CarName   string `json:"carName"`
	TrackName string `json:"trackName"`
	Laps      string `json:"laps"` // the value in the file now
}

// HasTelemetryLaps says whether a game's setups have a telemetry laps value.
func HasTelemetryLaps(gameID string) bool { return telemetryLapsField[gameID] != "" }

// TelemetryLapsPreview lists the setups in a game's folder whose file has fewer than 99
// telemetry laps, sorted by car, track and name.
func (l *Library) TelemetryLapsPreview(gameID string) ([]TeleLapsPlan, error) {
	out := []TeleLapsPlan{}
	field := telemetryLapsField[gameID]
	if field == "" || l.db == nil {
		return out, errors.New("this game has no telemetry laps setting")
	}
	all, err := l.db.SetupsForGame(gameID)
	if err != nil {
		return out, err
	}
	g := l.game(gameID)
	for _, s := range all {
		if s.RelPath == "" || !s.DeletedAt.IsZero() {
			continue
		}
		root, _, err := l.gameFolder(gameID, s.Car, s.Track)
		if err != nil {
			continue
		}
		raw, err := lastopened.ReadFile(filepath.Join(root, filepath.FromSlash(s.RelPath)))
		if err != nil {
			continue
		}
		fields, err := g.ParseSetup(raw)
		if err != nil {
			continue
		}
		for _, f := range fields {
			if f.Path == field && f.Value != maxTelemetryLaps {
				p := TeleLapsPlan{ID: s.ID, Name: s.Name, Laps: f.Value}
				p.CarName, p.TrackName = l.readableNames(gameID, s.Car, s.Track)
				out = append(out, p)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.CarName != b.CarName {
			return a.CarName < b.CarName
		}
		if a.TrackName != b.TrackName {
			return a.TrackName < b.TrackName
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

// SetTelemetryLaps sets telemetry laps to 99 in the files of the chosen setups in the game
// folder (D-176). Only on request, only with folder management on, never while driving, and
// after a backup of the folder. Nothing else in the files changes; the file date is kept, and
// the setups get no new version (telemetry laps isn't scored, D-52).
func (l *Library) SetTelemetryLaps(gameID string, ids []int64) ([]ManyResult, error) {
	out := []ManyResult{}
	field := telemetryLapsField[gameID]
	if field == "" || l.db == nil {
		return out, errors.New("this game has no telemetry laps setting")
	}
	if !l.managed(gameID) {
		return out, ErrNotManaged
	}
	if l.Live() == game.LiveDriving {
		return out, errors.New("Telemetry laps can't be changed while driving; change them from the pits or the menu.")
	}
	editor, ok := l.game(gameID).(game.Editor)
	if !ok {
		return out, errors.New("setups of this game can't be changed")
	}
	if len(ids) == 0 {
		return out, nil
	}
	if _, err := l.createBackup(gameID, folderbackup.ReasonTelemetryLaps); err != nil {
		return out, fmt.Errorf("no backup could be made, so nothing was changed: %w", err)
	}
	defer l.Rescan(gameID)
	l.folderMu.Lock()
	defer l.folderMu.Unlock()
	changed := 0
	for _, id := range ids {
		s, err := l.db.Setup(id)
		if err != nil || s == nil || s.Game != gameID || s.RelPath == "" {
			continue
		}
		res := ManyResult{ID: id, Name: s.Name}
		if err := l.setTelemetryLapsLocked(gameID, s, editor, field); err != nil {
			res.Error = err.Error()
		} else {
			changed++
		}
		out = append(out, res)
	}
	if changed > 0 {
		log.Printf("telemetry laps set to %s in %d setups (%s)", maxTelemetryLaps, changed, gameID)
	}
	return out, nil
}

func (l *Library) setTelemetryLapsLocked(gameID string, s *store.Setup, editor game.Editor, field string) error {
	root, _, err := l.gameFolder(gameID, s.Car, s.Track)
	if err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(s.RelPath))
	if !isInside(root, target) {
		return errors.New("SetupMan only changes files inside the setups folder.")
	}
	// A save the game made seconds ago is stored first (D-138); the file itself is then edited,
	// so whatever it holds keeps every other value.
	if _, err := l.secureFile(gameID, s, target); err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	raw, err := lastopened.ReadFile(target)
	if err != nil {
		return err
	}
	content, err := editor.EditSetup(raw, map[string]string{field: maxTelemetryLaps})
	if err != nil {
		return err
	}
	if bytes.Equal(content, raw) {
		return nil
	}
	if err := writeFileAtomic(target, content); err != nil {
		return err
	}
	_ = os.Chtimes(target, info.ModTime(), info.ModTime()) // lists and game-version estimates stay right (D-103)
	l.noteOwnTouch(target)                                 // not opened by the game (D-147)
	return nil
}
