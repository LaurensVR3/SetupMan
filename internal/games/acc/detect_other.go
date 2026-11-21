//go:build !windows

package acc

import (
	"os"
	"path/filepath"
)

// ACC only runs on Windows; these keep the project building elsewhere.
func steamLibraries() []string { return nil }

func documentsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents")
}
