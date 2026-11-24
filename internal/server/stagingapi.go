package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"setupman/internal/folderbackup"
	"setupman/internal/history"
	"setupman/internal/library"
)

func (s *Server) registerStaging() {
	s.mux.HandleFunc("POST /api/games/{game}/manage", s.handleManage)
	s.mux.HandleFunc("GET /api/games/{game}/backups", s.handleBackups)
	s.mux.HandleFunc("POST /api/games/{game}/backups", s.handleCreateBackup)
	s.mux.HandleFunc("GET /api/games/{game}/backups/{id}/plan", s.handleRestorePlan)
	s.mux.HandleFunc("POST /api/games/{game}/backups/{id}/restore", s.handleRestore)
	s.mux.HandleFunc("POST /api/setups/{id}/stage", s.handleStage)
	s.mux.HandleFunc("POST /api/setups/{id}/unstage", s.handleUnstage)
	s.mux.HandleFunc("POST /api/setups/{id}/keep", s.handleKeep)
	s.mux.HandleFunc("POST /api/setups/stage-many", s.handleMany(true))
	s.mux.HandleFunc("POST /api/setups/unstage-many", s.handleMany(false))
	s.mux.HandleFunc("POST /api/games/{game}/cars/{car}/tracks/{track}/keep-only", s.handleKeepOnly)
	s.mux.HandleFunc("POST /api/setups/{id}/edit", s.handleEdit)
	s.mux.HandleFunc("POST /api/setups/{id}/rename", s.handleRename)
	s.mux.HandleFunc("POST /api/setups/rename-tidy", s.handleRenameTidy)
	s.mux.HandleFunc("GET /api/games/{game}/tidy-preview", s.handleTidyPreview)
	s.mux.HandleFunc("GET /api/games/{game}/telemetry-laps", s.handleTeleLapsPreview)
	s.mux.HandleFunc("POST /api/games/{game}/telemetry-laps", s.handleSetTeleLaps)
	s.mux.HandleFunc("POST /api/setups/delete", s.handleDelete)
	s.mux.HandleFunc("POST /api/setups/{id}/undelete", s.handleUndelete)
	s.mux.HandleFunc("GET /api/bin", s.handleBin)
	s.mux.HandleFunc("GET /api/games/{game}/duplicates", s.handleDuplicates)
	s.mux.HandleFunc("POST /api/setups/{id}/temperature", s.handleTemperature)
}

// handleTeleLapsPreview lists the setups in the folder whose telemetry laps aren't 99 (D-176).
func (s *Server) handleTeleLapsPreview(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.TelemetryLapsPreview(r.PathValue("game"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setups": list})
}

// handleSetTeleLaps sets telemetry laps to 99 in the chosen setups' files (D-176).
func (s *Server) handleSetTeleLaps(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	res, err := s.lib.SetTelemetryLaps(r.PathValue("game"), in.IDs)
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

// handleTemperature proposes changes for another air and track temperature (nothing is saved).
func (s *Server) handleTemperature(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	var in struct {
		Air   int `json:"air"`
		Track int `json:"track"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	p, err := s.lib.ProposeTemperature(id, in.Air, in.Track)
	if err != nil {
		if errors.Is(err, history.ErrNotFound) {
			historyError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDelete moves setups into the bin (D-102).
// handleMany shows (stage) or hides several setups at once: {"ids": [1, 2, 3]} (D-138).
func (s *Server) handleMany(stage bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.IDs) == 0 {
			writeError(w, http.StatusBadRequest, "choose one or more setups")
			return
		}
		run := s.lib.UnstageMany
		if stage {
			run = s.lib.StageMany
		}
		res, list, err := run(in.IDs)
		if err != nil {
			stagingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"queued": res.Queued, "gameRunning": res.GameRunning, "message": res.Message, "results": list})
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	res, err := s.lib.DeleteSetups(in.IDs)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

// handleUndelete takes a setup out of the bin.
func (s *Server) handleUndelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	name, err := s.lib.RestoreDeleted(id)
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// handleBin lists the deleted setups.
func (s *Server) handleBin(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.Bin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bin": list, "days": library.BinDays})
}

// handleRenameTidy gives several setups their suggested tidy name.
func (s *Server) handleRenameTidy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": s.lib.RenameTidy(in.IDs)})
}

// handleTidyPreview lists the new names "apply naming standard" would give a whole game.
func (s *Server) handleTidyPreview(w http.ResponseWriter, r *http.Request) {
	plan, err := s.lib.TidyPreview(r.PathValue("game"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"renames": plan})
}

// handleRename renames a setup (and its file when SetupMan manages the folder).
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	name, err := s.lib.RenameSetup(id, in.Name)
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// handleEdit changes setup values (spec D-85): a new version, or a new setup with newName.
func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	var in struct {
		Edits   map[string]string `json:"edits"`
		NewName string            `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	res, err := s.lib.EditSetup(id, in.Edits, in.NewName)
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func stagingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotManaged):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, folderbackup.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		historyError(w, err)
	}
}

func (s *Server) handleManage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	res, err := s.lib.SetManaged(r.PathValue("game"), in.On)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	if s.lib.Backups == nil {
		writeJSON(w, http.StatusOK, map[string]any{"backups": []any{}})
		return
	}
	list, err := s.lib.Backups.List(r.PathValue("game"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": list, "dir": s.lib.Backups.GameDir(r.PathValue("game"))})
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	b, err := s.lib.CreateBackup(r.PathValue("game"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleRestorePlan(w http.ResponseWriter, r *http.Request) {
	p, err := s.lib.PlanRestore(r.PathValue("game"), r.PathValue("id"))
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	res, err := s.lib.RestoreBackup(r.PathValue("game"), r.PathValue("id"))
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleStage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	var in struct {
		VersionID int64 `json:"versionId"` // 0 = latest
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	res, err := s.lib.Stage(id, in.VersionID)
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleUnstage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	res, err := s.lib.Unstage(id)
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleKeep(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, msgNoSetup)
		return
	}
	if err := s.lib.Keep(id); err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKeepOnly(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SetupIDs []int64 `json:"setupIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	res, err := s.lib.KeepOnly(r.PathValue("game"), r.PathValue("car"), r.PathValue("track"), in.SetupIDs)
	if err != nil {
		stagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleDuplicates lists duplicate sets (D-106): ?car=&track= for one track (else the whole game),
// &near=1 to include near-duplicates.
func (s *Server) handleDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	writeJSON(w, http.StatusOK, map[string]any{"sets": s.lib.Duplicates(r.PathValue("game"), q.Get("car"), q.Get("track"), q.Get("near") == "1")})
}
