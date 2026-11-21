package gamekit

import (
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

// MaxFileSize: anything bigger is certainly not a setup file (those are a few KB).
const MaxFileSize = 1 << 20

// Layout describes a setups folder laid out as <root>/<car>/<track>/<setup file>.
type Layout struct {
	Ext   string                                  // setup file type, e.g. ".ini"
	Parse func(raw []byte) ([]setup.Field, error) // reads one setup file
	Car   func(folder string) game.Car            // readable car name, brand, class (Tracks left empty)
	Track func(folder string) string              // readable track name
	Skip  map[string]string                       // other file types and why they are not setups, e.g. ".sp"
	// SkipNames: file names (lower case) of the setup type that the game writes itself and
	// that are not setups, e.g. AC's "last.ini", with the reason shown under ignored files.
	SkipNames map[string]string
	Hint      string // shown when no setups are found
}

// Scan reads the folder (read-only, spec D-07), groups the setups per car and track and
// compares every pair within a track.
func (lay Layout) Scan(root string) (*game.Scan, error) {
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
	seen, nextID := 0, 0

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
		var tracks []game.Track
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
			t := game.Track{ID: strings.ToLower(trackDir.Name()), Name: lay.Track(trackDir.Name()), Folder: trackDir.Name()}
			for _, f := range files {
				rel := carTrack + "/" + f.Name()
				ext := strings.ToLower(filepath.Ext(f.Name()))
				switch {
				case f.IsDir():
					ignore(rel+"/", "subfolder (the game does not read subfolders)")
					continue
				case ext != lay.Ext:
					if why, ok := lay.Skip[ext]; ok {
						ignore(rel, why)
					} else {
						ignore(rel, "not a "+lay.Ext+" file")
					}
					continue
				}
				if why, ok := lay.SkipNames[strings.ToLower(f.Name())]; ok {
					ignore(rel, why)
					continue
				}
				seen++
				s, err := lay.read(filepath.Join(root, carDir.Name(), trackDir.Name(), f.Name()))
				if err != nil {
					ignore(rel, err.Error())
					continue
				}
				s.ID = fmt.Sprintf("s%d", nextID)
				nextID++
				s.RelPath = rel
				t.Setups = append(t.Setups, *s)
			}
			if len(t.Setups) == 0 {
				continue
			}
			sort.Slice(t.Setups, func(i, j int) bool {
				return strings.ToLower(t.Setups[i].Name) < strings.ToLower(t.Setups[j].Name)
			})
			t.Pairs = compare.All(t.Setups)
			tracks = append(tracks, t)
		}
		if len(tracks) == 0 {
			continue // empty car folders are normal (the game creates them)
		}
		car := lay.Car(carDir.Name())
		car.ID = carDir.Name()
		sort.Slice(tracks, func(i, j int) bool { return tracks[i].Name < tracks[j].Name })
		car.Tracks = tracks
		res.Cars = append(res.Cars, car)
	}
	sort.Slice(res.Cars, func(i, j int) bool { return res.Cars[i].Name+res.Cars[i].ID < res.Cars[j].Name+res.Cars[j].ID })
	if len(res.Cars) == 0 {
		if seen > 0 {
			res.Hint = "Found " + lay.Ext + " files but no usable setups. Check the ignored files."
		} else {
			res.Hint = lay.Hint
		}
	}
	return res, nil
}

func (lay Layout) read(path string) (*setup.Setup, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("file too large for a setup (%d KB)", info.Size()/1024)
	}
	raw, err := lastopened.ReadFile(path) // read-only
	if err != nil {
		return nil, err
	}
	fields, err := lay.Parse(raw)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	return &setup.Setup{
		Name:        strings.TrimSuffix(name, filepath.Ext(name)),
		Size:        info.Size(),
		Modified:    info.ModTime(),
		Fingerprint: compare.Fingerprint(fields),
		Fields:      fields,
		Raw:         raw,
	}, nil
}

// Wheels are the four corners, in the order games usually store them.
var Wheels = []string{"FL", "FR", "RL", "RR"}
