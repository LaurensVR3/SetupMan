package server

// Race prep (spec D-141): rank your setups for a session, and remember the last inputs.

import (
	"encoding/json"
	"net/http"
	"strconv"

	"setupman/internal/games/acc"
	"setupman/internal/lfm"
	"setupman/internal/library"
)

func (s *Server) registerPrep() {
	s.mux.HandleFunc("POST /api/prep", s.handlePrep)
	s.mux.HandleFunc("GET /api/prep/inputs", s.handleGetPrepInputs)
	s.mux.HandleFunc("PUT /api/prep/inputs", s.handlePutPrepInputs)
	s.mux.HandleFunc("GET /api/lfm/next", s.handleLFMNext)
	s.mux.HandleFunc("GET /api/lfm/race/{id}", s.handleLFMRace)
}

// lfmRace is an LFM race with what SetupMan can fill in from it (ACC: its car and track names).
type lfmRace struct {
	lfm.Race
	Game string `json:"game,omitempty"` // SetupMan's game id when SetupMan supports the race's game
	Car  string `json:"car,omitempty"`  // the driver's car as the game names it
}

func toPrep(r lfm.Race) lfmRace {
	out := lfmRace{Race: r}
	if r.SimID == 1 { // ACC
		out.Game = "acc"
		if r.HasCar {
			out.Car = acc.CarByModel(r.CarModel)
		}
	}
	return out
}

// handleLFMNext returns the driver's next LFM race(s), for the LFM ID in Settings. A race whose
// entry list no longer has the driver (signed off) is left out (D-143).
func (s *Server) handleLFMNext(w http.ResponseWriter, r *http.Request) {
	id := s.lib.Settings().LFMID
	if id == "" {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "races": []any{}})
		return
	}
	ids, err := s.lfm.NextRaceIDs(id)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	races := []lfmRace{}
	for _, rid := range ids {
		if rc, err := s.lfm.Race(rid, id, true); err == nil && !rc.SignedOff() {
			races = append(races, toPrep(rc))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "races": races})
}

// handleLFMRace returns any LFM race by its number (the next race of a series is the number + 1 …).
func (s *Server) handleLFMRace(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "enter an LFM race number")
		return
	}
	rc, err := s.lfm.Race(id, s.lib.Settings().LFMID, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toPrep(rc))
}

func (s *Server) handlePrep(w http.ResponseWriter, r *http.Request) {
	var in library.PrepInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.Game == "" || in.Car == "" || in.Track == "" {
		writeError(w, http.StatusBadRequest, "choose a game, car and track")
		return
	}
	list, err := s.lib.Prep(in)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setups": list})
}

// The inputs are remembered in SetupMan (not in one browser), so the phone and the PC share them.
func (s *Server) handleGetPrepInputs(w http.ResponseWriter, r *http.Request) {
	v, err := s.lib.GetSaved("prep")
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

func (s *Server) handlePutPrepInputs(w http.ResponseWriter, r *http.Request) {
	var in json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.SetSaved("prep", string(in)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
