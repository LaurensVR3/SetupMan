package library

// Renaming setups (original feature list: opt-in clever auto-renaming; spec D-12, D-49).
// A setup in the game folder can only be renamed when SetupMan manages that folder (D-73).

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/lastopened"
	"setupman/internal/naming"
	"setupman/internal/store"
)

// SuggestedName builds a tidy name for a setup from its details ("" = nothing to suggest).
func (l *Library) SuggestedName(setupID int64) string {
	if l.db == nil {
		return ""
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return ""
	}
	wet := false
	if v, _ := l.db.LatestVersion(s.ID); v != nil {
		if wc, ok := l.game(s.Game).(game.WetChecker); ok {
			if fields, err := l.game(s.Game).ParseSetup(v.Content); err == nil {
				wet = wc.IsWet(fields)
			}
		}
	}
	m := s.Meta
	if m.GameVersion == "" { // the version that was current when the setup was saved (D-103)
		m.GameVersion = l.GuessVersion(s.ID)
	}
	return l.suggest(s, m, s.Session, wet)
}

func (l *Library) suggest(s *store.Setup, m naming.Meta, session string, wet bool) string {
	name := naming.Suggest(m, session, wet, s.Name)
	if name == "" || strings.EqualFold(name, s.Name) {
		return ""
	}
	// "X (2)" whose tidy name is X while another file is called X: renaming would give "X (2)"
	// again, so the setup would be offered for tidying forever (rig 2026-11-24)
	if strings.EqualFold(copyBase(s.Name), name) && s.RelPath != "" {
		if root := l.folderOf(s.Game); root != "" {
			dir, ext := path.Dir(s.RelPath), path.Ext(s.RelPath)
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir+"/"+name+ext))); err == nil {
				return ""
			}
		}
	}
	return name
}

// copyBase is a name without a trailing copy number: "X (2)" → "X".
func copyBase(name string) string {
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		if _, err := strconv.Atoi(name[i+2 : len(name)-1]); err == nil {
			return name[:i]
		}
	}
	return name
}

// newSaveName is the name for a setup just saved in the game (D-144, rig test R-7): what its own
// name says, completed with the driver's initials (Settings → General), the session and the
// temperature of the session it was saved in, and otherwise what the setup it was made from says.
// The temperature is written as air and track as the game shows them, rounded: air 30.7 °C and
// track 39.1 °C give "3139" (D-145).
func (l *Library) newSaveName(s *store.Setup) (string, string) {
	m := s.Meta
	if m.GameVersion == "" {
		m.GameVersion = l.GuessVersion(s.ID)
	}
	if m.Initials == "" {
		m.Initials = l.Settings().Initials
	}
	var parent *store.Setup
	if s.ParentVersionID != 0 {
		if pv, err := l.db.Version(s.ParentVersionID); err == nil && pv != nil {
			parent, _ = l.db.Setup(pv.SetupID)
		}
	}
	session, from := s.Session, ""
	t, live := l.liveSession(s.Game)
	if session == "" && live {
		switch t.Session {
		case "qualifying", "superpole":
			session, from = naming.SessionQ, "session"
		case "race":
			session, from = naming.SessionR, "session"
		}
	}
	if session == "" && parent != nil && parent.Session != "" {
		session, from = parent.Session, "parent"
	}
	if !m.HasTemp() {
		switch {
		case live && t.TempsKnown:
			m.Air, m.Track = int(math.Round(t.AirTemp)), int(math.Round(t.RoadTemp))
		case parent != nil && parent.Meta.HasTemp():
			pm := parent.Meta
			m.Air, m.Track, m.TempMin, m.TempMax = pm.Air, pm.Track, pm.TempMin, pm.TempMax
		}
	}
	wet := false
	if v, _ := l.db.LatestVersion(s.ID); v != nil {
		if wc, ok := l.game(s.Game).(game.WetChecker); ok {
			if fields, err := l.game(s.Game).ParseSetup(v.Content); err == nil {
				wet = wc.IsWet(fields)
			}
		}
	}
	why := fmt.Sprintf("initials %q, session %q (%s), air %d track %d (range %d–%d), version %q", m.Initials, session, orDash(from), m.Air, m.Track, m.TempMin, m.TempMax, m.GameVersion)
	return l.suggest(s, m, session, wet), why
}

