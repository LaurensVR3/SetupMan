package library

import (
	"errors"
	"setupman/internal/game"
	"strings"

	"setupman/internal/history"
	"setupman/internal/setup"
	"setupman/internal/store"
)

// ErrNoHistory is returned when the library has no history database.
var ErrNoHistory = errors.New("version history is not available")

func (l *Library) parserFor(gameID string) history.Parser {
	if g := l.game(gameID); g != nil {
		return g.ParseSetup
	}
	return func([]byte) ([]setup.Field, error) { return nil, errors.New("unknown game") }
}

func (l *Library) historyConfig(gameID string) history.Config {
	st := l.Settings()
	return history.Config{Threshold: st.HistoryThreshold, NameThreshold: st.NameThreshold,
		EnduranceMinutes: st.GameOrDefault(gameID).EnduranceMinutes, GameRunning: l.gameRunning(gameID), TempRules: st.TempRules}
}

// gameRunning tells whether this game is running right now (from its live data).
func (l *Library) gameRunning(gameID string) bool {
	g := l.game(gameID)
	if g == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.live != game.LiveUnknown && l.liveGame == game.Short(g)
}

func (l *Library) setupGame(id int64) (string, error) {
	if l.db == nil {
		return "", ErrNoHistory
	}
	s, err := l.db.Setup(id)
	if err != nil {
		return "", err
	}
	if s == nil {
		return "", history.ErrNotFound
	}
	return s.Game, nil
}

// SetupPage is the setup page: history plus readable names for the breadcrumbs.
type SetupPage struct {
	history.Detail
	GameName  string `json:"gameName"`
	GameShort string `json:"gameShort"` // e.g. "ACC", "AC EVO"
	Ext       string `json:"ext"`       // setup file type, e.g. ".json"
	ReadOnly  string `json:"readOnly"`  // why SetupMan can only read this game's setups ("" = it may change them)
	CanEdit   bool   `json:"canEdit"`   // SetupMan can change values of this game's setups (D-85)
	CarName   string `json:"carName"`
	TrackName string `json:"trackName"`
	TrackID   string `json:"trackId"`
	Managed   bool   `json:"managed"`       // staging buttons are available (D-73)
	Suggested string `json:"suggestedName"` // tidy name built from the details ("" = none)
	// GuessedVersion is the game version current when the setup was saved (D-103).
	GuessedVersion string `json:"guessedVersion"`
}

// SetupDetail returns the setup page (versions, parent, family tree).
func (l *Library) SetupDetail(id int64) (SetupPage, error) {
	g, err := l.setupGame(id)
	if err != nil {
		return SetupPage{}, err
	}
	d, err := history.SetupDetail(l.db, id, l.parserFor(g))
	if err != nil {
		return SetupPage{}, err
	}
	p := SetupPage{Detail: d, TrackID: strings.ToLower(d.Setup.Track)}
	if gm := l.game(g); gm != nil {
		p.GameName, p.GameShort, p.Ext = gm.Name(), game.Short(gm), game.Ext(gm)
		p.ReadOnly = game.ReadOnlyReason(gm)
		_, editor := gm.(game.Editor)
		p.CanEdit = editor && p.ReadOnly == ""
	}
	p.CarName, p.TrackName = l.readableNames(g, d.Setup.Car, d.Setup.Track)
	p.Managed = l.managed(g)
	p.Suggested = l.SuggestedName(id)
	p.GuessedVersion = l.GuessVersion(id)
	return p, nil
}

// UpdateSetupInfo saves a note, session override or edited name fields.
func (l *Library) UpdateSetupInfo(id int64, in history.Info) (*store.Setup, error) {
	g, err := l.setupGame(id)
	if err != nil {
		return nil, err
	}
	s, err := history.UpdateInfo(l.db, id, in, l.historyConfig(g).EnduranceMinutes)
	if err == nil {
		l.publish(Event{Type: "history-changed"})
	}
	return s, err
}

// Relink sets a setup's parent version by hand (0 = no parent).
func (l *Library) Relink(id, parentVersionID int64) error {
	g, err := l.setupGame(id)
	if err != nil {
		return err
	}
	if err := history.Relink(l.db, id, parentVersionID, l.parserFor(g)); err != nil {
		return err
	}
	l.publish(Event{Type: "history-changed"})
	return nil
}

