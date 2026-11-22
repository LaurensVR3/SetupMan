package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"setupman/internal/telemetry"
)

// InsertTelemetryLap stores one resampled telemetry lap and its compressed trace.
func (q Q) InsertTelemetryLap(lap *telemetry.LapMeta, traceGz []byte) error {
	if lap.RecordedAt.IsZero() {
		lap.RecordedAt = time.Now()
	}
	statsBytes, err := json.Marshal(lap.Stats)
	if err != nil {
		return err
	}
	res, err := q.ex.Exec(`INSERT INTO telemetry_laps
		(game, car, track, source, source_file, label, lap_ms, lap_number, valid, is_reference,
		 track_length_m, session_id, setup_id, recorded_at, has_coords, stats_json, trace_gz)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lap.Game, lap.Car, strings.ToLower(lap.Track), lap.Source, lap.SourceFile, lap.Label,
		lap.LapMs, lap.LapNumber, btoi(lap.Valid), btoi(lap.IsReference),
		lap.TrackLengthM, lap.SessionID, lap.SetupID, ms(lap.RecordedAt), btoi(lap.HasCoords),
		string(statsBytes), traceGz)
	if err != nil {
		return err
	}
	lap.ID, err = res.LastInsertId()
	return err
}

// ListTelemetryLaps returns stored telemetry lap metadata (newest first), optionally filtered.
func (q Q) ListTelemetryLaps(game, car, track string) ([]telemetry.LapMeta, error) {
	var conds []string
	var args []any
	if game != "" {
		conds = append(conds, "t.game = ?")
		args = append(args, game)
	}
	if car != "" {
		conds = append(conds, "t.car = ?")
		args = append(args, car)
	}
	if track != "" {
		conds = append(conds, "t.track = ?")
		args = append(args, strings.ToLower(track))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	rows, err := q.ex.Query(`SELECT t.id, t.game, t.car, t.track, t.source, t.source_file, t.label,
		t.lap_ms, t.lap_number, t.valid, t.is_reference, t.track_length_m, t.session_id, t.setup_id,
		COALESCE(s.name, ''), t.recorded_at, t.has_coords, t.stats_json
		FROM telemetry_laps t
		LEFT JOIN setups s ON s.id = t.setup_id
		`+where+`
		ORDER BY t.is_reference DESC, t.recorded_at DESC, t.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []telemetry.LapMeta{}
	for rows.Next() {
		var m telemetry.LapMeta
		var valid, isRef, hasCoords int
		var at int64
		var statsJSON string
		if err := rows.Scan(&m.ID, &m.Game, &m.Car, &m.Track, &m.Source, &m.SourceFile, &m.Label,
			&m.LapMs, &m.LapNumber, &valid, &isRef, &m.TrackLengthM, &m.SessionID, &m.SetupID,
			&m.SetupName, &at, &hasCoords, &statsJSON); err != nil {
			return nil, err
		}
		m.Valid = valid != 0
		m.IsReference = isRef != 0
		m.HasCoords = hasCoords != 0
		m.RecordedAt = fromMS(at)
		_ = json.Unmarshal([]byte(statsJSON), &m.Stats)
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetTelemetryLap returns one lap's metadata and compressed trace blob (nil if not found).
func (q Q) GetTelemetryLap(id int64) (*telemetry.LapMeta, []byte, error) {
	var m telemetry.LapMeta
	var valid, isRef, hasCoords int
	var at int64
	var statsJSON string
	var traceGz []byte
	err := q.ex.QueryRow(`SELECT t.id, t.game, t.car, t.track, t.source, t.source_file, t.label,
		t.lap_ms, t.lap_number, t.valid, t.is_reference, t.track_length_m, t.session_id, t.setup_id,
		COALESCE(s.name, ''), t.recorded_at, t.has_coords, t.stats_json, t.trace_gz
		FROM telemetry_laps t
		LEFT JOIN setups s ON s.id = t.setup_id
		WHERE t.id = ?`, id).Scan(&m.ID, &m.Game, &m.Car, &m.Track, &m.Source, &m.SourceFile, &m.Label,
		&m.LapMs, &m.LapNumber, &valid, &isRef, &m.TrackLengthM, &m.SessionID, &m.SetupID,
		&m.SetupName, &at, &hasCoords, &statsJSON, &traceGz)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	m.Valid = valid != 0
	m.IsReference = isRef != 0
	m.HasCoords = hasCoords != 0
	m.RecordedAt = fromMS(at)
	_ = json.Unmarshal([]byte(statsJSON), &m.Stats)
	return &m, traceGz, nil
}

// SetTelemetryReference marks lap id as the reference lap for its (game, track), clearing the flag on siblings.
func (s *Store) SetTelemetryReference(id int64) error {
	return s.Tx(func(q Q) error {
		var game, track string
		err := q.ex.QueryRow(`SELECT game, track FROM telemetry_laps WHERE id = ?`, id).Scan(&game, &track)
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		if err != nil {
			return err
		}
		if _, err := q.ex.Exec(`UPDATE telemetry_laps SET is_reference = 0 WHERE game = ? AND track = ?`, game, track); err != nil {
			return err
		}
		_, err = q.ex.Exec(`UPDATE telemetry_laps SET is_reference = 1 WHERE id = ?`, id)
		return err
	})
}

// RenameTelemetryLap updates the user label of a telemetry lap.
func (q Q) RenameTelemetryLap(id int64, label string) error {
	res, err := q.ex.Exec(`UPDATE telemetry_laps SET label = ? WHERE id = ?`, strings.TrimSpace(label), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTelemetryLap deletes one telemetry lap by id.
func (q Q) DeleteTelemetryLap(id int64) error {
	_, err := q.ex.Exec(`DELETE FROM telemetry_laps WHERE id = ?`, id)
	return err
}

// DeleteSampleTelemetryLaps removes previously loaded sample laps for a track so reloading doesn't duplicate them.
func (q Q) DeleteSampleTelemetryLaps(track string) error {
	_, err := q.ex.Exec(`DELETE FROM telemetry_laps WHERE source = ? AND track = ?`, telemetry.SourceSample, strings.ToLower(track))
	return err
}

// HasTelemetryReference reports whether a reference lap already exists for (game, track).
func (q Q) HasTelemetryReference(game, track string) (bool, error) {
	var n int
	err := q.ex.QueryRow(`SELECT COUNT(*) FROM telemetry_laps WHERE game = ? AND track = ? AND is_reference = 1`, game, strings.ToLower(track)).Scan(&n)
	return n > 0, err
}

// TelemetrySourceFiles returns the set of already-imported MoTeC base filenames.
func (q Q) TelemetrySourceFiles() (map[string]bool, error) {
	rows, err := q.ex.Query(`SELECT DISTINCT source_file FROM telemetry_laps WHERE source_file != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = true
	}
	return out, rows.Err()
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}