func orDash(s string) string {
	if s == "" {
		return "name"
	}
	return s
}

// liveSession is the game's live data while a session runs (false otherwise). In the game's menus
// during a session (ACC's setup screen reports no live session; rig log 2026-11-21) the session
// being recorded gives its kind and latest temperatures.
func (l *Library) liveSession(gameID string) (game.Telemetry, bool) {
	src, ok := l.game(gameID).(game.TelemetrySource)
	if !ok {
		return game.Telemetry{}, false
	}
	t, running := src.Telemetry()
	if !running {
		return game.Telemetry{}, false
	}
	if t.Live {
		return t, true
	}
	l.recMu.Lock()
	defer l.recMu.Unlock()
	if r := l.recs[gameID]; r != nil && r.sessionID != 0 {
		return game.Telemetry{Session: r.kind, AirTemp: r.lastAir, RoadTemp: r.lastRoad, TempsKnown: r.hasTemps}, true
	}
	return t, false
}

// RenameSetup gives a setup a new name; a file in the game folder is renamed too.
func (l *Library) RenameSetup(setupID int64, newName string) (string, error) {
	got, game, err := l.renameSetup(setupID, newName)
	if err == nil && game != "" {
		l.Rescan(game)
	}
	return got, err
}

// renameSetup renames without rescanning; it returns the game to rescan when a file was renamed.
// Bulk renames rescan once at the end (one rescan per file is slow on a large library).
func (l *Library) renameSetup(setupID int64, newName string) (string, string, error) {
	rescan := ""
	if l.db == nil {
		return "", "", ErrNoHistory
	}
	newName = strings.TrimSpace(newName)
	if !validName(newName) {
		return "", "", errBadName
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return "", "", history.ErrNotFound
	}
	if s.Name == newName {
		return newName, "", nil
	}
	old := s.Name
	if s.RelPath == "" { // library only: just the name
		s.Name = newName
		if err := l.db.UpdateSetup(s); err != nil {
			return "", "", err
		}
	} else {
		if g := l.game(s.Game); g != nil && game.ReadOnlyReason(g) != "" {
			return "", "", errors.New(game.ReadOnlyReason(g))
		}
		if !l.managed(s.Game) {
			return "", "", errors.New("renaming a setup in the game's setups folder requires folder management (Settings → Games)")
		}
		if l.Live() == game.LiveDriving {
			return "", "", errors.New("setups can't be renamed while driving; rename them from the pits or the menu")
		}
		if err := l.renameFile(s, newName); err != nil {
			return "", "", err
		}
		rescan = s.Game
	}
	l.db.InsertEvent(store.Event{Game: s.Game, Kind: "renamed-by-you", SetupID: s.ID,
		Details: map[string]any{"from": old, "to": s.Name}})
	l.publish(Event{Type: "history-changed"})
	return s.Name, rescan, nil
}

