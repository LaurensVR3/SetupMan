package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"setupman/internal/lastopened"
	"setupman/internal/lfm"
	"setupman/internal/logos"
	"setupman/internal/naming"
	"setupman/internal/settings"
	"setupman/internal/version"
)

// started identifies this run of SetupMan; the UI reloads itself when it changes
// (e.g. after an update), so it never runs old page code against a new server.
var started = fmt.Sprint(time.Now().UnixNano())

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname() // the computer's name, shown in About (D-134)
	// lastOpened: whether Windows records when files are opened, for setup recognition (D-147)
	writeJSON(w, http.StatusOK, map[string]any{"app": "SetupMan", "started": started, "version": version.Get(), "host": host,
		"lastOpened": lastopened.Check()})
}

func (s *Server) handleGames(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"games": s.lib.Games(), "live": s.lib.Live()})
}

func (s *Server) handleGame(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lib.Game(r.PathValue("game"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown game")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleCar(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lib.Car(r.PathValue("game"), r.PathValue("car"))
	if !ok {
		writeError(w, http.StatusNotFound, "SetupMan has no setups for this car.")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lib.Track(r.PathValue("game"), r.PathValue("car"), r.PathValue("track"))
	if !ok {
		writeError(w, http.StatusNotFound, "SetupMan has no setups for this car on this track.")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	s.lib.Rescan(r.PathValue("game"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleCheckFolder tells the settings page whether a folder looks right, without saving anything.
func (s *Server) handleCheckFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder string `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	writeJSON(w, http.StatusOK, s.lib.CheckFolder(r.PathValue("game"), strings.TrimSpace(req.Folder)))
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st := s.lib.Settings()
	st.Network.PIN, st.Network.Token = "", "" // the PIN is only shown via /api/network, on this PC
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	st := settings.Defaults()
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings: "+err.Error())
		return
	}
	if st.Games == nil {
		st.Games = map[string]settings.Game{}
	}
	// "Managed" can only change through the manage action, which makes a backup first (D-74).
	current := s.lib.Settings()
	st.Network = current.Network // network access only changes through POST /api/network, on this PC (D-100)
	for id, g := range st.Games {
		g.Managed, g.ManagedFolder = current.GameOrDefault(id).Managed, current.GameOrDefault(id).ManagedFolder
		st.Games[id] = g
	}
	for id, cg := range current.Games {
		if _, ok := st.Games[id]; !ok && cg.Managed {
			st.Games[id] = cg
		}
	}
	st.Initials = strings.ToUpper(strings.TrimSpace(st.Initials))
	if st.Initials != "" && !validInitials(st.Initials) {
		writeError(w, http.StatusBadRequest, "initials are 2 to 6 letters, e.g. ABC")
		return
	}
	// Temperature labels per author (D-146): initials in upper case, known readings only.
	rules := naming.TempRules{}
	for k, v := range st.TempRules {
		k = strings.ToUpper(strings.TrimSpace(k))
		okFour := v.Four == "" || v.Four == naming.FourAirTrack || v.Four == naming.FourRange
		okSingle := v.Single == "" || v.Single == naming.SingleAir || v.Single == naming.SingleTrack || v.Single == naming.SingleEither
		if k == "" || len(k) > 20 || !okFour || !okSingle {
			writeError(w, http.StatusBadRequest, "invalid temperature label setting for "+k)
			return
		}
		if v.Four != "" || v.Single != "" {
			rules[k] = v
		}
	}
	st.TempRules = rules
	st.LFMID = strings.TrimSpace(st.LFMID)
	if st.LFMID != "" && !lfm.ValidID(st.LFMID) {
		writeError(w, http.StatusBadRequest, "the LFM ID is the number in your LFM profile address (lowfuelmotorsport.com/profile/<number>)")
		return
	}
	for id, g := range st.Games {
		if g.Folder != "" {
			abs, err := filepath.Abs(strings.TrimSpace(g.Folder))
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			g.Folder = abs
			st.Games[id] = g
		}
	}
	if err := s.store.Save(st); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.lib.Apply()
	writeJSON(w, http.StatusOK, s.lib.Settings())
}

// handleLogo serves a cached brand logo, downloading it from Wikimedia Commons if allowed (D-36).
func (s *Server) handleLogo(w http.ResponseWriter, r *http.Request) {
	path, ct, err := s.logos.Get(r.PathValue("brand"), s.lib.Settings().DownloadLogos)
	if err != nil {
		if !errors.Is(err, logos.ErrNoLogo) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// SVGs from the web may contain scripts; this header stops them from ever running.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "max-age=86400")
	http.ServeFile(w, r, path)
}

// handleEvents streams library events to the browser (Server-Sent Events).
// The connection is idle (no CPU) until something happens.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no") // a reverse proxy (nginx, D-129) must pass events on at once
	events, cancel := s.lib.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	// The current game state straight away, not only when it changes.
	if b, err := json.Marshal(s.lib.LiveEvent()); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	flusher.Flush()
	// A tiny comment every 30 s: proxies close connections that are silent for 60 s (D-129).
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ":\n\n")
			flusher.Flush()
		case e := <-events:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type browseResult struct {
	Path      string     `json:"path"`             // "" = the top level (drives)
	Parent    *string    `json:"parent,omitempty"` // nil at the top level
	Dirs      []dirEntry `json:"dirs"`
	Shortcuts []dirEntry `json:"shortcuts"`
}

// handleBrowse lists the subfolders of ?path=, for the in-page folder picker.
// With an empty path it lists the drives.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	res := browseResult{Dirs: []dirEntry{}, Shortcuts: s.shortcuts()}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		res.Dirs = drives()
		writeJSON(w, http.StatusOK, res)
		return
	}
	p, err := filepath.Abs(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot open folder: "+err.Error())
		return
	}
	res.Path = p
	parent := filepath.Dir(p)
	if parent == p { // drive root: go back to the drive list
		parent = ""
	}
	res.Parent = &parent
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "$") {
			res.Dirs = append(res.Dirs, dirEntry{e.Name(), filepath.Join(p, e.Name())})
		}
	}
	sort.Slice(res.Dirs, func(i, j int) bool {
		return strings.ToLower(res.Dirs[i].Name) < strings.ToLower(res.Dirs[j].Name)
	})
	writeJSON(w, http.StatusOK, res)
}

func drives() []dirEntry {
	if runtime.GOOS != "windows" {
		return []dirEntry{{"/", "/"}}
	}
	var out []dirEntry
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if _, err := os.Stat(root); err == nil {
			out = append(out, dirEntry{string(c) + ":", root})
		}
	}
	return out
}

// shortcuts are handy starting points: detected game folders, Documents and the current folder.
func (s *Server) shortcuts() []dirEntry {
	var out []dirEntry
	for _, g := range s.lib.Games() {
		if g.DetectedFolder != "" {
			out = append(out, dirEntry{g.Name, g.DetectedFolder})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, dirEntry{"Documents", filepath.Join(home, "Documents")})
	}
	if wd, err := os.Getwd(); err == nil {
		out = append(out, dirEntry{"SetupMan folder", wd})
	}
	return out
}

// validInitials: 2 to 6 letters A–Z, as the naming standard reads them from names (D-49).
func validInitials(s string) bool {
	if len(s) < 2 || len(s) > 6 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
