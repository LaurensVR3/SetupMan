package library

// Setup names become file names in the game's setups folder, so they follow Windows' rules for
// file names (D-138): no \ / : * ? " < > |, no control characters, no name Windows reserves
// (CON, NUL, COM1 …), and no space or dot at the end.

import (
	"errors"
	"strings"
)

const badNameChars = `\/:*?"<>|`

// errBadName is the message for a name typed by the user that can't be a file name.
var errBadName = errors.New(`a setup name can't be empty, start with a dot, end with a space or dot, contain \ / : * ? " < > |, or be a name Windows reserves (such as CON or NUL)`)

// validName reports whether name can be used as a setup (file) name as it is.
func validName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(badNameChars, r) {
			return false
		}
	}
	return !reservedName(name)
}

// reservedName: Windows refuses these as file names, also with an extension ("con.json").
func reservedName(name string) bool {
	base := strings.ToUpper(strings.TrimSpace(strings.SplitN(name, ".", 2)[0]))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

// cleanName turns a name from outside (an imported or shared file) into a valid setup name:
// characters Windows doesn't allow become "-", and fallback is used when nothing is left.
func cleanName(name, fallback string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32:
			b.WriteRune(' ')
		case strings.ContainsRune(badNameChars, r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ". ")
	if out == "" {
		return fallback
	}
	if reservedName(out) {
		out = "_" + out
	}
	return out
}
