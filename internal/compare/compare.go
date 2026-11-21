// Package compare scores how similar two setups are and lists their differences.
//
// Score (spec D-20, D-17): the percentage of scored fields that are identical.
// Every field counts equally. A field present in only one setup counts as different.
package compare

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"strings"

	"setupman/internal/setup"
)

// Diff is one field that differs between setup A and setup B.
type Diff struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	A        string `json:"a"`               // "" when the field is missing in A
	B        string `json:"b"`               // "" when the field is missing in B
	Delta    string `json:"delta,omitempty"` // B minus A, for numbers, e.g. "+1" or "-0.5"
	Excluded bool   `json:"excluded"`        // true = not counted in the score
}

// Result is the outcome of comparing two setups.
type Result struct {
	Score     float64 `json:"score"`     // 0..100
	Identical bool    `json:"identical"` // all scored fields equal
	Same      int     `json:"same"`      // number of equal scored fields
	Total     int     `json:"total"`     // number of scored fields (union of both setups)
	Clicks    int     `json:"clicks"`    // sum of |B-A| over differing whole-number fields
	Diffs     []Diff  `json:"diffs"`     // scored differences first, then not-scored ones
}

// Pair is the comparison of two setups, by index into a list of setups.
type Pair struct {
	A int `json:"a"`
	B int `json:"b"`
	Result
	Differ  int    `json:"differ"`  // scored settings that differ
	Summary string `json:"summary"` // the first few scored differences, e.g. "Rear wing +1, TC -1, …"
}

// All compares every pair in a list of setups.
func All(setups []setup.Setup) []Pair {
	pairs := []Pair{}
	for i := 0; i < len(setups); i++ {
		for j := i + 1; j < len(setups); j++ {
			p := Pair{A: i, B: j, Result: Compare(setups[i].Fields, setups[j].Fields)}
			p.Differ, p.Summary = brief(p.Diffs, 5)
			// The full list is large (a track can have thousands of pairs) and is worked out again
			// when two setups are compared, so only the summary is kept.
			p.Diffs = nil
			pairs = append(pairs, p)
		}
	}
	return pairs
}

// brief counts the scored differences and describes the first max of them.
func brief(diffs []Diff, max int) (int, string) {
	n := 0
	var parts []string
	for _, d := range diffs {
		if d.Excluded {
			continue
		}
		n++
		if len(parts) < max {
			parts = append(parts, strings.TrimSpace(d.Label+" "+d.Delta))
		}
	}
	out := strings.Join(parts, ", ")
	if n > max {
		out += ", …"
	}
	return n, out
}

// Compare compares the fields of setup a with those of setup b.
func Compare(a, b []setup.Field) Result {
	bByPath := make(map[string]setup.Field, len(b))
	for _, f := range b {
		bByPath[f.Path] = f
	}
	var res Result
	var excludedDiffs []Diff
	seen := make(map[string]bool, len(a))

	add := func(fa, fb *setup.Field) {
		ref := fa
		if ref == nil {
			ref = fb
		}
		equal := fa != nil && fb != nil && equalValues(*fa, *fb)
		if !ref.Excluded {
			res.Total++
			if equal {
				res.Same++
			}
		}
		if equal {
			return
		}
		d := Diff{Path: ref.Path, Label: ref.Label, Excluded: ref.Excluded}
		if fa != nil {
			d.A = fa.Value
		}
		if fb != nil {
			d.B = fb.Value
		}
		if fa != nil && fb != nil && fa.IsNumber() && fb.IsNumber() {
			delta := fb.Num - fa.Num
			d.Delta = formatDelta(delta)
			if fa.Converted() && fb.Converted() { // in the game's own units, e.g. "+0.2°" (D-141)
				d.Delta = formatDelta(fb.RealValue()-fa.RealValue()) + unitSuffix(fb.Unit)
			}
			if fa.Kind == "int" && fb.Kind == "int" && !ref.Excluded {
				res.Clicks += int(math.Abs(delta))
			}
		}
		if ref.Excluded {
			excludedDiffs = append(excludedDiffs, d)
		} else {
			res.Diffs = append(res.Diffs, d)
		}
	}

	for i := range a {
		fa := &a[i]
		seen[fa.Path] = true
		if fb, ok := bByPath[fa.Path]; ok {
			add(fa, &fb)
		} else {
			add(fa, nil)
		}
	}
	for i := range b {
		if fb := &b[i]; !seen[fb.Path] {
			add(nil, fb)
		}
	}

	res.Diffs = append(res.Diffs, excludedDiffs...)
	if res.Diffs == nil {
		res.Diffs = []Diff{}
	}
	res.Identical = res.Same == res.Total
	if res.Total == 0 {
		res.Score = 100
	} else {
		res.Score = 100 * float64(res.Same) / float64(res.Total)
	}
	return res
}

// Fingerprint is equal for two setups exactly when all their scored fields are equal.
func Fingerprint(fields []setup.Field) string {
	var lines []string
	for _, f := range fields {
		if f.Excluded {
			continue
		}
		v := f.Kind + ":" + f.Value
		if f.IsNumber() {
			v = "num:" + strconv.FormatFloat(f.Num, 'g', -1, 64)
		}
		lines = append(lines, f.Path+"="+v+"\n")
	}
	sort.Strings(lines) // independent of key order in the file
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func equalValues(a, b setup.Field) bool {
	if a.IsNumber() && b.IsNumber() {
		return a.Num == b.Num
	}
	return a.Kind == b.Kind && a.Value == b.Value
}

// unitSuffix: "°" and "%" follow the number directly, other units after a space.
func unitSuffix(u string) string {
	switch u {
	case "":
		return ""
	case "°", "%":
		return u
	}
	return " " + u
}

func formatDelta(d float64) string {
	s := strconv.FormatFloat(d, 'f', -1, 64)
	if math.Abs(d-math.Round(d)) > 1e-9 {
		// Round floats like 0.10000000149 to something readable.
		s = strings.TrimRight(strings.TrimRight(strconv.FormatFloat(d, 'f', 4, 64), "0"), ".")
	}
	if d > 0 {
		return "+" + s
	}
	return s
}
