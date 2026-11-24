// Package gamekit holds what several games share: finding their folders, checking whether the
// game runs, and scanning a <car>/<track>/<setup file> folder. ACC has its own copies of these
// (it came first); new games use this package.
package gamekit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var vdfPath = regexp.MustCompile(`"path"\s+"((?:[^"\\]|\\.)*)"`)
var acfInstallDir = regexp.MustCompile(`"installdir"\s+"((?:[^"\\]|\\.)*)"`)

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

// SteamGame finds a Steam game by its app id: whether Steam has it, and its install folder
// ("" when unknown).
func SteamGame(appID string) (installed bool, dir string) {
	for _, lib := range SteamLibraries() {
		b, err := os.ReadFile(filepath.Join(lib, "steamapps", "appmanifest_"+appID+".acf"))
		if err != nil {
			continue
		}
		if m := acfInstallDir.FindStringSubmatch(string(b)); m != nil {
			d := filepath.Join(lib, "steamapps", "common", m[1])
			if IsDir(d) {
				return true, d
			}
		}
		return true, ""
	}
	return false, ""
}

// IsDir reports whether p is an existing folder.
func IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Words turns an id such as "ks_mazda_mx5_cup" or "HyundaiI20NRally2" into words:
// "Mazda Mx5 Cup", "Hyundai I20 N Rally2". Used when the game gives no readable name.
func Words(id string) string {
	id = strings.TrimPrefix(strings.TrimPrefix(id, "ks_"), "KS_")
	var b strings.Builder
	rs := []rune(id)
	up := func(r rune) bool { return r >= 'A' && r <= 'Z' }
	low := func(r rune) bool { return r >= 'a' && r <= 'z' }
	for i, r := range rs {
		if r == '_' || r == '-' || r == ' ' {
			b.WriteRune(' ')
			continue
		}
		// camelCase boundary: "hyundaiI20", and the last capital of a run: "NRally" → "N Rally"
		if i > 0 && up(r) && (low(rs[i-1]) || (rs[i-1] >= '0' && rs[i-1] <= '9') || (up(rs[i-1]) && i+1 < len(rs) && low(rs[i+1]))) {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	parts := strings.Fields(b.String())
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
