// Package acr supports Assetto Corsa Rally. All setups live in one save file, so SetupMan only
// reads them: it never writes, renames or removes anything there (D-111).
package acr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/game"
	"setupman/internal/games/gamekit"
	"setupman/internal/lastopened"
	"setupman/internal/setup"
)

// SaveFile is the file that holds every setup.
const SaveFile = "CarSetupsDataSaveSlot.sav"

// Game is the AC Rally implementation of game.Game.
type Game struct {
	live gamekit.ProcessLive
}

// New creates the AC Rally game.
func New() *Game {
	// acr.exe is the name the user gave (Q-G2), but AC Rally 0.7 never showed as running with it
	// (Q-ACR1). The real program name is to be read on the rig while the game runs (R-14, D-136).
	return &Game{live: gamekit.ProcessLive{Exes: []string{"acr.exe"}}}
}

func (g *Game) ID() string       { return "acr" }
func (g *Game) Name() string     { return "Assetto Corsa Rally" }
func (g *Game) Short() string    { return "AC Rally" }
func (g *Game) SetupExt() string { return ".acrsetup" }

// ReadOnlyReason: see the package comment.
func (g *Game) ReadOnlyReason() string {
	return "AC Rally keeps all setups in one save file, so SetupMan only reads them. Change, rename or remove AC Rally setups in the game."
}

// Live reports whether AC Rally runs (driving isn't detected yet).
func (g *Game) Live() game.LiveState { return g.live.Live() }

// Detect looks for the save folder in %LOCALAPPDATA%\acr\Saved\SaveGames.
func (g *Game) Detect() game.Detection {
	var d game.Detection
	if la := gamekit.LocalAppData(); la != "" {
		if f := filepath.Join(la, "acr", "Saved", "SaveGames"); gamekit.IsDir(f) {
			d.Folder, d.Installed = f, true
		}
	}
	return d
}

// ParseSetup turns a stored record back into comparable fields.
func (g *Game) ParseSetup(content []byte) ([]setup.Field, error) {
	rec, err := ReadRecord(content)
	if err != nil {
		return nil, err
	}
	return fields(rec), nil
}

// Scan reads the save file (read-only). Setups are grouped per car and stage; their "path" is
// <car>/<stage>/<name>, which only identifies them (there are no separate files).
func (g *Game) Scan(folder string) (*game.Scan, error) {
	folder = filepath.Clean(folder)
	if !gamekit.IsDir(folder) {
		return nil, fmt.Errorf("cannot open folder %s", folder)
	}
	res := &game.Scan{Root: folder, ScannedAt: time.Now(), Cars: []game.Car{}, Ignored: []game.Issue{}, Warnings: []game.Issue{}}
	path := filepath.Join(folder, SaveFile)
	info, err := os.Stat(path)
	if err != nil {
		res.Hint = `No AC Rally setups found. Choose the folder that contains ` + SaveFile + ` (normally %LOCALAPPDATA%\acr\Saved\SaveGames).`
		return res, nil
	}
	if info.Size() > 64<<20 {
		res.Ignored = append(res.Ignored, game.Issue{RelPath: SaveFile, Reason: "file too large"})
		return res, nil
	}
	b, err := lastopened.ReadFile(path) // read-only
	if err != nil {
		return nil, err
	}
	recs, err := ReadSave(b)
	if err != nil {
		res.Ignored = append(res.Ignored, game.Issue{RelPath: SaveFile, Reason: "could not read the setups in this file (unknown version of the save format)"})
		return res, nil
	}
	cars := map[string]*game.Car{}
	tracks := map[string]map[string]*game.Track{}
	used := map[string]int{}
	for i, rec := range recs {
		if cars[rec.Car] == nil {
			n := gamekit.Words(rec.Car)
			cars[rec.Car] = &game.Car{ID: rec.Car, Name: n, Brand: strings.Fields(n + " ?")[0], Class: class(rec.Car)}
			tracks[rec.Car] = map[string]*game.Track{}
		}
		tid := strings.ToLower(rec.Track)
		t := tracks[rec.Car][tid]
		if t == nil {
			t = &game.Track{ID: tid, Name: gamekit.Words(rec.Track), Folder: rec.Track}
			tracks[rec.Car][tid] = t
		}
		name := rec.Name
		if name == "" {
			name = "Setup " + strconv.Itoa(i+1)
		}
		key := strings.ToLower(rec.Car + "/" + rec.Track + "/" + name)
		used[key]++
		if used[key] > 1 {
			name = fmt.Sprintf("%s (%d)", name, used[key])
		}
		f := fields(rec)
		mod := rec.Saved
		if mod.IsZero() {
			mod = info.ModTime()
		}
		t.Setups = append(t.Setups, setup.Setup{
			ID: fmt.Sprintf("s%d", i), Name: name, RelPath: rec.Car + "/" + rec.Track + "/" + name,
			Size: int64(len(rec.Raw)), Modified: mod, Fingerprint: compare.Fingerprint(f), Fields: f, Raw: rec.Raw,
		})
	}
	for id, c := range cars {
		for _, t := range tracks[id] {
			sort.Slice(t.Setups, func(i, j int) bool { return strings.ToLower(t.Setups[i].Name) < strings.ToLower(t.Setups[j].Name) })
			t.Pairs = compare.All(t.Setups)
			c.Tracks = append(c.Tracks, *t)
		}
		sort.Slice(c.Tracks, func(i, j int) bool { return c.Tracks[i].Name < c.Tracks[j].Name })
		res.Cars = append(res.Cars, *c)
	}
	sort.Slice(res.Cars, func(i, j int) bool { return res.Cars[i].Name < res.Cars[j].Name })
	if len(res.Cars) == 0 {
		res.Hint = "The AC Rally save file holds no setups yet."
	}
	return res, nil
}

