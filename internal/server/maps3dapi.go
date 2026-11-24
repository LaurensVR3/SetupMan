package server

// 3D telemetry API (spec D-190): a track's scene, a lap's path, offline photos.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"setupman/internal/library"
	"setupman/internal/maps3d"
	"setupman/internal/telemetry"
)

func (s *Server) registerMaps3D() {
	s.mux.HandleFunc("GET /api/telemetry/3d/{game}/{track}", s.handleScene3D)
	s.mux.HandleFunc("POST /api/telemetry/3d/{game}/{track}/photos", s.handlePhotos3D)
	s.mux.HandleFunc("DELETE /api/telemetry/3d/{game}/{track}/photos", s.handlePhotos3D)
	s.mux.HandleFunc("GET /api/telemetry/3d/{game}/{track}/photo/{z}/{x}/{y}", s.handlePhotoTile3D)
	s.mux.HandleFunc("GET /api/telemetry/files/{id}/laps/{n}/path", s.handleLapPath3D)
	s.mux.HandleFunc("GET /api/maps3d", s.handleMaps3D)
}

func scene3DError(w http.ResponseWriter, err error) {
	if errors.Is(err, library.ErrNo3D) {
		writeError(w, http.StatusNotFound, "SetupMan has no 3D map data for this track yet.")
		return
	}
	telemetryError(w, err)
}

// handleScene3D returns a track's scene, with the MapTiler key so the browser can load the photos
// from MapTiler itself.
func (s *Server) handleScene3D(w http.ResponseWriter, r *http.Request) {
	sc, err := s.lib.Track3D(r.PathValue("game"), r.PathValue("track"))
	if err != nil {
		scene3DError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*library.Scene3D
		MapTilerKey string `json:"mapTilerKey,omitempty"`
	}{sc, strings.TrimSpace(s.store.Load().MapTilerKey)})
}

func (s *Server) handleLapPath3D(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	n, okN := lapN(r)
	if !ok || !okN {
		writeError(w, http.StatusBadRequest, msgNoLap)
		return
	}
	p, err := s.lib.TelemetryLap3D(id, n)
	if err != nil {
		scene3DError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePhotos3D starts (POST) or deletes (DELETE) a track's offline photos.
func (s *Server) handlePhotos3D(w http.ResponseWriter, r *http.Request) {
	track := strings.ToLower(r.PathValue("track"))
	g, ok := telemetry.Geo(track)
	if !ok || s.lib.Maps == nil {
		writeError(w, http.StatusNotFound, "SetupMan has no 3D map data for this track yet.")
		return
	}
	var err error
	if r.Method == http.MethodDelete {
		err = s.lib.Maps.DeletePhotos(track)
	} else {
		err = s.lib.Maps.DownloadPhotos(track, g)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.lib.Maps.PhotoStatus(track, g))
}

func (s *Server) handlePhotoTile3D(w http.ResponseWriter, r *http.Request) {
	var t maps3d.Tile
	var err1, err2, err3 error
	t.Z, err1 = strconv.Atoi(r.PathValue("z"))
	t.X, err2 = strconv.Atoi(r.PathValue("x"))
	t.Y, err3 = strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".jpg"))
	if err1 != nil || err2 != nil || err3 != nil || s.lib.Maps == nil {
		http.NotFound(w, r)
		return
	}
	p := s.lib.Maps.PhotoTile(strings.ToLower(r.PathValue("track")), t)
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, p)
}

// handleMaps3D lists the tracks with offline photos kept and their size (Settings).
func (s *Server) handleMaps3D(w http.ResponseWriter, r *http.Request) {
	if s.lib.Maps == nil {
		writeJSON(w, http.StatusOK, map[string]int64{})
		return
	}
	writeJSON(w, http.StatusOK, s.lib.Maps.Kept())
}
