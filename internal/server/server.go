// Package server is SetupMan's local web server: a JSON API plus the embedded
// web UI. It listens on 127.0.0.1, or on the local network with a PIN when allowed (D-100).
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"

	"setupman/internal/fullbackup"
	"setupman/internal/lfm"
	"setupman/internal/library"
	"setupman/internal/logos"
	"setupman/internal/settings"
)

//go:embed web
var webFiles embed.FS

// Server holds the HTTP handlers.
type Server struct {
	lib   *library.Library
	store *settings.Store
	logos *logos.Store
	lfm   *lfm.Client // Low Fuel Motorsport race information (D-142)
	mux   *http.ServeMux

	// Full SetupMan backups (D-92) and how to restart SetupMan; set by main.
	Full    *fullbackup.Store
	Restart func()
	// Port SetupMan listens on, and whether it listens on the network now (set by main).
	Port      int
	Listening bool
}

// New creates the server.
func New(lib *library.Library, store *settings.Store, logoStore *logos.Store) *Server {
	s := &Server{lib: lib, store: store, logos: logoStore, lfm: lfm.New(), mux: http.NewServeMux()}

	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err) // only possible if the embed directive is broken
	}
	files := http.FileServerFS(web)
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache") // always use the files of the running version
		files.ServeHTTP(w, r)
	})
	s.mux.HandleFunc("GET /api/ping", s.handlePing)
	s.mux.HandleFunc("GET /api/games", s.handleGames)
	s.mux.HandleFunc("GET /api/games/{game}", s.handleGame)
	s.mux.HandleFunc("GET /api/games/{game}/cars/{car}", s.handleCar)
	s.mux.HandleFunc("GET /api/games/{game}/cars/{car}/tracks/{track}", s.handleTrack)
	s.mux.HandleFunc("POST /api/games/{game}/rescan", s.handleRescan)
	s.mux.HandleFunc("POST /api/games/{game}/check-folder", s.handleCheckFolder)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	s.mux.HandleFunc("GET /api/browse", s.handleBrowse)
	s.mux.HandleFunc("GET /api/logos/{brand}", s.handleLogo)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.registerHistory()
	s.registerSystem()
	s.registerStaging()
	s.registerFuel()
	s.registerLaps()
	s.registerTransfer()
	s.registerFullBackup()
	s.registerNetwork()
	s.registerPrep()
	s.registerNaming()
	s.registerTelemetry()
	return s
}

// ServeHTTP adds two protections for a local app:
//   - requests from this PC must be addressed to localhost (blocks "DNS rebinding",
//     where a web page tries to talk to local apps through a fake domain name);
//   - requests from other computers need network access switched on and the PIN (D-100);
//   - requests that change something must send JSON (blocks plain HTML form posts
//     from other web sites);
//   - no other web site may show SetupMan inside its own page (blocks "clickjacking", where a
//     page hides SetupMan's buttons under its own, D-140).
//
// LocalName is the friendly address of SetupMan on its own PC: http://setupman.localhost (D-116).
const LocalName = "setupman.localhost"

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if nw := s.lib.Settings().Network; nw.HostName != "" && strings.EqualFold(host, nw.HostName) {
		// Through the network name (D-129) the request comes via a reverse proxy, possibly on
		// this PC, so it is always treated as another device: PIN required.
		if !s.checkRemote(w, r, host) {
			return
		}
	} else if isLoopback(r.RemoteAddr) {
		// Only these names: a web page can't point a made-up domain at this PC (DNS rebinding).
		// Browsers send every *.localhost name to this PC, so LocalName needs no setup (D-116).
		if host != "127.0.0.1" && host != "localhost" && !strings.EqualFold(host, LocalName) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	} else if !s.checkRemote(w, r, host) { // another computer (D-100)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead &&
		!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	withGzip(w, r, s.mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
