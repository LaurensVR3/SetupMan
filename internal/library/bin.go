package library

// The bin (spec D-102): deleting a setup takes it out of ACC (when SetupMan chooses what ACC
// shows) and out of SetupMan's lists, but keeps it with all its versions for 30 days.

import (
	"errors"
	"log"
	"time"

	"setupman/internal/game"
	"setupman/internal/history"
	"setupman/internal/store"
)

// BinDays is how long deleted setups are kept.
const BinDays = 30

// BinItem is one setup in the bin.
type BinItem struct {
	ID        int64     `json:"id"`
	Game      string    `json:"game"`
	Car       string    `json:"car"`
	Track     string    `json:"track"`
	CarName   string    `json:"carName"`
	TrackName string    `json:"trackName"`
	Name      string    `json:"name"`
	DeletedAt time.Time `json:"deletedAt"`
	GoneAt    time.Time `json:"goneAt"` // removed for good after this
}

// DeleteResult says what happened per setup.
type DeleteResult struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

// DeleteSetups moves setups into the bin. A setup that is in ACC's folder is first hidden from
// ACC, which needs SetupMan to choose what ACC shows (D-73) and the driver not to be driving.
func (l *Library) DeleteSetups(ids []int64) ([]DeleteResult, error) {
	if l.db == nil {
		return nil, ErrNoHistory
	}
	if l.Live() == game.LiveDriving {
		return nil, errors.New("setups can't be deleted while driving; delete them from the pits or the menu")
	}
	// Files in game folders are taken out first, all at once per game: one folder lock and one
	// rescan for the whole batch, instead of one rescan per file (slow with a large library).
	setups := map[int64]*store.Setup{}
	inFolder := map[string][]int64{}
	for _, id := range ids {
		s, err := l.db.Setup(id)
		if err != nil || s == nil || !s.DeletedAt.IsZero() {
			continue
		}
		setups[id] = s
		if s.RelPath != "" && l.managed(s.Game) {
			inFolder[s.Game] = append(inFolder[s.Game], id)
		}
	}
	unstageErr := map[int64]error{}
	for gameID, gids := range inFolder {
		for id, err := range l.unstageMany(gameID, gids) {
			unstageErr[id] = err
		}
	}
	out := []DeleteResult{}
	for _, id := range ids {
		s := setups[id]
		if s == nil {
			continue
		}
		res := DeleteResult{ID: id, Name: s.Name}
		if s.RelPath != "" {
			if !l.managed(s.Game) {
				res.Error = "it is in the game's setups folder; removing files there requires folder management (Settings → Games)"
				out = append(out, res)
				continue
			}
			if err := unstageErr[id]; err != nil {
				res.Error = "could not take it out of the game's setups folder: " + err.Error()
				out = append(out, res)
				continue
			}
		}
		if err := l.db.SetDeleted(id, time.Now()); err != nil {
			return out, err
		}
		l.db.InsertEvent(store.Event{Game: s.Game, Kind: "deleted", SetupID: id, Details: map[string]any{"name": s.Name}})
		out = append(out, res)
	}
	l.publish(Event{Type: "history-changed"})
	return out, nil
}

// RestoreDeleted takes a setup out of the bin. It comes back hidden from ACC.
func (l *Library) RestoreDeleted(id int64) (string, error) {
	if l.db == nil {
		return "", ErrNoHistory
	}
	s, err := l.db.Setup(id)
	if err != nil || s == nil || s.DeletedAt.IsZero() {
		return "", history.ErrNotFound
	}
	if err := l.db.SetDeleted(id, time.Time{}); err != nil {
		return "", err
	}
	l.db.InsertEvent(store.Event{Game: s.Game, Kind: "undeleted", SetupID: id, Details: map[string]any{"name": s.Name}})
	l.publish(Event{Type: "history-changed"})
	return s.Name, nil
}

// Bin lists the deleted setups.
func (l *Library) Bin() ([]BinItem, error) {
	out := []BinItem{}
	if l.db == nil {
		return out, nil
	}
	list, err := l.db.DeletedSetups()
	if err != nil {
		return out, err
	}
	for _, s := range list {
		it := BinItem{ID: s.ID, Game: s.Game, Car: s.Car, Track: s.Track, Name: s.Name, DeletedAt: s.DeletedAt,
			GoneAt: s.DeletedAt.AddDate(0, 0, BinDays)}
		it.CarName, it.TrackName = l.readableNames(s.Game, s.Car, s.Track)
		out = append(out, it)
	}
	return out, nil
}

// purgeBin removes setups that have been in the bin for more than BinDays.
func (l *Library) purgeBin() {
	if l.db == nil {
		return
	}
	if n, err := l.db.PurgeDeleted(time.Now().AddDate(0, 0, -BinDays)); err != nil {
		log.Printf("emptying the bin: %v", err)
	} else if n > 0 {
		log.Printf("removed %d setups that were in the bin for more than %d days", n, BinDays)
	}
}
