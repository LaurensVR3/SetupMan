package server

// Page checks (A-7): every function a page's template calls must be one the page provides (its
// setup() result, its props) or a JavaScript global. The blank Lap analysis page of 2026-11-23
// (D-174) called kindLabel, which the page imported but didn't hand to its template; Vue then stops
// drawing the page. This test finds such mistakes before a release, without a browser.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// globals a template may call without the page providing them
var templateGlobals = map[string]bool{
	"Math": true, "Number": true, "String": true, "JSON": true, "Object": true, "Array": true, "Date": true,
	"Boolean": true, "parseInt": true, "parseFloat": true, "isNaN": true, "isFinite": true, "Intl": true,
	"encodeURIComponent": true, "decodeURIComponent": true,
}

var (
	reCall       = regexp.MustCompile(`(^|[^\w.$])([A-Za-z_$][\w$]*)\s*\(`)
	reMustache   = regexp.MustCompile(`(?s)\{\{(.*?)\}\}`)
	reDirective  = regexp.MustCompile(`(?s)\s(?:v-[\w-]+(?::[\w.-]+)?|:[\w.-]+|@[\w.-]+|#[\w-]+)="([^"]*)"`)
	reArrowParam = regexp.MustCompile(`\(?\s*([\w$,\s]*)\)?\s*=>`)
	reQuoted     = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
)

var jsWords = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "return": true, "typeof": true, "function": true, "in": true, "of": true, "new": true}

// templateRanges returns the start and end of each `template: ` ... “ string in a file.
func templateRanges(src string) [][2]int {
	var out [][2]int
	for i := 0; ; {
		j := strings.Index(src[i:], "template: `")
		if j < 0 {
			return out
		}
		start := i + j + len("template: `")
		end := strings.Index(src[start:], "`")
		if end < 0 {
			return out
		}
		out = append(out, [2]int{start, start + end})
		i = start + end + 1
	}
}

// objectKeys returns the top-level keys of the object literal starting at src[open] == '{'
// (shorthand "a", "a: …", "a(…) {…}"); spread = true when it holds a "...spread".
func objectKeys(src string, open int) (keys []string, spread bool) {
	depth, entry := 0, true
	for i := open; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '{' || c == '(' || c == '[':
			depth++
			if depth == 1 {
				entry = true
			}
			continue
		case c == '}' || c == ')' || c == ']':
			depth--
			if depth == 0 {
				return
			}
			continue
		case c == '"' || c == '\'' || c == '`':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
			continue
		}
		if depth != 1 {
			continue
		}
		if c == ',' {
			entry = true
			continue
		}
		if entry && (c == ' ' || c == '\n' || c == '\t' || c == '\r') {
			continue
		}
		if entry {
			entry = false
			if strings.HasPrefix(src[i:], "...") {
				spread = true
				continue
			}
			j := i
			for j < len(src) && (src[j] == '_' || src[j] == '$' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			if j > i {
				keys = append(keys, src[i:j])
			}
		}
	}
	return
}

// provided lists what a component's script part (the code before its template) hands its template.
func provided(script string) (names map[string]bool, unknown bool) {
	names = map[string]bool{}
	add := func(at int) {
		ks, sp := objectKeys(script, at)
		for _, k := range ks {
			names[k] = true
		}
		unknown = unknown || sp
	}
	// setup's result: the last "return {" of setup(), or setup: () => ({ … })
	if i := strings.LastIndex(script, "setup"); i >= 0 {
		body := script[i:]
		if j := strings.Index(body, "=> ({"); j >= 0 && j < 40 {
			add(i + j + len("=> ("))
		} else if j := strings.LastIndex(body, "return {"); j >= 0 {
			add(i + j + len("return "))
		} else {
			unknown = true // a setup we can't read: don't judge this component
		}
	}
	// props: ["a", "b"] or props: { a: …, b: … }
	if i := strings.Index(script, "props: "); i >= 0 {
		rest := script[i+len("props: "):]
		if strings.HasPrefix(rest, "[") {
			end := strings.Index(rest, "]")
			for _, p := range strings.Split(rest[1:end], ",") {
				names[strings.Trim(strings.TrimSpace(p), `"'`)] = true
			}
		} else if strings.HasPrefix(rest, "{") {
			add(i + len("props: "))
		}
	}
	// methods / computed blocks, should a component use them
	for _, block := range []string{"methods: {", "computed: {"} {
		if i := strings.Index(script, block); i >= 0 {
			add(i + len(block) - 1)
		}
	}
	return names, unknown
}

// calledIn returns the functions a template calls in its expressions (mustaches and directives),
// leaving out method calls (a.b()), arrow-function parameters and JavaScript words.
func calledIn(tpl string) []string {
	var exprs []string
	for _, m := range reMustache.FindAllStringSubmatch(tpl, -1) {
		exprs = append(exprs, m[1])
	}
	for _, m := range reDirective.FindAllStringSubmatch(tpl, -1) {
		exprs = append(exprs, m[1])
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range exprs {
		e = reQuoted.ReplaceAllString(e, "''") // text in quotes: "the file (.zip)", 'var(--ink)'
		params := map[string]bool{}
		for _, m := range reArrowParam.FindAllStringSubmatch(e, -1) {
			for _, p := range strings.Split(m[1], ",") {
				params[strings.TrimSpace(p)] = true
			}
		}
		for _, m := range reCall.FindAllStringSubmatch(e, -1) {
			n := m[2]
			if jsWords[n] || params[n] || strings.HasPrefix(n, "$") || templateGlobals[n] || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func TestTemplatesCallOnlyWhatPagesProvide(t *testing.T) {
	var files []string
	for _, dir := range []string{"web/pages", "web/components"} {
		fs, err := filepath.Glob(filepath.Join(dir, "*.js"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, fs...)
	}
	if len(files) < 10 {
		t.Fatalf("found only %d page files", len(files))
	}
	checked := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		prev := 0
		for _, r := range templateRanges(src) {
			script := src[prev:r[0]]
			if k := strings.LastIndex(script, "export const "); k >= 0 {
				script = script[k:] // this component only
			}
			names, unknown := provided(script)
			prev = r[1]
			if unknown {
				continue
			}
			checked++
			var missing []string
			for _, c := range calledIn(src[r[0]:r[1]]) {
				if !names[c] {
					missing = append(missing, c)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				line := strings.Count(src[:r[0]], "\n") + 1
				t.Errorf("%s (template at line %d) calls %s, which the component doesn't provide: return it from setup()",
					filepath.ToSlash(f), line, strings.Join(missing, ", "))
			}
		}
	}
	if checked < 20 {
		t.Errorf("only %d templates could be checked", checked)
	}
}

// The checker itself: it must catch the D-174 mistake.
func TestTemplateCheckerCatchesMissingFunction(t *testing.T) {
	script := `export const P = { setup() { const a = 1; return { a, lapTime, sum: computed(() => 2) }; },`
	names, unknown := provided(script)
	if unknown || !names["a"] || !names["lapTime"] || !names["sum"] {
		t.Fatalf("provided: %v %v", names, unknown)
	}
	tpl := `<i :title="'Download the file (.zip)'"></i><span v-if="l.kind">{{ kindLabel(l.kind).toLowerCase() }}</span><b @click="items.map((x) => fmt(x))">{{ lapTime(l.ms) }} {{ Math.round(a) }}</b>`
	got := calledIn(tpl)
	sort.Strings(got)
	if strings.Join(got, ",") != "fmt,kindLabel,lapTime" {
		t.Errorf("called: %v", got)
	}
}
