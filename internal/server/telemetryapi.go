package server

// Telemetry suite API (spec D-159 … D-162).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"setupman/internal/history"
	"setupman/internal/library"
)

func (s *Server) registerTelemetry() {
	s.mux.HandleFunc("GET /api/telemetry", s.handleTelemetry)
	s.mux.HandleFunc("GET /api/telemetry/games/{game}/files", s.handleTelemetryFiles)
	s.mux.HandleFunc("POST /api/telemetry/games/{game}/reindex", s.handleTelemetryReindex)
	s.mux.HandleFunc("GET /api/telemetry/files/{id}", s.handleTelemetryFile)
	s.mux.HandleFunc("GET /api/telemetry/files/{id}/laps/{n}", s.handleTelemetryLap)
	s.mux.HandleFunc("GET /api/telemetry/files/{id}/stint", s.handleTelemetryStint)
	s.mux.HandleFunc("PUT /api/telemetry/files/{id}/setup", s.handleTelemetrySetup)
	s.mux.HandleFunc("PUT /api/telemetry/files/{id}/note", s.handleTelemetryNote)
	s.mux.HandleFunc("PUT /api/telemetry/files/{id}/laps/{n}/reference", s.handleTelemetryReference)
	s.mux.HandleFunc("GET /api/setups/{id}/telemetry", s.handleSetupTelemetry)
	s.mux.HandleFunc("GET /api/sessions/{id}/telemetry", s.handleSessionTelemetry)
}

func telemetryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, history.ErrNotFound):
		writeError(w, http.StatusNotFound, "These laps are no longer in SetupMan's telemetry.")
	case errors.Is(err, library.ErrSetupMismatch):
		writeError(w, http.StatusBadRequest, "That setup is for another car. Choose a setup of this session's car.")
	case errors.Is(err, library.ErrTeleGone):
		writeError(w, http.StatusGone, "This session's MoTeC file is no longer in ACC's telemetry folder.")
	case errors.Is(err, library.ErrNoHistory):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func lapN(r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("n"))
	return n, err == nil && n >= 0
}

// handleTelemetry returns the telemetry overview of every game that has telemetry files.
func (s *Server) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"games": s.lib.TelemetryGames()})
}

// handleTelemetryFiles returns the sessions and laps of every car on one track (?track=), with
// each car's reference lap and setups to choose from.
func (s *Server) handleTelemetryFiles(w http.ResponseWriter, r *http.Request) {
	track := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("track")))
	if track == "" {
		writeError(w, http.StatusBadRequest, "track is missing")
		return
	}
	v, err := s.lib.TelemetryTrack(r.PathValue("game"), track)
	if errors.Is(err, history.ErrNotFound) {
		writeError(w, http.StatusNotFound, "No telemetry for this track in SetupMan. Its files may have been removed from ACC's MoTeC folder.")
		return
	}
	if err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleTelemetryReindex(w http.ResponseWriter, r *http.Request) {
	if err := s.lib.ReindexTelemetry(r.PathValue("game")); err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTelemetryFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid telemetry id")
		return
	}
	d, err := s.lib.TelemetryFile(id)
	if err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleTelemetryLap returns channels of one lap: ?ch=SPEED,THROTTLE (at most 40) and &map=1 for
// the track map.
func (s *Server) handleTelemetryLap(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	n, okN := lapN(r)
	if !ok || !okN {
		writeError(w, http.StatusBadRequest, "invalid lap")
		return
	}
	var names []string
	seen := map[string]bool{}
	for _, c := range strings.Split(r.URL.Query().Get("ch"), ",") {
		if c = strings.TrimSpace(c); c != "" && len(names) < 40 && !seen[strings.ToUpper(c)] {
			seen[strings.ToUpper(c)] = true
			names = append(names, c)
		}
	}
	d, err := s.lib.TelemetryLap(id, n, names, r.URL.Query().Get("map") == "1")
	if err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleTelemetryStint returns the brakes over a session (D-170).
func (s *Server) handleTelemetryStint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid telemetry id")
		return
	}
	st, err := s.lib.TelemetryStint(id)
	if err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleTelemetrySetup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		SetupID int64 `json:"setupId"`
	}
	if !ok || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.SetTelemetrySetup(id, in.SetupID); err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleTelemetryNote saves the note of a file, or of one lap when "lap" is given.
func (s *Server) handleTelemetryNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		Note string `json:"note"`
		Lap  *int   `json:"lap"`
	}
	if !ok || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	n := -1
	if in.Lap != nil && *in.Lap >= 0 {
		n = *in.Lap
	}
	if err := s.lib.SetTelemetryNote(id, n, in.Note); err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTelemetryReference(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	n, okN := lapN(r)
	var in struct {
		On bool `json:"on"`
	}
	if !ok || !okN || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.SetTelemetryReference(id, n, in.On); err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetupTelemetry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid setup id")
		return
	}
	files, err := s.lib.TelemetryForSetup(id)
	if err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleSessionTelemetry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	files, err := s.lib.TelemetryForSession(id)
	if err != nil {
		telemetryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}
