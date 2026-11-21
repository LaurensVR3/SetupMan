package acc

// Editing setups (spec D-85): change values (in ACC's steps/clicks) and write the file back
// in the same layout ACC uses. Values that are not changed keep their exact original text.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// EditSetup applies edits (field path → new whole-number value) to a setup file and
// returns the new file content. Only whole-number fields can be changed; values the game
// calculates itself (floats) are left alone.
func (g *Game) EditSetup(content []byte, edits map[string]string) ([]byte, error) {
	return Edit(content, edits)
}

// Edit is EditSetup without a Game value.
func Edit(content []byte, edits map[string]string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	root, err := readNode(dec)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("invalid JSON: unexpected data after the end")
	}
	mainTyres := ""
	if basic, ok := root.get("basicSetup"); ok && basic.kind == kindObject {
		if t, ok := basic.get("tyres"); ok {
			mainTyres = t.canonical()
		}
	}
	for path, val := range edits {
		n, err := resolve(root, path, mainTyres)
		if err != nil {
			return nil, err
		}
		old, ok := n.scalar.(json.Number)
		if !ok {
			return nil, fmt.Errorf("%s is not a number", path)
		}
		if _, err := strconv.ParseInt(old.String(), 10, 64); err != nil {
			return nil, fmt.Errorf("%s is calculated by the game and can't be changed", path)
		}
		v, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("%s: %q is not a whole number of 0 or more", path, val)
		}
		n.scalar = json.Number(strconv.FormatInt(v, 10))
	}
	var b bytes.Buffer
	writeACC(&b, root, 0, true)
	b.WriteString("\n")
	return b.Bytes(), nil
}

// resolve finds the node for a flattened path like "basicSetup.tyres.tyrePressure[2]" or
// "basicSetup.strategy.pitStrategy#1.fuelToAdd" (#n = n-th distinct pit strategy, as listed).
func resolve(root *node, path, mainTyres string) (*node, error) {
	n := root
	rest := path
	for rest != "" {
		// next key, up to "." or "[" or "#"
		end := strings.IndexAny(rest, ".[#")
		key := rest
		if end >= 0 {
			key = rest[:end]
			rest = rest[end:]
		} else {
			rest = ""
		}
		if key != "" {
			child, ok := n.get(key)
			if !ok || n.kind != kindObject {
				return nil, fmt.Errorf("%s: no such setting", path)
			}
			n = child
		}
		switch {
		case strings.HasPrefix(rest, "."):
			rest = rest[1:]
		case strings.HasPrefix(rest, "["):
			close := strings.Index(rest, "]")
			i, err := strconv.Atoi(rest[1:close])
			if err != nil || n.kind != kindArray || i < 0 || i >= len(n.vals) {
				return nil, fmt.Errorf("%s: no such setting", path)
			}
			n, rest = n.vals[i], rest[close+1:]
		case strings.HasPrefix(rest, "#"):
			j := 1
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			k, _ := strconv.Atoi(rest[1:j])
			entries := pitEntries(n.vals, mainTyres)
			if n.kind != kindArray || k < 1 || k > len(entries) {
				return nil, fmt.Errorf("%s: no such setting", path)
			}
			n, rest = entries[k-1], rest[j:]
		}
	}
	if n.kind != kindScalar {
		return nil, fmt.Errorf("%s is not a single value", path)
	}
	return n, nil
}

// writeACC writes JSON in the layout ACC uses: tabs, the opening brace of an object value on
// its own line, arrays of numbers on one line ("[ 1, 2 ]").
func writeACC(b *bytes.Buffer, n *node, depth int, top bool) {
	ind := strings.Repeat("\t", depth)
	switch n.kind {
	case kindObject:
		if !top {
			b.WriteString("\n" + ind)
		}
		b.WriteString("{")
		for i, k := range n.keys {
			b.WriteString("\n" + ind + "\t" + strconv.Quote(k) + ":")
			v := n.vals[i]
			if v.kind != kindObject {
				b.WriteString(" ")
			}
			writeACC(b, v, depth+1, false)
			if i < len(n.keys)-1 {
				b.WriteString(",")
			}
		}
		b.WriteString("\n" + ind + "}")
	case kindArray:
		scalars := true
		for _, v := range n.vals {
			if v.kind != kindScalar {
				scalars = false
			}
		}
		if scalars {
			b.WriteString("[ ")
			for i, v := range n.vals {
				if i > 0 {
					b.WriteString(", ")
				}
				writeScalar(b, v.scalar)
			}
			b.WriteString(" ]")
			return
		}
		b.WriteString("[")
		for i, v := range n.vals {
			writeACC(b, v, depth+1, false)
			if i < len(n.vals)-1 {
				b.WriteString(",")
			}
		}
		b.WriteString("\n" + ind + "]")
	default:
		writeScalar(b, n.scalar)
	}
}

func writeScalar(b *bytes.Buffer, v any) {
	switch t := v.(type) {
	case json.Number:
		b.WriteString(t.String()) // original text, so floats keep every digit
	case nil:
		b.WriteString("null")
	default:
		enc, _ := json.Marshal(t)
		b.Write(enc)
	}
}
