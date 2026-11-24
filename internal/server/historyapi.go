package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"setupman/internal/history"
	"setupman/internal/library"
)

func (s *Server) registerHistory() {
	s.mux.HandleFunc("GET /api/setups/{id}", s.handleSetup)
	s.mux.HandleFunc("PUT /api/setups/{id}", s.handleUpdateSetup)
	s.mux.HandleFunc("PUT /api/setups/{id}/parent", s.handleRelink)
	s.mux.HandleFunc("GET /api/versions/{id}", s.handleVersion)
	s.mux.HandleFunc("GET /api/setups/{id}/latest", s.handleLatest)
	s.mux.HandleFunc("GET /api/changes", s.handleChanges)
	s.mux.HandleFunc("GET /api/games/{game}/cars/{car}/tracks/{track}/families", s.handleFamilies)
	s.mux.HandleFunc("POST /api/changes/seen", s.handleChangesSeen)
	s.mux.HandleFunc("POST /api/games/{game}/history/preview", s.handleHistoryPreview(false))
	s.mux.HandleFunc("POST /api/games/{game}/history/apply", s.handleHistoryPreview(true))
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func historyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, history.ErrNotFound):
		writeError(w, http.StatusNotFound, "This setup is not in SetupMan.")
	case errors.Is(err, library.ErrNoHistory):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	d, err := s.lib.SetupDetail(id)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	var in history.Info
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	st, err := s.lib.UpdateSetupInfo(id, in)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleRelink(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	var in struct {
		ParentVersionID int64 `json:"parentVersionId"` // 0 = no parent
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	if err := s.lib.Relink(id, in.ParentVersionID); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoVersion)
		return
	}
	v, fields, err := s.lib.VersionFields(id)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": v, "fields": fields})
}

// handleLatest returns a setup's name and newest values (to compare setups hidden from the game).
func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	name, v, fields, err := s.lib.LatestFields(id)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "version": v, "fields": fields})
}

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	ev, unseen, err := s.lib.Changes(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev, "unseen": unseen})
}

func (s *Server) handleChangesSeen(w http.ResponseWriter, r *http.Request) {
	if err := s.lib.MarkChangesSeen(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleHistoryPreview(apply bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Threshold     float64 `json:"threshold"`
			NameThreshold float64 `json:"nameThreshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, msgBadRequest)
			return
		}
		if in.Threshold < 50 || in.Threshold > 100 || in.NameThreshold < 40 || in.NameThreshold > 100 {
			writeError(w, http.StatusBadRequest, "thresholds out of range")
			return
		}
		p, err := s.lib.HistoryPreview(r.PathValue("game"), history.Config{Threshold: in.Threshold, NameThreshold: in.NameThreshold}, apply)
		if err != nil {
			historyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

// handleFamilies returns every family tree of one car on one track.
func (s *Server) handleFamilies(w http.ResponseWriter, r *http.Request) {
	roots, err := s.lib.TrackFamilies(r.PathValue("game"), r.PathValue("car"), r.PathValue("track"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"families": roots})
}
