package server

// Exporting and importing setups (spec D-91).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"setupman/internal/library"
)

// maxImportBody limits an import upload (files arrive base64-encoded in JSON).
const maxImportBody = 100 << 20

func (s *Server) registerTransfer() {
	s.mux.HandleFunc("GET /api/export", s.handleExport)
	s.mux.HandleFunc("POST /api/games/{game}/cars/{car}/tracks/{track}/import", s.handleImport)
	s.mux.HandleFunc("GET /api/setups/{id}/share", s.handleShare)
	s.mux.HandleFunc("POST /api/shared/import", s.handleImportShared)
}

// handleShare downloads a setup as one .setupman file (D-94): ?note=1&laps=1 include those.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	q := r.URL.Query()
	data, name, err := s.lib.Share(id, q.Get("note") == "1", q.Get("laps") == "1")
	if err != nil {
		historyError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Write(data)
}

// handleImportShared imports shared setups into the car and track named in each file.
func (s *Server) handleImportShared(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Files []library.ImportFile `json:"files"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBody)).Decode(&in); err != nil || len(in.Files) == 0 {
		writeError(w, http.StatusBadRequest, "choose one or more .setupman files")
		return
	}
	res := library.ImportResult{Items: []library.ImportItem{}}
	for _, f := range in.Files {
		it, err := s.lib.ImportShared(f.Data, "", "")
		if err != nil {
			it = library.ImportItem{Name: f.Name, Result: "skipped", Reason: err.Error()}
		}
		if it.Result == "imported" {
			res.Imported++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, it)
	}
	writeJSON(w, http.StatusOK, res)
}

// handleExport sends a ZIP of the newest version of each setup: /api/export?ids=1,2,3
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	for _, p := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	var buf bytes.Buffer
	if _, err := s.lib.Export(ids, &buf); err != nil {
		historyError(w, err)
		return
	}
	name := fmt.Sprintf("SetupMan setups %s.zip", time.Now().Format("2006-01-02 1504"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Files []library.ImportFile `json:"files"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBody)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "the files could not be read (at most 75 MB at once)")
		return
	}
	res, err := s.lib.Import(r.PathValue("game"), r.PathValue("car"), r.PathValue("track"), in.Files)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
