// Package logos downloads car-brand logos from Wikimedia Commons once and caches
// them in %APPDATA%\SetupMan\logos (spec D-36). Logos are never shipped with SetupMan.
//
// Wikimedia rate-limits clients, so downloads run one at a time, each brand is
// fetched at most once, and failures are remembered for a day.
package logos

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UserAgent identifies SetupMan to Wikimedia without any personal data.
const UserAgent = "SetupMan/0.2 (open-source simracing setup manager)"

// commonsFiles maps a brand (as used in the game name tables) to a Wikimedia Commons file.
var commonsFiles = map[string]string{
	// Game logos use "game:<id>". ACC: Kunos Simulazioni, CC BY-SA 4.0 on Wikimedia Commons.
	"game:acc":     "Assetto Corsa Competizione.png",
	"game:ac":      "Assetto Corsa Logo.png", // CC BY-SA 4.0. AC EVO and AC Rally have no logo on Commons (initials are shown)
	"Hyundai":      "Hyundai Motor Company logo.svg",
	"Mazda":        "Mazda logo with emblem, new.svg",
	"Renault":      "Renault 2021 Text.svg",
	"Toyota":       "Toyota carlogo.svg",
	"Alpine":       "Alpine logo.svg",
	"Aston Martin": "Aston Martin wordmark.svg",
	"Audi":         "Audi-Logo 2016.svg",
	"Bentley":      "Bentley.svg",
	"BMW":          "BMW.svg",
	"Chevrolet":    "Chevrolet-logo.png",
	"Ferrari":      "Ferrari wordmark.svg",
	"Ford":         "Ford-Logo-Vector.svg",
	"Ginetta":      "Logo ginetta.png", // badge crop, CC BY 2.0 (Thomas's Pics)
	"Honda":        "Honda Logo.svg",
	"Jaguar":       "Jaguar 2024.svg",
	"KTM":          "KTM-Logo.svg",
	"Lamborghini":  "Lamborghini - logo wordmark (italy, 1963-).svg",
	"Lexus":        "Lexus.svg",
	"Maserati":     "Maserati logo 2.svg",
	"McLaren":      "McLaren Automotive logo.svg",
	"Mercedes-AMG": "Mercedes-AMG logo.svg",
	"Nissan":       "Nissan 2020 logo.svg",
	"Porsche":      "Porsche Wortmarke.svg",
}

// ErrNoLogo means there is no logo for this brand (unknown, switched off, or download failed).
var ErrNoLogo = errors.New("no logo")

// Store is the logo cache.
type Store struct {
	dir     string
	baseURL string // Commons "Special:FilePath" URL; replaced in tests
	client  *http.Client

	mu       sync.Mutex
	download sync.Mutex // one download at a time
	failed   map[string]time.Time
}

// New creates a cache in dir.
func New(dir string) *Store {
	return &Store{
		dir:     dir,
		baseURL: "https://commons.wikimedia.org/wiki/Special:FilePath/",
		client:  &http.Client{Timeout: 15 * time.Second},
		failed:  map[string]time.Time{},
	}
}

// Known reports whether a logo source exists for the brand.
func Known(brand string) bool { _, ok := commonsFiles[brand]; return ok }

// Get returns the cached logo file for a brand, downloading it first if allowed.
func (s *Store) Get(brand string, allowDownload bool) (path, contentType string, err error) {
	file, ok := commonsFiles[brand]
	if !ok {
		return "", "", ErrNoLogo
	}
	ext := strings.ToLower(filepath.Ext(file))
	contentType = map[string]string{".svg": "image/svg+xml", ".png": "image/png"}[ext]
	path = filepath.Join(s.dir, safeName(brand)+ext)
	if isFile(path) {
		return path, contentType, nil
	}
	if !allowDownload {
		return "", "", ErrNoLogo
	}

	s.download.Lock()
	defer s.download.Unlock()
	if isFile(path) { // fetched while we waited
		return path, contentType, nil
	}
	s.mu.Lock()
	failedAt, failed := s.failed[brand]
	s.mu.Unlock()
	if failed && time.Since(failedAt) < 24*time.Hour {
		return "", "", ErrNoLogo
	}
	if err := s.fetch(file, ext, path); err != nil {
		s.mu.Lock()
		s.failed[brand] = time.Now()
		s.mu.Unlock()
		return "", "", fmt.Errorf("%w: %v", ErrNoLogo, err)
	}
	return path, contentType, nil
}

func (s *Store) fetch(file, ext, path string) error {
	u := s.baseURL + url.PathEscape(file)
	if ext == ".png" {
		u += "?width=400" // big photos/PNGs: ask Commons for a small version
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if ext == ".svg" && !strings.Contains(string(b[:min(len(b), 2048)]), "<svg") {
		return errors.New("not an SVG")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func safeName(brand string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '_'
	}, brand)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
