package ac

// Changing AC setups (spec D-85, D-137). Only the number after VALUE= (or another key) of the
// changed settings is replaced; every other byte of the file stays exactly as it was.

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Edit applies edits (field path → new whole number) to an AC setup file. A path is the section
// name ("PRESSURE_LF" = its VALUE) or "SECTION.KEY". Values may be below zero (AC stores camber,
// for example, as -39). Only settings that hold a whole number can be changed; the car, author and
// Custom Shaders Patch details can't.
func Edit(content []byte, edits map[string]string) ([]byte, error) {
	want := map[string]int64{} // "SECTION.KEY" → new value
	for path, val := range edits {
		sec, key, ok := strings.Cut(strings.ToUpper(path), ".")
		if !ok {
			key = "VALUE"
		}
		if sec == "CAR" || sec == "ABOUT" || strings.HasPrefix(sec, "__") {
			return nil, fmt.Errorf("%s can't be changed", path)
		}
		v, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a whole number", path, val)
		}
		want[sec+"."+key] = v
	}

	var out bytes.Buffer
	found := map[string]bool{}
	sec := ""
	for _, line := range bytes.SplitAfter(content, []byte("\n")) {
		body := strings.TrimRight(string(line), "\r\n")
		trimmed := strings.TrimSpace(body)
		switch {
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			sec = strings.ToUpper(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
		case trimmed != "" && trimmed[0] != ';' && trimmed[0] != '#':
			k, old, ok := strings.Cut(body, "=")
			id := sec + "." + strings.ToUpper(strings.TrimSpace(k))
			if v, change := want[id]; ok && change {
				if _, err := strconv.ParseInt(strings.TrimSpace(old), 10, 64); err != nil {
					return nil, fmt.Errorf("%s is not a whole number in this setup and can't be changed", id)
				}
				end := line[len(body):] // keep the line ending as it was
				out.WriteString(k + "=" + strconv.FormatInt(v, 10))
				out.Write(end)
				found[id] = true
				continue
			}
		}
		out.Write(line)
	}
	for id := range want {
		if !found[id] {
			return nil, fmt.Errorf("%s: no such setting in this setup", strings.TrimSuffix(id, ".VALUE"))
		}
	}
	return out.Bytes(), nil
}
