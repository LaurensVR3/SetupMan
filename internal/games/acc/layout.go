package acc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"setupman/internal/compare"
	"setupman/internal/game"
	"setupman/internal/lastopened"
	"setupman/internal/setup"
)

// MaxFileSize: anything bigger is certainly not an ACC setup (those are ~2-9 KB).
const MaxFileSize = 1 << 20

// ScanFolder reads a folder laid out like ACC's Setups folder
// (<root>/<car>/<track>/<setup>.json), groups the setups per car and track
// and compares every pair within a track.
//
// The folder is only ever read, never written (spec D-07).
func ScanFolder(root string) (*game.Scan, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("cannot open folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", root)
	}

	res := &game.Scan{Root: root, ScannedAt: time.Now(), Cars: []game.Car{}, Ignored: []game.Issue{}, Warnings: []game.Issue{}}
	ignore := func(rel, reason string) { res.Ignored = append(res.Ignored, game.Issue{RelPath: rel, Reason: reason}) }
	jsonSeen := 0
	nextID := 0

	carDirs, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("cannot read folder: %w", err)
	}
	for _, carDir := range carDirs {
		if !carDir.IsDir() {
			ignore(carDir.Name(), "not inside a <car>/<track> folder")
			continue
		}
		trackDirs, err := os.ReadDir(filepath.Join(root, carDir.Name()))
		if err != nil {
			ignore(carDir.Name(), "cannot read folder: "+err.Error())
			continue
		}
		tracksByID := map[string]*game.Track{}
		spellings := map[string]map[string]int{}

		for _, trackDir := range trackDirs {
			carTrack := carDir.Name() + "/" + trackDir.Name()
			if !trackDir.IsDir() {
				ignore(carTrack, "not inside a <car>/<track> folder")
				continue
			}
			files, err := os.ReadDir(filepath.Join(root, carDir.Name(), trackDir.Name()))
			if err != nil {
				ignore(carTrack, "cannot read folder: "+err.Error())
				continue
			}
			for _, f := range files {
				rel := carTrack + "/" + f.Name()
				if f.IsDir() {
					ignore(rel+"/", "subfolder (the game does not read subfolders)")
					continue
				}
				if !strings.EqualFold(filepath.Ext(f.Name()), ".json") {
					ignore(rel, "not a .json file")
					continue
				}
				jsonSeen++
				s, carName, err := readSetup(filepath.Join(root, carDir.Name(), trackDir.Name(), f.Name()))
				if err != nil {
					ignore(rel, err.Error())
					continue
				}
				if !strings.EqualFold(carName, carDir.Name()) {
					res.Warnings = append(res.Warnings, game.Issue{RelPath: rel,
						Reason: fmt.Sprintf("file says car %q but is in folder %q", carName, carDir.Name())})
				}
				s.ID = fmt.Sprintf("s%d", nextID)
				nextID++
				s.RelPath = rel

				id := strings.ToLower(trackDir.Name())
				t := tracksByID[id]
				if t == nil {
					t = &game.Track{ID: id, Name: TrackName(id)}
					tracksByID[id] = t
					spellings[id] = map[string]int{}
				}
				spellings[id][trackDir.Name()]++
				t.Setups = append(t.Setups, *s)
			}
		}
		if len(tracksByID) == 0 {
			continue // empty car folders are normal (the game creates them)
		}

		name, brand, class, year := CarInfo(carDir.Name())
		car := game.Car{ID: carDir.Name(), Name: name, Brand: brand, Class: class, Year: year}
		for id, t := range tracksByID {
			t.Folder = mostCommon(spellings[id])
			sort.Slice(t.Setups, func(i, j int) bool {
				return strings.ToLower(t.Setups[i].Name) < strings.ToLower(t.Setups[j].Name)
			})
			t.Pairs = compare.All(t.Setups)
			car.Tracks = append(car.Tracks, *t)
		}
		sort.Slice(car.Tracks, func(i, j int) bool { return car.Tracks[i].Name < car.Tracks[j].Name })
		res.Cars = append(res.Cars, car)
	}
	sort.Slice(res.Cars, func(i, j int) bool { return res.Cars[i].Name+res.Cars[i].ID < res.Cars[j].Name+res.Cars[j].ID })

	if len(res.Cars) == 0 {
		if jsonSeen > 0 {
			res.Hint = "Found .json files but no usable setups. Check the ignored files."
		} else {
			res.Hint = "No setups found. Choose the folder that contains the car folders (normally Documents\\Assetto Corsa Competizione\\Setups)."
		}
	}
	return res, nil
}

// readSetup opens a file read-only and parses it. It returns the car name found in the file.
func readSetup(path string) (*setup.Setup, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if info.Size() > MaxFileSize {
		return nil, "", fmt.Errorf("file too large for a setup (%d KB)", info.Size()/1024)
	}
	raw, err := lastopened.ReadFile(path) // read-only
	if err != nil {
		return nil, "", err
	}
	parsed, err := Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	name := filepath.Base(path)
	return &setup.Setup{
		Name:        strings.TrimSuffix(name, filepath.Ext(name)),
		Size:        info.Size(),
		Modified:    info.ModTime(),
		Fingerprint: compare.Fingerprint(parsed.Fields),
		Fields:      parsed.Fields,
		Raw:         raw,
	}, parsed.CarName, nil
}

// mostCommon returns the most frequent spelling; ties go to the alphabetically first.
func mostCommon(counts map[string]int) string {
	best, bestN := "", -1
	for s, n := range counts {
		if n > bestN || (n == bestN && s < best) {
			best, bestN = s, n
		}
	}
	return best
}
