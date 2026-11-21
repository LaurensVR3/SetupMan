// Package acc reads Assetto Corsa Competizione setup files (.json) and turns
// them into a flat, ordered list of fields that can be compared.
//
// The file is decoded generically (not into fixed structs) so that fields
// added by future game updates are still picked up and compared.
package acc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"setupman/internal/setup"
)

// Field is the game-neutral field type.
type Field = setup.Field

// Setup is a parsed setup file.
type Setup struct {
	CarName string
	Fields  []Field // in file order
}

// pitStrategyPath is the array whose duplicate entries are dropped (spec D-16).
const pitStrategyPath = "basicSetup.strategy.pitStrategy"

// Parse reads one ACC setup file.
func Parse(r io.Reader) (*Setup, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	root, err := readNode(dec)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("invalid JSON: unexpected data after the end")
	}
	if root.kind != kindObject {
		return nil, errors.New("not an ACC setup: top level is not an object")
	}
	car, ok := root.get("carName")
	if !ok || car.kind != kindScalar {
		return nil, errors.New("not an ACC setup: no carName")
	}
	if _, ok := root.get("basicSetup"); !ok {
		return nil, errors.New("not an ACC setup: no basicSetup")
	}

	// The main tyres are needed to recognise copied pit-strategy slots (spec D-23).
	mainTyres := ""
	if basic, _ := root.get("basicSetup"); basic.kind == kindObject {
		if t, ok := basic.get("tyres"); ok {
			mainTyres = t.canonical()
		}
	}
	s := &Setup{CarName: fmt.Sprint(car.scalar)}
	flatten(root, nil, "", s, mainTyres)
	applyCarUnits(s) // real values for this car (D-141)
	applyScreens(s)  // the game's own tabs, names and order (D-144)
	return s, nil
}

// ---------- flattening ----------

// flatten walks the tree and appends one Field per scalar value.
// keys holds the object keys on the way down (used for labels and sections).
func flatten(n *node, keys []string, path string, s *Setup, mainTyres string) {
	switch n.kind {
	case kindObject:
		for i, k := range n.keys {
			flatten(n.vals[i], append(keys, k), join(path, k), s, mainTyres)
		}
	case kindArray:
		if path == pitStrategyPath {
			for i, e := range pitEntries(n.vals, mainTyres) {
				prefix := fmt.Sprintf("Pit strategy %d: ", i+1)
				sub := &Setup{}
				flatten(e, nil, "", sub, "")
				for _, f := range sub.Fields {
					f.Path = fmt.Sprintf("%s#%d.%s", path, i+1, f.Path)
					f.Label = prefix + f.Label
					f.Section = "Pit strategy"
					f.Excluded = isExcluded(f.Path)
					s.Fields = append(s.Fields, f)
				}
			}
			return
		}
		for i, e := range n.vals {
			p := fmt.Sprintf("%s[%d]", path, i)
			if e.kind == kindScalar {
				s.Fields = append(s.Fields, makeField(p, keys, i, len(n.vals), e.scalar))
			} else {
				flatten(e, keys, p, s, mainTyres)
			}
		}
	case kindScalar:
		s.Fields = append(s.Fields, makeField(path, keys, -1, 0, n.scalar))
	}
}

func makeField(path string, keys []string, idx, arrLen int, v any) Field {
	f := Field{
		Path:     path,
		Label:    label(keys, idx, arrLen),
		Section:  section(keys),
		Excluded: isExcluded(path),
	}
	if len(keys) > 0 {
		if u, ok := realUnits[keys[len(keys)-1]]; ok {
			f.Real, f.Unit, f.Offset, f.Step = true, u.unit, u.offset, u.step
		}
		f.Choices = choices[keys[len(keys)-1]]
	}
	switch t := v.(type) {
	case json.Number:
		f.Value = t.String()
		f.Num, _ = t.Float64()
		if _, err := strconv.ParseInt(f.Value, 10, 64); err == nil {
			f.Kind = "int"
		} else {
			f.Kind = "float"
		}
	case string:
		f.Value, f.Kind = t, "string"
	case bool:
		f.Value, f.Kind = strconv.FormatBool(t), "bool"
	default:
		f.Value, f.Kind = "null", "null"
	}
	return f
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// pitEntries returns the pit-strategy slots that matter (spec D-16, D-23).
// Slot 1 is always kept. A later slot is a "copied" slot and dropped when it is
// identical to a slot kept before it, or when its tyres are just a copy of the
// main setup's tyres (ACC fills unused slots like that).
func pitEntries(vals []*node, mainTyres string) []*node {
	seen := map[string]bool{}
	var out []*node
	for i, v := range vals {
		c := v.canonical()
		if seen[c] {
			continue
		}
		if i > 0 && mainTyres != "" && v.kind == kindObject {
			if t, ok := v.get("tyres"); ok && t.canonical() == mainTyres {
				continue
			}
		}
		seen[c] = true
		out = append(out, v)
	}
	return out
}

// ---------- ordered JSON tree ----------
// encoding/json maps lose key order; we keep it so fields show in file order.

type nodeKind int

const (
	kindScalar nodeKind = iota
	kindObject
	kindArray
)

type node struct {
	kind   nodeKind
	keys   []string // objects only
	vals   []*node  // objects and arrays
	scalar any      // json.Number, string, bool or nil
}

func (n *node) get(key string) (*node, bool) {
	for i, k := range n.keys {
		if k == key {
			return n.vals[i], true
		}
	}
	return nil, false
}

// canonical returns a text form that is equal for equal content
// (object keys sorted, numbers compared by value).
func (n *node) canonical() string {
	var b bytes.Buffer
	n.writeCanonical(&b)
	return b.String()
}

func (n *node) writeCanonical(b *bytes.Buffer) {
	switch n.kind {
	case kindObject:
		idx := make([]int, len(n.keys))
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(a, c int) bool { return n.keys[idx[a]] < n.keys[idx[c]] })
		b.WriteByte('{')
		for j, i := range idx {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(n.keys[i]))
			b.WriteByte(':')
			n.vals[i].writeCanonical(b)
		}
		b.WriteByte('}')
	case kindArray:
		b.WriteByte('[')
		for i, v := range n.vals {
			if i > 0 {
				b.WriteByte(',')
			}
			v.writeCanonical(b)
		}
		b.WriteByte(']')
	default:
		if num, ok := n.scalar.(json.Number); ok {
			if f, err := num.Float64(); err == nil {
				b.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
				return
			}
		}
		enc, _ := json.Marshal(n.scalar)
		b.Write(enc)
	}
}

func readNode(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &node{kind: kindObject}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				v, err := readNode(dec)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, key)
				n.vals = append(n.vals, v)
			}
			_, err := dec.Token() // closing }
			return n, err
		case '[':
			n := &node{kind: kindArray}
			for dec.More() {
				v, err := readNode(dec)
				if err != nil {
					return nil, err
				}
				n.vals = append(n.vals, v)
			}
			_, err := dec.Token() // closing ]
			return n, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return &node{kind: kindScalar, scalar: t}, nil
	}
}

// stripIndexes turns "a.b[2]" into "a.b" and "x#1.y" into "x.y".
func stripIndexes(path string) string {
	var b strings.Builder
	skip := false
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch {
		case c == '[':
			skip = true
		case c == ']':
			skip = false
		case c == '#':
			for i+1 < len(path) && path[i+1] >= '0' && path[i+1] <= '9' {
				i++
			}
		case !skip:
			b.WriteByte(c)
		}
	}
	return b.String()
}