// autoRename gives new in-game saves a tidy name, when the user switched this on (opt-in).
func (l *Library) autoRename(gameID string) {
	l.mu.Lock()
	st := l.states[gameID]
	ids := st.newSetups
	st.newSetups = nil // always emptied, so the list never grows
	l.mu.Unlock()
	gs := l.Settings().GameOrDefault(gameID)
	if l.db == nil || !l.managed(gameID) || !gs.AutoRename {
		return
	}
	merged := false
	for _, id := range ids {
		s, err := l.db.Setup(id)
		if err != nil || s == nil || s.RelPath == "" {
			continue
		}
		if l.mergeGameSave(gameID, s) {
			merged = true
			continue
		}
		name, why := l.newSaveName(s)
		if name == "" {
			log.Printf("auto-rename %s: name already follows the naming standard (%s)", s.Name, why)
			continue
		}
		old := s.Name
		if got, err := l.RenameSetup(s.ID, name); err != nil {
			log.Printf("auto-rename %s: %v", old, err)
		} else {
			log.Printf("auto-rename %s → %s (%s)", old, got, why)
			// Remember the name the game knows, so its next save under that name becomes a new
			// version of this setup instead of a separate one (D-138).
			_ = l.db.SetKV(autoRenameKey(s.Game, s.Car, s.Track, old), strconv.FormatInt(s.ID, 10))
			if ns, _ := l.db.Setup(s.ID); ns != nil && !strings.Contains(ns.Note, old) {
				ns.Note = strings.TrimSpace(ns.Note + "\n" + fmt.Sprintf("Saved in the game as \"%s\"; renamed by SetupMan to \"%s\".", old, got))
				l.db.UpdateSetup(ns)
			}
		}
	}
	if merged {
		l.Rescan(gameID)
		l.publish(Event{Type: "history-changed"})
	}
}

func autoRenameKey(gameID, car, track, name string) string {
	return "autorename/" + gameID + "/" + strings.ToLower(car) + "/" + strings.ToLower(track) + "/" + strings.ToLower(name)
}

// mergeGameSave handles the game saving again under the name it knows (e.g. "quali") after
// SetupMan gave an earlier save of that name a tidy name. The save is a new version of the renamed
// setup: its values go into the renamed file and the "quali" file is removed, so the folder doesn't
// fill up with "quali", "ABC Q 2327 (2)", … (D-138). It returns false when this doesn't apply or
// fails; the save is then treated as usual (nothing is lost either way).
func (l *Library) mergeGameSave(gameID string, s *store.Setup) bool {
	saved, _ := l.db.GetKV(autoRenameKey(gameID, s.Car, s.Track, s.Name))
	targetID, _ := strconv.ParseInt(saved, 10, 64)
	if targetID == 0 || targetID == s.ID {
		return false
	}
	t, err := l.db.Setup(targetID)
	if err != nil || t == nil || t.RelPath == "" || !t.DeletedAt.IsZero() ||
		!strings.EqualFold(t.Car, s.Car) || !strings.EqualFold(t.Track, s.Track) || l.Live() == game.LiveDriving {
		return false
	}
	g := l.game(gameID)
	err = func() error {
		l.folderMu.Lock()
		defer l.folderMu.Unlock()
		root, _, err := l.gameFolder(gameID, t.Car, t.Track)
		if err != nil {
			return err
		}
		from := filepath.Join(root, filepath.FromSlash(s.RelPath))
		to := filepath.Join(root, filepath.FromSlash(t.RelPath))
		if !isInside(root, from) || !isInside(root, to) {
			return errors.New("SetupMan only changes files inside the setups folder.")
		}
		raw, err := lastopened.ReadFile(from)
		if err != nil {
			return err
		}
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		fields, err := g.ParseSetup(raw)
		if err != nil {
			return err
		}
		if _, err := l.secureFile(gameID, t, to); err != nil { // anything unread in the renamed file is kept
			return err
		}
		latest, _ := l.db.LatestVersion(t.ID)
		v := latest
		changes := ""
		if latest == nil || latest.Fingerprint != compare.Fingerprint(fields) || !bytes.Equal(latest.Content, raw) {
			if latest != nil {
				if old, err := g.ParseSetup(latest.Content); err == nil {
					changes = history.Summary(compare.Compare(old, fields), 5)
				}
			}
			v = &store.Version{SetupID: t.ID, Fingerprint: compare.Fingerprint(fields), Content: raw, FileModified: info.ModTime(),
				Source: "change", Note: fmt.Sprintf("Saved in the game as \"%s\"", s.Name)}
			if err := l.db.InsertVersion(v); err != nil {
				return err
			}
		}
		if err := writeFileAtomic(to, raw); err != nil {
			return err
		}
		_ = os.Chtimes(to, info.ModTime(), info.ModTime())
		t.DiskVersionID = v.ID
		if err := l.db.UpdateSetup(t); err != nil {
			return err
		}
		if err := os.Remove(from); err != nil {
			return err // both files stay; the save keeps its own record
		}
		if err := l.db.RemoveSetupRecord(s.ID); err != nil {
			return err
		}
		return l.db.InsertEvent(store.Event{Game: gameID, Kind: "new-version", SetupID: t.ID, VersionID: v.ID,
			Details: map[string]any{"name": t.Name, "seq": v.Seq, "changes": changes}})
	}()
	if err != nil {
		log.Printf("saving %s into %s: %v", s.Name, t.Name, err)
		return false
	}
	return true
}

