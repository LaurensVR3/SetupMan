package server

// Temperature labels per author (D-146): the overview for Settings → General → Naming standard.

import "net/http"

func (s *Server) registerNaming() {
	s.mux.HandleFunc("GET /api/naming/temperatures", s.handleTempAuthors)
}

func (s *Server) handleTempAuthors(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.TempAuthors()
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