// VersionFields returns one stored version with its fields.
func (l *Library) VersionFields(versionID int64) (*store.Version, []setup.Field, error) {
	if l.db == nil {
		return nil, nil, ErrNoHistory
	}
	v, err := l.db.Version(versionID)
	if err != nil {
		return nil, nil, err
	}
	if v == nil {
		return nil, nil, history.ErrNotFound
	}
	g, err := l.setupGame(v.SetupID)
	if err != nil {
		return nil, nil, err
	}
	return history.VersionFields(l.db, versionID, l.parserFor(g))
}

// LatestFields returns a setup's name and the values of its newest version, e.g. to compare a
// setup that is not in the game folder with others (D-138).
func (l *Library) LatestFields(setupID int64) (string, *store.Version, []setup.Field, error) {
	if l.db == nil {
		return "", nil, nil, ErrNoHistory
	}
	s, err := l.db.Setup(setupID)
	if err != nil || s == nil {
		return "", nil, nil, history.ErrNotFound
	}
	v, err := l.db.LatestVersion(setupID)
	if err != nil || v == nil {
		return "", nil, nil, history.ErrNotFound
	}
	v, fields, err := l.VersionFields(v.ID)
	return s.Name, v, fields, err
}

// GetSaved returns a small saved value (e.g. fuel inputs), "" when not set.
func (l *Library) GetSaved(key string) (string, error) {
	if l.db == nil {
		return "", nil
	}
	return l.db.GetKV(key)
}

// SetSaved stores a small value.
func (l *Library) SetSaved(key, value string) error {
	if l.db == nil {
		return ErrNoHistory
	}
	return l.db.SetKV(key, value)
}

// Changes returns the newest changelog entries and how many are unseen.
func (l *Library) Changes(limit int) ([]store.Event, int, error) {
	if l.db == nil {
		return []store.Event{}, 0, nil
	}
	ev, err := l.db.Events(false, limit)
	if err != nil {
		return nil, 0, err
	}
	n, err := l.db.CountUnseen()
	return ev, n, err
}

// MarkChangesSeen marks the whole changelog as seen.
func (l *Library) MarkChangesSeen() error {
	if l.db == nil {
		return nil
	}
	err := l.db.MarkAllSeen()
	if err == nil {
		l.publish(Event{Type: "history-changed"})
	}
	return err
}

// HistoryPreview rebuilds the probable history with the given thresholds; apply saves it.
func (l *Library) HistoryPreview(gameID string, cfg history.Config, apply bool) (history.Preview, error) {
	if l.db == nil {
		return history.Preview{}, ErrNoHistory
	}
	if l.game(gameID) == nil {
		return history.Preview{}, history.ErrNotFound
	}
	cfg.EnduranceMinutes = l.historyConfig(gameID).EnduranceMinutes
	p, err := history.Rebuild(l.db, gameID, l.parserFor(gameID), cfg, apply)
	if err != nil {
		return p, err
	}
	if apply {
		l.publish(Event{Type: "history-changed"})
	}
	for i := range p.Groups {
		p.Groups[i].CarName, p.Groups[i].TrackName = l.readableNames(gameID, p.Groups[i].Car, p.Groups[i].Track)
	}
	return p, nil
}

// readableNames looks up car and track names in the latest scan (falls back to the ids).
func (l *Library) readableNames(gameID, car, track string) (string, string) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	carName, trackName := car, track
	if s := l.states[gameID]; s != nil && s.scan != nil {
		for _, c := range s.scan.Cars {
			if strings.EqualFold(c.ID, car) {
				carName = c.Name
				for _, t := range c.Tracks {
					if t.ID == strings.ToLower(track) {
						trackName = t.Name
					}
				}
			}
		}
	}
	return carName, trackName
}

// TrackFamilies returns every family tree of one car on one track (track page, Families tab).
func (l *Library) TrackFamilies(gameID, car, track string) ([]*history.Node, error) {
	if l.db == nil {
		return []*history.Node{}, nil
	}
	return history.TrackFamilies(l.db, gameID, car, track)
}
