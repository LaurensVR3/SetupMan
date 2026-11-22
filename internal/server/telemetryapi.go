package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"setupman/internal/telemetry"
)

const maxTelemetryUpload = 120 << 20 // 120 MB JSON body (~85 MB raw .ld file)

func (s *Server) registerTelemetry() {
	s.mux.HandleFunc("GET /api/telemetry/laps", s.handleTelemetryLaps)
	s.mux.HandleFunc("GET /api/telemetry/compare", s.handleTelemetryCompare)
	s.mux.HandleFunc("PUT /api/telemetry/laps/{id}/reference", s.handleTelemetrySetReference)
	s.mux.HandleFunc("PUT /api/telemetry/laps/{id}", s.handleTelemetryRename)
	s.mux.HandleFunc("DELETE /api/telemetry/laps/{id}", s.handleTelemetryDelete)
	s.mux.HandleFunc("GET /api/telemetry/motec", s.handleTelemetryListMoTeC)
	s.mux.HandleFunc("POST /api/telemetry/import-motec", s.handleTelemetryImportMoTeC)
	s.mux.HandleFunc("POST /api/telemetry/sample/nords24", s.handleTelemetrySampleNords24)
}

func (s *Server) handleTelemetryLaps(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	laps, err := s.lib.TelemetryLaps(q.Get("game"), q.Get("car"), q.Get("track"))
	if err != nil {
		historyError(w, err)
		return
	}
	if laps == nil {
		laps = []telemetry.LapMeta{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"laps":        laps,
		"live":        s.lib.TelemetryLiveStatus(),
		"motecFolder": s.lib.MoTeCFolder(),
	})
}

func (s *Server) handleTelemetryCompare(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	refID, err1 := strconv.ParseInt(q.Get("ref"), 10, 64)
	cmpID, err2 := strconv.ParseInt(q.Get("cmp"), 10, 64)
	if err1 != nil || err2 != nil || refID <= 0 || cmpID <= 0 {
		writeError(w, http.StatusBadRequest, "select both a reference lap and a comparison lap")
		return
	}
	res, err := s.lib.CompareTelemetry(refID, cmpID)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTelemetrySetReference(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid lap id")
		return
	}
	if err := s.lib.SetTelemetryReference(id); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTelemetryRename(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid lap id")
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.lib.RenameTelemetryLap(id, in.Label); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTelemetryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid lap id")
		return
	}
	if err := s.lib.DeleteTelemetryLap(id); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTelemetryListMoTeC(w http.ResponseWriter, r *http.Request) {
	dir, files, err := s.lib.ListMoTeCFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if files == nil {
		files = []telemetry.MoTeCFileInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"folder": dir,
		"files":  files,
	})
}

func (s *Server) handleTelemetryImportMoTeC(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FileName  string `json:"fileName"`  // import directly from ACC's MoTeC folder
		LDName    string `json:"ldName"`    // uploaded .ld filename
		LDData    string `json:"ldData"`    // base64-encoded .ld bytes
		LDXData   string `json:"ldxData"`   // optional base64-encoded .ldx bytes
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTelemetryUpload)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "could not read upload (max ~85 MB)")
		return
	}
	var (
		laps []telemetry.LapMeta
		err  error
	)
	if strings.TrimSpace(in.FileName) != "" {
		laps, err = s.lib.ImportMoTeCFromDisk(in.FileName)
	} else if in.LDData != "" {
		ldBytes, decErr := base64.StdEncoding.DecodeString(in.LDData)
		if decErr != nil {
			writeError(w, http.StatusBadRequest, "invalid base64 .ld file data")
			return
		}
		var ldxBytes []byte
		if in.LDXData != "" {
			ldxBytes, _ = base64.StdEncoding.DecodeString(in.LDXData)
		}
		laps, err = s.lib.ImportMoTeCUpload(in.LDName, ldBytes, ldxBytes)
	} else {
		writeError(w, http.StatusBadRequest, "choose a MoTeC .ld file to import")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"laps": laps, "count": len(laps)})
}

func (s *Server) handleTelemetrySampleNords24(w http.ResponseWriter, r *http.Request) {
	refID, cmpID, err := s.lib.LoadNords24Samples()
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refId": refID, "cmpId": cmpID})
}
