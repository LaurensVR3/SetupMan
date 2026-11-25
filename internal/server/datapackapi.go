package server

// Data packs (spec D-201): create one (preview, then save and download), open one someone sent
// (inspect, then import), list and remove imported packs. Files being made or opened wait in a
// temporary folder for at most an hour.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"setupman/internal/datapack"
	"setupman/internal/library"
)

// maxPackUpload limits a pack someone opens (a pack is a few MB per lap).
const maxPackUpload = 2 << 30

const packTempLife = time.Hour

type packTemp struct {
	path, name string
	made       time.Time
}

// packTemps holds packs made for download and packs opened for import, by token.
type packTemps struct {
	mu    sync.Mutex
	files map[string]packTemp
}

func (p *packTemps) add(path, name string) string {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clean()
	p.files[tok] = packTemp{path: path, name: name, made: time.Now()}
	return tok
}

func (p *packTemps) take(tok string) (packTemp, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clean()
	t, ok := p.files[tok]
	return t, ok
}

func (p *packTemps) drop(tok string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.files[tok]; ok {
		os.Remove(t.path)
		delete(p.files, tok)
	}
}

// clean removes files older than an hour (called with mu held).
func (p *packTemps) clean() {
	for tok, t := range p.files {
		if time.Since(t.made) > packTempLife {
			os.Remove(t.path)
			delete(p.files, tok)
		}
	}
}

func packTempFile() (*os.File, error) {
	dir := filepath.Join(os.TempDir(), "SetupMan packs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "pack-*.zip")
}

func (s *Server) registerDataPacks() {
	s.packs = &packTemps{files: map[string]packTemp{}}
	s.mux.HandleFunc("POST /api/datapacks/preview", s.handlePackPreview)
	s.mux.HandleFunc("POST /api/datapacks", s.handlePackCreate)
	s.mux.HandleFunc("GET /api/datapacks/download/{token}", s.handlePackDownload)
	s.mux.HandleFunc("POST /api/datapacks/inspect", s.handlePackInspect)
	s.mux.HandleFunc("POST /api/datapacks/import", s.handlePackImport)
	s.mux.HandleFunc("GET /api/datapacks", s.handlePackList)
	s.mux.HandleFunc("GET /api/datapacks/best-laps/{game}", s.handleBestLaps)
	s.mux.HandleFunc("DELETE /api/datapacks/{id}", s.handlePackRemove)
}

func readPackRequest(w http.ResponseWriter, r *http.Request) (library.PackRequest, bool) {
	var req library.PackRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return req, false
	}
	return req, true
}

func (s *Server) handlePackPreview(w http.ResponseWriter, r *http.Request) {
	req, ok := readPackRequest(w, r)
	if !ok {
		return
	}
	p, err := s.lib.PreviewPack(req)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePackCreate makes the pack and returns a token to download it with.
func (s *Server) handlePackCreate(w http.ResponseWriter, r *http.Request) {
	req, ok := readPackRequest(w, r)
	if !ok {
		return
	}
	f, err := packTempFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name, err := s.lib.WritePack(req, f)
	size, _ := f.Seek(0, io.SeekCurrent)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		historyError(w, err)
		return
	}
	tok := s.packs.add(f.Name(), name)
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "name": name, "bytes": size})
}

func (s *Server) handlePackDownload(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	t, ok := s.packs.take(tok)
	if !ok {
		writeError(w, http.StatusNotFound, "This data pack has expired. Export it again.")
		return
	}
	f, err := os.Open(t.path)
	if err != nil {
		writeError(w, http.StatusNotFound, "This data pack has expired. Export it again.")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(t.name))
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	io.Copy(w, f)
}

// handlePackInspect takes an uploaded pack (the ZIP as the request body) and says what is in it.
func (s *Server) handlePackInspect(w http.ResponseWriter, r *http.Request) {
	f, err := packTempFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, maxPackUpload))
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		writeError(w, http.StatusBadRequest, "The file could not be received (at most 2 GB).")
		return
	}
	c, err := s.lib.InspectPack(f, n)
	f.Close()
	if err != nil {
		os.Remove(f.Name())
		status := http.StatusBadRequest
		if errors.Is(err, datapack.ErrNotPack) {
			status = http.StatusUnsupportedMediaType // the page then tries it as a ZIP of setups
		}
		writeError(w, status, err.Error())
		return
	}
	tok := s.packs.add(f.Name(), "")
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "pack": c})
}

func (s *Server) handlePackImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Stage bool   `json:"stage"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	t, ok := s.packs.take(in.Token)
	if !ok {
		writeError(w, http.StatusNotFound, "This data pack has expired. Import it again.")
		return
	}
	f, err := os.Open(t.path)
	if err != nil {
		writeError(w, http.StatusNotFound, "This data pack has expired. Import it again.")
		return
	}
	st, _ := f.Stat()
	res, err := s.lib.ImportPack(f, st.Size(), in.Stage)
	f.Close()
	if err != nil {
		historyError(w, err)
		return
	}
	s.packs.drop(in.Token)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePackList(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.Packs()
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handlePackRemove(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.RemovePack(r.PathValue("id"))
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleBestLaps lists the user's fastest lap of every car on every track (a best-laps pack).
func (s *Server) handleBestLaps(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.BestLaps(r.PathValue("game"))
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
