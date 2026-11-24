package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"setupman/internal/fuel"
)

func (s *Server) registerFuel() {
	s.mux.HandleFunc("POST /api/fuel/race", s.handleFuelRace)
	s.mux.HandleFunc("POST /api/fuel/qualifying", s.handleFuelQualifying)
	s.mux.HandleFunc("GET /api/fuel/{game}/{car}/{track}", s.handleGetFuelInputs)
	s.mux.HandleFunc("PUT /api/fuel/{game}/{car}/{track}", s.handlePutFuelInputs)
}

func (s *Server) handleFuelRace(w http.ResponseWriter, r *http.Request) {
	var in fuel.Race
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	p, err := fuel.CalcRace(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleFuelQualifying(w http.ResponseWriter, r *http.Request) {
	var in fuel.Qualifying
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	p, err := fuel.CalcQualifying(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// Fuel inputs are remembered per car and track, so the calculation can be prepared in
// advance (D-59) and the pit-stop time is kept per track (D-56).
func fuelKey(r *http.Request) string {
	return "fuel/" + r.PathValue("game") + "/" + strings.ToLower(r.PathValue("car")) + "/" + strings.ToLower(r.PathValue("track"))
}

func (s *Server) handleGetFuelInputs(w http.ResponseWriter, r *http.Request) {
	v, err := s.lib.GetSaved(fuelKey(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == "" {
		v = "{}"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(v))
}

func (s *Server) handlePutFuelInputs(w http.ResponseWriter, r *http.Request) {
	var in json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	if err := s.lib.SetSaved(fuelKey(r), string(in)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
