package ac

// Real values per car (spec D-141): an AC setup file stores numbers whose meaning comes from the
// car's setup.ini (in the installed game, inside data.acd or a data folder). For each setting it
// gives MIN, MAX, STEP and how the game shows the value (SHOW_CLICKS), as Content Manager reads it:
//   0 = the stored number is the value itself (camber is stored in tenths of a degree);
//   1 = stored number × STEP;
//   2 = MIN + stored number × STEP.
// Without the installed game (e.g. on another PC) the values stay as stored.

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"setupman/internal/setup"
)

// acRange is one setting of a car's setup.ini.
type acRange struct {
	min, step float64
	mode      int  // SHOW_CLICKS
	ratios    bool // a gear ratio list: shown by name in the game, kept as stored here
}

type carRanges struct {
	mu    sync.Mutex
	byCar map[string]map[string]acRange // car folder → section → range (nil = not readable)
}

// ranges returns the setup.ini of a car from the installed game, read once per car.
func (g *Game) ranges(model string) map[string]acRange {
	dir := g.contentDir()
	if dir == "" || model == "" || strings.ContainsAny(model, `\/:`) || strings.Contains(model, "..") {
		return nil
	}
	g.carRanges.mu.Lock()
	defer g.carRanges.mu.Unlock()
	if r, ok := g.carRanges.byCar[model]; ok {
		return r
	}
	if g.carRanges.byCar == nil {
		g.carRanges.byCar = map[string]map[string]acRange{}
	}
	r := readRanges(filepath.Join(dir, "cars", model), model)
	g.carRanges.byCar[model] = r
	return r
}

// readRanges reads setup.ini from the car's data.acd, or from its data folder.
func readRanges(carDir, folder string) map[string]acRange {
	var ini []byte
	if data, err := os.ReadFile(filepath.Join(carDir, "data.acd")); err == nil {
		ini, _ = acdFile(data, folder, "setup.ini")
	}
	if ini == nil {
		ini, _ = os.ReadFile(filepath.Join(carDir, "data", "setup.ini"))
	}
	if ini == nil {
		return nil
	}
	return parseRanges(ini)
}

func parseRanges(ini []byte) map[string]acRange {
	out := map[string]acRange{}
	vals := map[string]map[string]string{}
	sec := ""
	sc := bufio.NewScanner(bytes.NewReader(bytes.TrimPrefix(ini, []byte("\xef\xbb\xbf"))))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexAny(line, ";"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			sec = strings.ToUpper(strings.TrimSpace(line[1 : len(line)-1]))
			vals[sec] = map[string]string{}
		case sec != "":
			if k, v, ok := strings.Cut(line, "="); ok {
				vals[sec][strings.ToUpper(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
		}
	}
	for sec, kv := range vals {
		if _, ok := kv["RATIOS"]; ok {
			out[sec] = acRange{ratios: true}
			continue
		}
		if _, ok := kv["MIN"]; !ok {
			continue // not a setting (e.g. [DISPLAY_METHOD])
		}
		r := acRange{step: 1}
		r.min, _ = strconv.ParseFloat(kv["MIN"], 64)
		if s, err := strconv.ParseFloat(kv["STEP"], 64); err == nil && s != 0 {
			r.step = s
		}
		r.mode, _ = strconv.Atoi(kv["SHOW_CLICKS"])
		out[sec] = r
	}
	return out
}

// percentKeys are shown as a percentage in the game (as Content Manager labels them).
var percentKeys = map[string]bool{
	"DIFF_POWER": true, "DIFF_COAST": true, "FRONT_DIFF_COAST": true, "FRONT_DIFF_POWER": true,
	"REAR_DIFF_COAST": true, "REAR_DIFF_POWER": true, "CENTER_DIFF_PRELOAD": true,
	"AWD_FRONT_TORQUE_DISTRIBUTION": true, "ENGINE_LIMITER": true, "FRONT_BIAS": true, "BRAKE_POWER_MULT": true,
}

// applyRanges gives the fields of a setup their real values from the car's setup.ini.
func applyRanges(fields []setup.Field, ranges map[string]acRange) {
	if ranges == nil {
		return
	}
	for i := range fields {
		f := &fields[i]
		r, ok := ranges[f.Path] // a setting's path is its section name, e.g. "CAMBER_LF"
		if !ok || r.ratios || !f.IsNumber() || f.Excluded {
			continue
		}
		prefix := strings.SplitN(f.Path, "_", 2)[0]
		switch r.mode {
		case 1:
			f.Offset, f.Step = 0, r.step
		case 2:
			f.Offset, f.Step = r.min, r.step
		default: // the stored number is the value; camber is stored in tenths of a degree
			f.Offset, f.Step = 0, 1
			if prefix == "CAMBER" {
				f.Step = 0.1
			}
		}
		switch {
		case prefix == "CAMBER" && r.mode == 0:
			f.Unit = "°"
		case percentKeys[f.Path] && r.mode == 0:
			f.Unit = "%"
		case prefix == "PRESSURE" && r.mode == 0:
			f.Unit = "psi"
		case prefix == "FUEL" && r.mode == 0:
			f.Unit = "L"
		}
		f.Real = !(f.Offset == 0 && f.Step == 1 && f.Unit == "")
	}
}
