package server

// Full SetupMan backups (spec D-82, D-92).

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"setupman/internal/fullbackup"
	"setupman/internal/game"
)

func (s *Server) registerFullBackup() {
	s.mux.HandleFunc("GET /api/full-backups", s.handleFullList)
	s.mux.HandleFunc("POST /api/full-backups", s.handleFullCreate)
	s.mux.HandleFunc("POST /api/full-backups/upload", s.handleFullUpload)
	s.mux.HandleFunc("GET /api/full-backups/{id}/download", s.handleFullDownload)
	s.mux.HandleFunc("POST /api/full-backups/{id}/restore", s.handleFullRestore)
	s.mux.HandleFunc("POST /api/open-folder", s.handleOpenFolder)
	s.mux.HandleFunc("GET /api/log", s.handleLogDownload)
}

// handleLogDownload sends setupman.log as a download, for a device on the network that can't
// open the folder on the PC SetupMan runs on (D-175).
func (s *Server) handleLogDownload(w http.ResponseWriter, r *http.Request) {
	if s.Full == nil {
		writeError(w, http.StatusNotFound, "The log file is not available.")
		return
	}
	f, err := os.Open(filepath.Join(s.Full.DataDir, "setupman.log"))
	if err != nil {
		writeError(w, http.StatusNotFound, "There is no log file yet.")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="setupman.log"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleOpenFolder opens a backup folder in Windows Explorer on the PC SetupMan runs on.
// Only SetupMan's own backup folders can be opened (not any path a caller sends).
func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which string `json:"which"` // "full", "log" or a game id
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	var dir string
	switch {
	case in.Which == "full" && s.Full != nil:
		dir = s.Full.Dir()
	case in.Which == "log" && s.Full != nil:
		dir = s.Full.DataDir // setupman.log, for attaching to a problem report (D-156)
	case in.Which != "" && s.lib.Backups != nil:
		dir = s.lib.Backups.GameDir(in.Which)
	default:
		writeError(w, http.StatusBadRequest, "unknown folder")
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := openFolder(dir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"opened": dir, "local": isLoopback(r.RemoteAddr)})
}

func (s *Server) fullAvailable(w http.ResponseWriter) bool {
	if s.Full == nil {
		writeError(w, http.StatusNotFound, "full backups are not available")
		return false
	}
	return true
}

func (s *Server) handleFullList(w http.ResponseWriter, r *http.Request) {
	if s.Full == nil {
		writeJSON(w, http.StatusOK, map[string]any{"backups": []any{}})
		return
	}
	list, err := s.Full.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": list, "dir": s.Full.Dir()})
}

func (s *Server) handleFullCreate(w http.ResponseWriter, r *http.Request) {
	if !s.fullAvailable(w) {
		return
	}
	sum, err := s.lib.CreateFullBackup(s.Full, fullbackup.ReasonManual)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleFullUpload(w http.ResponseWriter, r *http.Request) {
	if !s.fullAvailable(w) {
		return
	}
	var in struct {
		Data []byte `json:"data"` // base64
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 400<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "the file could not be read (at most 300 MB)")
		return
	}
	sum, err := s.Full.AddFile(in.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleFullDownload(w http.ResponseWriter, r *http.Request) {
	if !s.fullAvailable(w) {
		return
	}
	id := r.PathValue("id")
	p := s.Full.Path(id)
	f, err := os.Open(p)
	if p == "" || err != nil {
		writeError(w, http.StatusNotFound, fullbackup.ErrNotFound.Error())
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="SetupMan full backup `+id+`.zip"`)
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleFullRestore schedules the restore and restarts SetupMan, which applies it before
// opening the library.
func (s *Server) handleFullRestore(w http.ResponseWriter, r *http.Request) {
	if !s.fullAvailable(w) {
		return
	}
	if s.Restart == nil {
		writeError(w, http.StatusConflict, "SetupMan can't restart automatically here; restart it manually to complete the restore")
		return
	}
	if s.lib.Live() == game.LiveDriving {
		writeError(w, http.StatusConflict, "backups can't be restored while driving; restore from the pits or the menu")
		return
	}
	if err := s.Full.ScheduleRestore(r.PathValue("id")); err != nil {
		if errors.Is(err, fullbackup.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.Restart()
}