// class reads the rally class from the car id, e.g. "HyundaiI20NRally2" → "Rally2".
func class(car string) string {
	up := strings.ToUpper(car)
	for _, c := range []struct{ code, name string }{
		{"RALLY1", "Rally1"}, {"RALLY2", "Rally2"}, {"RALLY3", "Rally3"}, {"RALLY4", "Rally4"},
		{"WRC", "WRC"}, {"GROUPB", "Group B"},
	} {
		if strings.Contains(up, c.code) {
			return c.name
		}
	}
	return "Cars"
}

// fields turns a record into comparable fields. The conditions the setup was saved for
// (surface, weather, tags, game version) are shown but not compared.
func fields(rec Record) []setup.Field {
	out := []setup.Field{
		info("Info.Version", "Game version", rec.Version),
		info("Info.Surface", "Surface", strings.Join(rec.Surfaces, ", ")),
		info("Info.Weather", "Weather", strings.Join(rec.Weathers, ", ")),
		info("Info.Tags", "Tags", strings.Join(tagNames(rec.Tags), ", ")),
	}
	vals := append([][2]string(nil), rec.Values...)
	sort.SliceStable(vals, func(i, j int) bool { return sortKey(vals[i][0]) < sortKey(vals[j][0]) })
	for _, kv := range vals {
		out = append(out, makeField(kv[0], kv[1]))
	}
	return out
}

func info(path, label, v string) setup.Field {
	return setup.Field{Path: path, Label: label, Section: "Stage", Value: v, Kind: "string", Excluded: true, Hidden: true}
}

func tagNames(tags []string) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = gamekit.Words(strings.TrimSuffix(t, "_Tag"))
	}
	return out
}

var cornerRank = map[string]string{"FrontLeft": "0", "FrontRight": "1", "RearLeft": "2", "RearRight": "3", "Front": "0", "Rear": "2"}

// sortKey: "Wheels.RearLeft.Camber" → "Wheels.Camber.2" (group, setting, corner). Groups keep the file's order.
func sortKey(key string) string {
	p := strings.Split(key, ".")
	if len(p) == 3 {
		if r, ok := cornerRank[p[1]]; ok {
			return groupRank(p[0]) + p[0] + "." + p[2] + "." + r
		}
	}
	return groupRank(p[0]) + key
}

func groupRank(g string) string {
	for i, n := range []string{"Gearbox", "Brakes", "Differentials", "Suspensions", "Axles", "Wheels", "Other"} {
		if g == n {
			return string(rune('a' + i))
		}
	}
	return "z"
}

var corners = map[string]string{"FrontLeft": "FL", "FrontRight": "FR", "RearLeft": "RL", "RearRight": "RR", "Front": "front", "Rear": "rear"}

// makeField: "Wheels.FrontLeft.TyrePressure" = "32.000000" → "Tyre pressure FL" (Tyres), 32.
func makeField(key, val string) setup.Field {
	parts := strings.Split(key, ".")
	group, item, corner := parts[0], parts[len(parts)-1], ""
	if len(parts) == 3 {
		corner = corners[parts[1]]
	}
	label := words(item)
	switch item {
	case "FrontBias":
		label = "Brake bias"
	case "ABS", "TCS":
		label = item
	}
	if corner != "" {
		label += " " + corner
	}
	f := setup.Field{Path: key, Label: label, Section: section(group, item)}
	if n, err := strconv.ParseFloat(val, 64); err == nil {
		f.Num = n
		f.Value = strconv.FormatFloat(n, 'f', -1, 64)
		f.Kind = "float"
		if n == float64(int64(n)) {
			f.Kind = "int"
		}
		switch item {
		case "TyrePressure":
			f.Unit, f.Step = "psi", 1
		case "Camber":
			f.Unit, f.Step = "°", 1
		case "SpringStiffness", "ARBStiffness":
			f.Unit, f.Step = "N/m", 1
		}
		return f
	}
	// "(Discs:Brembo_09.B499.50_355x32_…)" → "Brembo 09.B499.50 355x32 …"
	v := strings.TrimSuffix(strings.TrimPrefix(val, "("), ")")
	if _, after, ok := strings.Cut(v, ":"); ok {
		v = after
	}
	f.Value, f.Kind = strings.ReplaceAll(v, "_", " "), "string"
	return f
}

func section(group, item string) string {
	switch {
	case item == "TyrePressure":
		return "Tyres"
	case item == "Camber" || item == "Toe":
		return "Alignment"
	case group == "Other" && (item == "ABS" || item == "TCS"):
		return "Electronics"
	case group == "Brakes":
		return "Brakes"
	case group == "Differentials" || group == "Gearbox":
		return "Drivetrain"
	case group == "Suspensions" || group == "Axles":
		return "Mechanical balance"
	}
	return "Other"
}

// words splits "LSDPreload" into "LSD preload" and "SpringStiffness" into "Spring stiffness".
func words(s string) string {
	var out []string
	var cur []rune
	rs := []rune(s)
	isUp := func(r rune) bool { return r >= 'A' && r <= 'Z' }
	for i, r := range rs {
		if i > 0 && isUp(r) && (!isUp(rs[i-1]) || (i+1 < len(rs) && !isUp(rs[i+1]))) {
			out = append(out, string(cur))
			cur = nil
		}
		cur = append(cur, r)
	}
	out = append(out, string(cur))
	for i := 1; i < len(out); i++ {
		if strings.ToUpper(out[i]) != out[i] {
			out[i] = strings.ToLower(out[i])
		}
	}
	return strings.Join(out, " ")
}
