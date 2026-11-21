package library

// Sharing a setup as one file (spec D-94): the game's setup file (exact bytes) plus part of
// SetupMan's metadata. Nothing leaves the PC unless the user sends the file. Another SetupMan
// imports it into its library (never straight into the game folder, like D-91).

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/history"
	"setupman/internal/naming"
	"setupman/internal/store"
)

// ShareExt is the file extension of a shared setup.
const ShareExt = ".setupman"

const shareKind = "setupman-shared-setup"

// Shared is the content of a .setupman file.
type Shared struct {
	Kind     string      `json:"kind"`
	Version  int         `json:"version"`
	SharedAt time.Time   `json:"sharedAt"`
	Game     string      `json:"game"`
	Car      string      `json:"car"`
	Track    string      `json:"track"`
	Name     string      `json:"name"`
	Session  string      `json:"session,omitempty"`
	Meta     naming.Meta `json:"meta"`
	Note     string      `json:"note,omitempty"`
	Laps     *LapStats   `json:"laps,omitempty"` // the sender's lap times on this version (optional)
	Setup    []byte      `json:"setup"`          // the game's setup file, exact bytes (base64)
}

// Share builds a .setupman file for a setup's newest version. Returns the file and a file name.
func (l *Library) Share(setupID int64, withNote, withLaps bool) ([]byte, string, error) {
	if l.db == nil {
		return nil, "", ErrNoHistory
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return nil, "", history.ErrNotFound
	}
	v, err := l.db.LatestVersion(s.ID)
	if err != nil || v == nil {
		return nil, "", errors.New("this setup has no stored version")
	}
	sh := Shared{Kind: shareKind, Version: 1, SharedAt: time.Now(), Game: s.Game, Car: s.Car, Track: s.Track,
		Name: s.Name, Session: s.Session, Meta: s.Meta, Setup: v.Content}
	if withNote {
		sh.Note = s.Note
	}
	if withLaps {
		if lt, err := l.SetupLapTimes(s.ID); err == nil {
			for _, vl := range lt.Versions {
				if vl.VersionID == v.ID && vl.Laps > 0 {
					st := vl.LapStats
					sh.Laps = &st
				}
			}
		}
	}
	data, err := json.MarshalIndent(sh, "", "  ")
	return data, s.Name + ShareExt, err
}

// ImportShared adds a shared setup to the library, in the car and track named in the file.
// When onlyCar/onlyTrack are set (import on a track page), a file for elsewhere is skipped.
func (l *Library) ImportShared(data []byte, onlyCar, onlyTrack string) (ImportItem, error) {
	var sh Shared
	if err := json.Unmarshal(data, &sh); err != nil || sh.Kind != shareKind {
		return ImportItem{}, errors.New("this is not a shared SetupMan setup")
	}
	if sh.Version > 1 {
		return ImportItem{}, errors.New("this setup was shared by a newer SetupMan; update SetupMan first")
	}
	item := ImportItem{Name: sh.Name, Result: "skipped"}
	g := l.game(sh.Game)
	if g == nil || l.db == nil {
		item.Reason = "it is for a game SetupMan doesn't support here (" + sh.Game + ")"
		return item, nil
	}
	if sh.Car == "" || sh.Track == "" || strings.ContainsAny(sh.Car+sh.Track, `\/:*?"<>|`) {
		item.Reason = "the file doesn't say which car and track it is for"
		return item, nil
	}
	if onlyCar != "" && (!strings.EqualFold(sh.Car, onlyCar) || !strings.EqualFold(sh.Track, onlyTrack)) {
		item.Reason = fmt.Sprintf("it is for another car or track (%s, %s)", sh.Car, sh.Track)
		return item, nil
	}
	fields, err := g.ParseSetup(sh.Setup)
	if err != nil {
		item.Reason = "the setup inside is damaged"
		return item, nil
	}
	car, track := strings.ToLower(sh.Car), strings.ToLower(sh.Track)
	fp := compare.Fingerprint(fields)
	existing, err := l.db.SetupsForTrack(sh.Game, car, track)
	if err != nil {
		return item, err
	}
	names := map[string]bool{}
	for _, s := range existing {
		names[strings.ToLower(s.Name)] = true
		if v, _ := l.db.LatestVersion(s.ID); v != nil && v.Fingerprint == fp {
			item.Reason = "already in your library as " + s.Name
			item.SetupID = s.ID
			return item, nil
		}
	}
	name := cleanName(sh.Name, "Shared setup") // becomes a file name in the game folder later (D-138)
	final := name
	for i := 2; names[strings.ToLower(final)]; i++ {
		final = fmt.Sprintf("%s (%d)", name, i)
	}
	note := fmt.Sprintf("Shared with you on %s.", sh.SharedAt.Format("2 Jan 2006"))
	if sh.Laps != nil && sh.Laps.Laps > 0 {
		note += " Their lap times: " + lapSummary(*sh.Laps) + "."
	}
	if sh.Note != "" {
		note += "\nTheir notes: " + sh.Note
	}
	session := sh.Session
	if session != naming.SessionQ && session != naming.SessionR && session != naming.SessionEndu {
		session = ""
	}
	s := &store.Setup{Game: sh.Game, Car: car, Track: track, Name: final, Meta: sh.Meta, MetaManual: true,
		Session: session, SessionManual: session != "", Note: note}
	if err := l.db.InsertSetup(s); err != nil {
		return item, err
	}
	v := &store.Version{SetupID: s.ID, Fingerprint: fp, Content: sh.Setup, Source: "imported", Note: "Shared setup"}
	if err := l.db.InsertVersion(v); err != nil {
		return item, err
	}
	l.db.InsertEvent(store.Event{Game: sh.Game, Kind: "imported-file", SetupID: s.ID, VersionID: v.ID,
		Details: map[string]any{"name": final}})
	l.publish(Event{Type: "history-changed"})
	return ImportItem{Name: final, SetupID: s.ID, Result: "imported"}, nil
}

func lapSummary(st LapStats) string {
	parts := []string{fmt.Sprintf("%d laps", st.Laps)}
	if st.BestMs > 0 {
		parts = append(parts, "best "+msText(st.BestMs))
	}
	if st.AvgMs > 0 {
		parts = append(parts, "average "+msText(st.AvgMs))
	}
	if st.FuelLaps > 0 {
		parts = append(parts, fmt.Sprintf("%.2f L per lap", st.FuelPerLap))
	}
	return strings.Join(parts, ", ")
}

func msText(ms int) string {
	return fmt.Sprintf("%d:%06.3f", ms/60000, float64(ms%60000)/1000)
}
