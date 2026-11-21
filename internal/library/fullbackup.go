package library

import (
	"setupman/internal/fullbackup"
)

// CreateFullBackup makes a full SetupMan backup: the library, settings and a fresh ACC setups
// folder backup of every game that has a folder (spec D-82, D-92).
func (l *Library) CreateFullBackup(fb *fullbackup.Store, reason string) (fullbackup.Summary, error) {
	if l.db == nil {
		return fullbackup.Summary{}, ErrNoHistory
	}
	folders := map[fullbackup.FolderRef]string{}
	if l.Backups != nil {
		sums, err := l.BackupAllFolders()
		if err != nil {
			return fullbackup.Summary{}, err
		}
		for _, s := range sums {
			folders[fullbackup.FolderRef{Game: s.Game, ID: s.ID}] = l.Backups.Path(s.Game, s.ID)
		}
	}
	return fb.Create(reason, l.db, folders)
}