// TidyResult is what happened to one setup in a bulk rename.
type TidyResult struct {
	ID    int64  `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Error string `json:"error,omitempty"`
}

// TidyPlan is one proposed rename for the whole-game "apply naming standard" preview.
type TidyPlan struct {
	ID        int64  `json:"id"`
	CarName   string `json:"carName"`
	TrackName string `json:"trackName"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// TidyPreview lists every setup of a game whose name differs from the naming standard, so the
// user sees all new names before anything is renamed.
func (l *Library) TidyPreview(gameID string) ([]TidyPlan, error) {
	out := []TidyPlan{}
	if l.db == nil {
		return out, ErrNoHistory
	}
	all, err := l.db.SetupsForGame(gameID)
	if err != nil {
		return out, err
	}
	for _, s := range all {
		if s.RelPath == "" || !s.DeletedAt.IsZero() {
			continue // only setups in the game folder
		}
		if to := l.SuggestedName(s.ID); to != "" {
			p := TidyPlan{ID: s.ID, From: s.Name, To: to}
			p.CarName, p.TrackName = l.readableNames(gameID, s.Car, s.Track)
			out = append(out, p)
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
		return strings.ToLower(a.From) < strings.ToLower(b.From)
	})
	return out, nil
}

// RenameTidy gives several setups their suggested tidy name (setups already tidy are skipped).
// The game folder is rescanned once at the end.
func (l *Library) RenameTidy(ids []int64) []TidyResult {
	out := []TidyResult{}
	rescan := map[string]bool{}
	defer func() {
		for g := range rescan {
			l.Rescan(g)
		}
	}()
	for _, id := range ids {
		s, err := l.db.Setup(id)
		if err != nil || s == nil {
			continue
		}
		name := l.SuggestedName(id)
		if name == "" {
			continue
		}
		res := TidyResult{ID: id, From: s.Name}
		if got, g, err := l.renameSetup(id, name); err != nil {
			res.Error = err.Error()
		} else {
			res.To = got
			if g != "" {
				rescan[g] = true
			}
		}
		out = append(out, res)
	}
	return out
}

// Patches returns a game's release list (D-103). It takes no lock, so views may call it.
func (l *Library) Patches(gameID string) []game.Patch {
	if pl, ok := l.game(gameID).(game.PatchLister); ok {
		return pl.Patches()
	}
	return []game.Patch{}
}

// GuessVersion is the game version that was current when a setup was first saved (its oldest
// version's file date), or "" when unknown (D-103).
func (l *Library) GuessVersion(setupID int64) string {
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return ""
	}
	vs, err := l.db.Versions(setupID)
	if err != nil || len(vs) == 0 {
		return ""
	}
	t := vs[0].FileModified
	if t.IsZero() {
		t = vs[0].ObservedAt
	}
	return game.VersionAt(l.Patches(s.Game), t)
}

// versionFor is the game version of a setup for lists: the version in its name, else the
// release current on the date it was last saved (guessed = true). "" when unknown (D-118).
func (l *Library) versionFor(gameID, fromName string, saved time.Time) (string, bool) {
	if fromName != "" {
		return fromName, false
	}
	if saved.IsZero() {
		return "", false
	}
	v := game.VersionAt(l.Patches(gameID), saved)
	return v, v != ""
}
