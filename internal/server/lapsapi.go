package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"setupman/internal/history"
)

func (s *Server) registerLaps() {
	s.mux.HandleFunc("GET /api/setups/{id}/laps", s.handleSetupLaps)
	s.mux.HandleFunc("PUT /api/sessions/{id}/setup", s.handleSessionSetup)
	s.mux.HandleFunc("PUT /api/sessions/{id}/stints/{n}/setup", s.handleStintSetup)
	s.mux.HandleFunc("PUT /api/recording/setup", s.handleRecordingSetup)
	s.mux.HandleFunc("GET /api/laps/{game}/{car}/{track}", s.handleTrackLaps)
	s.mux.HandleFunc("GET /api/recording", s.handleRecording)
	s.mux.HandleFunc("GET /api/pit", s.handlePit)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleSession)
}

// handlePit returns the pit stop advice for the session being recorded (D-124).
func (s *Server) handlePit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.lib.PitAdvice())
}

// handleSession returns the report of one session (D-127).
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	d, err := s.lib.SessionReport(id)
	if errors.Is(err, history.ErrNotFound) {
		writeError(w, http.StatusNotFound, "This session is not in SetupMan.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleSetupLaps returns the recorded lap times of a setup (spec D-44, D-48).
func (s *Server) handleSetupLaps(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid setup id")
		return
	}
	d, err := s.lib.SetupLapTimes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleSessionSetup lets the user say which setup was driven in a session (0 = unknown).
func (s *Server) handleSessionSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	var in struct {
		SetupID int64 `json:"setupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.SetSessionSetup(id, in.SetupID); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleStintSetup gives one stint of a session another setup (D-177).
func (s *Server) handleStintSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	n, err := strconv.Atoi(r.PathValue("n"))
	if !ok || err != nil {
		writeError(w, http.StatusBadRequest, "invalid stint")
		return
	}
	var in struct {
		SetupID int64 `json:"setupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.SetStintSetup(id, n, in.SetupID); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRecordingSetup says which setup is driven now (top bar, D-177).
func (s *Server) handleRecordingSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SetupID int64 `json:"setupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.SetupID == 0 {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.SetRecordingSetup(in.SetupID); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleTrackLaps sums up all laps on a car and track, plus sessions without a known setup.
func (s *Server) handleTrackLaps(w http.ResponseWriter, r *http.Request) {
	game, car, track := r.PathValue("game"), r.PathValue("car"), strings.ToLower(r.PathValue("track"))
	st, err := s.lib.TrackLapTimes(game, car, track)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	un, err := s.lib.UnassignedSessions(game, car, track)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	per, err := s.lib.TrackSetupLapTimes(game, car, track)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pl, err := s.lib.TrackPitLoss(game, car, track)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": st, "unassigned": un, "perSetup": per, "pitLoss": pl})
}

func (s *Server) handleRecording(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.lib.Recording())
}
