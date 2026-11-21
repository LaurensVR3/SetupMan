package library

import "fmt"

// FolderCheck is the plain-language result of checking a folder in Settings.
type FolderCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// CheckFolder scans a folder for a game without storing anything (read-only).
func (l *Library) CheckFolder(id, folder string) FolderCheck {
	g := l.game(id)
	if g == nil {
		return FolderCheck{Message: "Unknown game."}
	}
	if folder == "" {
		return FolderCheck{Message: "No folder chosen."}
	}
	res, err := g.Scan(folder)
	if err != nil {
		return FolderCheck{Message: "This folder can't be opened: " + err.Error()}
	}
	setups, tracks := 0, 0
	for i := range res.Cars {
		tracks += len(res.Cars[i].Tracks)
		for j := range res.Cars[i].Tracks {
			setups += len(res.Cars[i].Tracks[j].Setups)
		}
	}
	if setups == 0 {
		return FolderCheck{Message: fmt.Sprintf("This doesn't look like a %s setups folder. %s", g.Name(), res.Hint)}
	}
	return FolderCheck{OK: true, Message: fmt.Sprintf("%d setups found for %d cars on %d tracks.", setups, len(res.Cars), tracks)}
}
