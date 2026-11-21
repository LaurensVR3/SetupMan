package library

// Changing setups in SetupMan (spec D-85). An edit is always saved in the library first
// (as a new version, or as a new setup made from this one). If SetupMan manages the game's
// folder, the changed setup is also put in the game folder.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"setupman/internal/compare"
	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/naming"
	"setupman/internal/store"
)

// EditResult tells the UI what happened.
type EditResult struct {
	SetupID      int64  `json:"setupId"` // the edited setup, or the new one
	VersionID    int64  `json:"versionId"`
	Changes      string `json:"changes"`
	InGameFolder bool   `json:"inGameFolder"`
	Queued       bool   `json:"queued"`
	GameRunning  bool   `json:"gameRunning"`
	Message      string `json:"message"`
}

// EditSetup applies changes to a setup's newest version. With newName set, the result
// becomes a new setup (made from this one) instead of a new version.
func (l *Library) EditSetup(setupID int64, edits map[string]string, newName string) (EditResult, error) {
	if l.db == nil {
		return EditResult{}, ErrNoHistory
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return EditResult{}, history.ErrNotFound
	}
	g := l.game(s.Game)
	editor, ok := g.(game.Editor)
	if !ok {
		return EditResult{}, errors.New("setups of this game can't be changed in SetupMan yet")
	}
	if len(edits) == 0 && newName == "" {
		return EditResult{}, errors.New("nothing was changed")
	}
	newName = strings.TrimSpace(newName)
	if newName != "" && !validName(newName) {
		return EditResult{}, errBadName
	}

	latest, err := l.db.LatestVersion(s.ID)
	if err != nil || latest == nil {
		return EditResult{}, errors.New("this setup has no stored version")
	}
	content, err := editor.EditSetup(latest.Content, edits)
	if err != nil {
		return EditResult{}, err
	}
	fields, err := g.ParseSetup(content)
	if err != nil {
		return EditResult{}, err
	}
	oldFields, _ := g.ParseSetup(latest.Content)
	changes := history.Summary(compare.Compare(oldFields, fields), 8)
	if changes == "" {
		changes = describeEdits(edits)
	}
	fp := compare.Fingerprint(fields)
	res := EditResult{Changes: changes}

	if newName == "" {
		if fp == latest.Fingerprint && string(content) == string(latest.Content) {
			return EditResult{}, errors.New("these are the values the setup already has")
		}
		v := &store.Version{SetupID: s.ID, Fingerprint: fp, Content: content, Source: "edited",
			Note: "Changed in SetupMan: " + changes}
		if err := l.db.InsertVersion(v); err != nil {
			return res, err
		}
		res.SetupID, res.VersionID = s.ID, v.ID
		l.db.InsertEvent(store.Event{Game: s.Game, Kind: "edited", SetupID: s.ID, VersionID: v.ID,
			Details: map[string]any{"name": s.Name, "seq": v.Seq, "changes": changes}})
		res.InGameFolder = s.RelPath != ""
	} else {
		m := naming.ParseWith(newName, l.Settings().TempRules)
		ns := &store.Setup{Game: s.Game, Car: s.Car, Track: s.Track, Name: newName, Meta: m,
			Session:         naming.GuessSession(m, l.historyConfig(s.Game).EnduranceMinutes),
			ParentVersionID: latest.ID, LinkKind: store.LinkManual, LinkScore: compare.Compare(oldFields, fields).Score}
		if err := l.db.InsertSetup(ns); err != nil {
			return res, err
		}
		v := &store.Version{SetupID: ns.ID, Fingerprint: fp, Content: content, Source: "edited",
			Note: fmt.Sprintf("Made in SetupMan from %s: %s", s.Name, changes)}
		if err := l.db.InsertVersion(v); err != nil {
			return res, err
		}
		res.SetupID, res.VersionID = ns.ID, v.ID
		l.db.InsertEvent(store.Event{Game: s.Game, Kind: "edited-new", SetupID: ns.ID, VersionID: v.ID,
			Details: map[string]any{"name": newName, "from": s.Name, "changes": changes}})
		res.InGameFolder = true // a new setup is meant to be used: put it in the game folder if allowed
	}

	// Put it in the game folder when SetupMan manages that folder (D-73).
	if res.InGameFolder && l.managed(s.Game) {
		sr, err := l.Stage(res.SetupID, 0)
		if err != nil {
			return res, err
		}
		res.Queued, res.GameRunning = sr.Queued, sr.GameRunning
		res.Message = "Saved. " + game.Short(g) + " has the new values."
		if sr.Queued {
			res.Message = "Saved. " + game.Short(g) + " gets the new values when you stop driving."
		}
	} else {
		res.InGameFolder = false
		res.Message = "Saved in SetupMan."
		if !l.managed(s.Game) {
			res.Message += " To use it in " + game.Short(g) + ", enable folder management (Settings → Games)."
		}
	}
	l.publish(Event{Type: "history-changed"})
	return res, nil
}

func describeEdits(edits map[string]string) string {
	keys := make([]string, 0, len(edits))
	for k := range edits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+" = "+edits[k])
	}
	return strings.Join(parts, ", ")
}
