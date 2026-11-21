package library

import "testing"

func TestSetupNames(t *testing.T) {
	for _, ok := range []string{"ABC R 1.10.4 2327 TC25", "quali (2)", "BGK", "console", "COM10", "a.b"} {
		if !validName(ok) {
			t.Errorf("validName(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", " x", "x ", ".x", "x.", "a:b", "a/b", `a\b`, "a?b", "a*b", `a"b`, "a<b", "a|b", "con", "NUL", "com1", "LPT9", "aux.json", "a\tb"} {
		if validName(bad) {
			t.Errorf("validName(%q) = true", bad)
		}
	}
	cases := map[string]string{
		"evil:name?":   "evil-name-",
		"quali:v2":     "quali-v2",
		"  .hidden.  ": "hidden",
		"con":          "_con",
		"":             "Imported setup",
		"???":          "---",
		"ABC R 2327":   "ABC R 2327",
		"line\nbreak":  "line break",
		`Car\Track\x`:  "Car-Track-x",
	}
	for in, want := range cases {
		got := cleanName(in, "Imported setup")
		if got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
		if !validName(got) {
			t.Errorf("cleanName(%q) = %q is not valid", in, got)
		}
	}
}
