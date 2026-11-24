package server

import (
	"encoding/json"
	"net/http"

	"setupman/internal/autostart"
	"setupman/internal/library"
)

func (s *Server) registerSystem() {
	s.mux.HandleFunc("GET /api/autostart", s.handleGetAutostart)
	s.mux.HandleFunc("PUT /api/autostart", s.handlePutAutostart)
}

type autostartState struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

func (s *Server) handleGetAutostart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, autostartState{Supported: autostart.Supported, Enabled: autostart.Enabled()})
}

// handlePutAutostart turns "Start with Windows" on or off (spec D-71).
func (s *Server) handlePutAutostart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	if err := autostart.Set(in.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.lib.Notify(library.Event{Type: "autostart"}) // keeps the tray menu tick in sync
	writeJSON(w, http.StatusOK, autostartState{Supported: autostart.Supported, Enabled: autostart.Enabled()})
}
