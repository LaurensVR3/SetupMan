package acc

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"setupman/internal/game"
)

// detect decides whether ACC is installed.
//   - libraries: Steam library folders (each contains a "steamapps" folder)
//   - documents: the user's Documents folder ("" when unknown)
//
// ACC counts as installed when Steam has it, or when its setups folder exists
// (setups are still worth managing even if the game was removed).
func detect(libraries []string, documents string) game.Detection {
	var d game.Detection
	for _, lib := range libraries {
		if isFile(filepath.Join(lib, "steamapps", "appmanifest_"+SteamAppID+".acf")) {
			d.Installed = true
			break
		}
	}
	if documents != "" {
		folder := filepath.Join(documents, "Assetto Corsa Competizione", "Setups")
		if isDir(folder) {
			d.Folder = folder
			d.Installed = true
		}
	}
	return d
}

var vdfPath = regexp.MustCompile(`"path"\s+"((?:[^"\\]|\\.)*)"`)

// librariesFromSteam reads <steam>/steamapps/libraryfolders.vdf and returns all library folders,
// always including the Steam folder itself.
func librariesFromSteam(steam string) []string {
	if steam == "" {
		return nil
	}
	libs := []string{filepath.Clean(steam)}
	b, err := os.ReadFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	for _, m := range vdfPath.FindAllStringSubmatch(string(b), -1) {
		p := filepath.Clean(strings.ReplaceAll(m[1], `\\`, `\`))
		if !strings.EqualFold(p, libs[0]) {
			libs = append(libs, p)
		}
	}
	return libs
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
