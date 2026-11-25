package store

// Telemetry metadata (spec D-159, D-160): which of the game's telemetry files (ACC's MoTeC logs)
// SetupMan has indexed, their laps, and how they link to recorded sessions, laps and setups. The
// files themselves stay where the game wrote them and are never changed; only this metadata lives
// in the library (and so in full backups).

import (
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

// Match kinds of a telemetry file.
const (
	TeleNone    = "none"    // no session and no setup known
	TeleSession = "session" // linked to a session SetupMan recorded; the setup is the session's
	TeleManual  = "manual"  // setup set by the user (files without a recorded session)
)

// Telemetry tables (D-159 … D-167). ensureTelemetry runs at every start: it creates the tables
// and adds any column a library from an earlier build lacks, so no library can end up with a
// half-made schema (a 0.24.0 test build once made one without "turns", D-167).
var teleColumns = []struct{ table, name, def string }{
	{"telemetry_files", "id", "INTEGER PRIMARY KEY"},
	{"telemetry_files", "game", "TEXT NOT NULL DEFAULT ''"},
	{"telemetry_files", "name", "TEXT NOT NULL DEFAULT '' COLLATE NOCASE"},
	{"telemetry_files", "size", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_files", "mtime", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_files", "car", "TEXT NOT NULL DEFAULT '' COLLATE NOCASE"},
	{"telemetry_files", "track", "TEXT NOT NULL DEFAULT '' COLLATE NOCASE"},
	{"telemetry_files", "started_at", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_files", "duration_ms", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_files", "channels", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_files", "session_id", "INTEGER REFERENCES sessions(id) ON DELETE SET NULL"},
	{"telemetry_files", "setup_id", "INTEGER"},
	{"telemetry_files", "version_id", "INTEGER"},
	{"telemetry_files", "match", "TEXT NOT NULL DEFAULT 'none'"},
	{"telemetry_files", "note", "TEXT NOT NULL DEFAULT ''"},
	{"telemetry_files", "missing", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_files", "superseded", "INTEGER NOT NULL DEFAULT 0"}, // kept_by > 0 (older code read this)
	{"telemetry_files", "kept_by", "INTEGER NOT NULL DEFAULT 0"},    // another save of the session is the one shown (RefreshTelemetrySaves)
	{"telemetry_laps", "file_id", "INTEGER NOT NULL DEFAULT 0 REFERENCES telemetry_files(id) ON DELETE CASCADE"},
	{"telemetry_laps", "n", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_laps", "start_ms", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_laps", "lap_ms", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_laps", "complete", "INTEGER NOT NULL DEFAULT 0"}, // the lap clock restarted at both ends
	{"telemetry_laps", "full", "INTEGER NOT NULL DEFAULT 0"},     // and it is a normal lap of the track (RefreshTelemetryFull)
	{"telemetry_laps", "dist_m", "REAL NOT NULL DEFAULT 0"},
	{"telemetry_laps", "turns", "REAL NOT NULL DEFAULT 0"}, // how far the car turned (yaw rate summed): ±1 on a normal track
	{"telemetry_laps", "profile", "BLOB"},                  // lowest speed in each 1 % of the lap (km/h ÷ 2), to spot cut chicanes
	{"telemetry_laps", "lap_id", "INTEGER REFERENCES laps(id) ON DELETE SET NULL"},
	{"telemetry_laps", "reference", "INTEGER NOT NULL DEFAULT 0"},
	{"telemetry_laps", "note", "TEXT NOT NULL DEFAULT ''"},
	{"telemetry_laps", "sess_file", "INTEGER NOT NULL DEFAULT 0"}, // the file the session is shown as (RefreshTelemetrySaves)
	{"telemetry_laps", "sess_n", "INTEGER NOT NULL DEFAULT 0"},    // the lap's number in that session
	{"telemetry_laps", "dup", "INTEGER NOT NULL DEFAULT 0"},       // the same lap is shown from another save
}

// ensureTelemetry reports added = true when it had to add a table or column to a library that
// already had telemetry (then everything must be indexed again).
func ensureTelemetry(db *sql.DB) (added bool, err error) {
	var had int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('telemetry_files', 'telemetry_laps')`).Scan(&had); err != nil {
		return false, err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS telemetry_files (
	id          INTEGER PRIMARY KEY,
	game        TEXT NOT NULL,
	name        TEXT NOT NULL COLLATE NOCASE,
	size        INTEGER NOT NULL,
	mtime       INTEGER NOT NULL,
	car         TEXT NOT NULL COLLATE NOCASE,
	track       TEXT NOT NULL COLLATE NOCASE,
	started_at  INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	channels    INTEGER NOT NULL,
	UNIQUE (game, name)
);
CREATE TABLE IF NOT EXISTS telemetry_laps (
	file_id   INTEGER NOT NULL REFERENCES telemetry_files(id) ON DELETE CASCADE,
	n         INTEGER NOT NULL,
	start_ms  INTEGER NOT NULL,
	lap_ms    INTEGER NOT NULL,
	complete  INTEGER NOT NULL,
	PRIMARY KEY (file_id, n)
);`); err != nil {
		return false, err
	}
	added = had > 0 && had < 2 // one of the two tables was missing
	for _, c := range teleColumns {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.name).Scan(&n); err != nil {
			return false, err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.name + ` ` + c.def); err != nil {
				return false, err
			}
			added = added || had == 2
		}
	}
	_, err = db.Exec(`
CREATE INDEX IF NOT EXISTS telemetry_files_track ON telemetry_files(game, car, track);
CREATE INDEX IF NOT EXISTS telemetry_files_session ON telemetry_files(session_id);
CREATE INDEX IF NOT EXISTS telemetry_laps_session ON telemetry_laps(sess_file);`)
	return added, err
}

// telemetryReindex has every telemetry file indexed again (library version 12, D-167 … D-169, or
// after ensureTelemetry repaired the schema): laps used to be cut from the wrong part of most
// channels where ACC kept only its last laps, laps carry a speed profile, and saves are put
// together per lap. Notes, reference laps and links are kept; outlines and turns are made again.
func telemetryReindex(db *sql.DB) error {
	_, err := db.Exec(`
UPDATE telemetry_files SET size = -1;
UPDATE telemetry_laps SET full = 0;
DELETE FROM kv WHERE key LIKE 'tele-outline/%' OR key LIKE 'tele-turns/%';`)
	return err
}

// ErrTeleNotFound: the telemetry file or lap does not exist.
var ErrTeleNotFound = errors.New("telemetry file or lap not found")

// TeleFile is one indexed telemetry file. A file another save of the same session is shown as
// (KeptBy ≠ 0) takes its session details (start, link, setup, note) from that save.
type TeleFile struct {
	ID         int64     `json:"id"`
	Game       string    `json:"game"`
	Name       string    `json:"name"` // file name in the game's telemetry folder
	Size       int64     `json:"size"`
	Modified   time.Time `json:"-"`
	Car        string    `json:"car"`
	Track      string    `json:"track"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Channels   int       `json:"channels"`
	SessionID  int64     `json:"sessionId"`
	Kind       string    `json:"kind"`      // the session's kind ("" when not linked)
	SetupID    int64     `json:"setupId"`   // the setup driven: the session's, or set by hand
	VersionID  int64     `json:"versionId"` //
	SetupName  string    `json:"setupName,omitempty"`
	Match      string    `json:"match"`
	Note       string    `json:"note"`
	Missing    bool      `json:"missing,omitempty"` // no longer in the folder
	Laps       int       `json:"laps"`              // full laps of the session
	BestMs     int       `json:"bestMs"`            // best full lap that was not invalid (0 = none)
	KeptBy     int64     `json:"keptBy,omitempty"`  // the save of the same session that is shown
	Ext        string    `json:"ext,omitempty"`     // sender's initials of a file from a data pack (D-201)
	Pack       string    `json:"-"`                 // that pack (its files are in SetupMan's own folder)
}

// TeleLap is one lap of a telemetry file, with what SetupMan recorded about it when linked.
// FileID and N say where the lap is (its file may be an earlier save of the session, D-167);
// Lap is its number in the session as shown.
type TeleLap struct {
	FileID    int64   `json:"fileId"`
	N         int     `json:"n"`
	Lap       int     `json:"lap"`
	StartMs   int64   `json:"startMs"`
	LapMs     int     `json:"lapMs"`
	Complete  bool    `json:"complete"` // a full lap (RefreshTelemetryFull)
	Timed     bool    `json:"-"`        // the lap clock restarted at both ends (pit and data-less laps too)
	DistM     float64 `json:"distM"`    // metres driven in the lap (speed integrated); < 100: no data
	Turns     float64 `json:"-"`        // turns the car made in the lap (yaw rate summed), -1 clockwise
	Profile   []byte  `json:"-"`        // lowest speed in each 1 % of the lap, km/h ÷ 2 (nil: no data)
	LapID     int64   `json:"lapId"`
	Reference bool    `json:"reference"`
	Note      string  `json:"note"`
	// From the recorded lap (only when LapID ≠ 0)
	Valid    *bool   `json:"valid,omitempty"`
	Pit      bool    `json:"pit,omitempty"`
	FuelUsed float64 `json:"fuelUsed,omitempty"`
	Air      float64 `json:"air,omitempty"`
	Road     float64 `json:"road,omitempty"`
	HasTemps bool    `json:"hasTemps,omitempty"`
	// SessionID: the recorded session of the linked lap (0 = not linked), D-188
	SessionID int64 `json:"sessionId,omitempty"`
	// LapSetupID: the setup the linked lap was driven with (setup per stint, D-177); 0 = not known.
	// LapSetupName is filled in by the library where it shows a session's laps.
	LapSetupID   int64  `json:"lapSetupId,omitempty"`
	LapSetupName string `json:"lapSetupName,omitempty"`
	// Ext: the sender's initials of a lap from a data pack (D-201); never counted as the user's own
	Ext string `json:"ext,omitempty"`
}

// TeleKnown is what the indexer needs to know about a file it indexed before.
type TeleKnown struct {
	ID       int64
	Size     int64
	Modified time.Time
	Missing  bool
}

// TelemetryKnown returns the files indexed for a game, by lower-case name.
func (q Q) TelemetryKnown(game string) (map[string]TeleKnown, error) {
	rows, err := q.ex.Query(`SELECT id, name, size, mtime, missing FROM telemetry_files WHERE game = ? AND ext = ''`, game) // not data-pack files (D-201)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TeleKnown{}
	for rows.Next() {
		var k TeleKnown
		var name string
		var mt int64
		if err := rows.Scan(&k.ID, &name, &k.Size, &mt, &k.Missing); err != nil {
			return nil, err
		}
		k.Modified = fromMS(mt)
		out[strings.ToLower(name)] = k
	}
	return out, rows.Err()
}

// SaveTelemetryFile stores a newly indexed (or changed) file with its laps. Notes and reference
// marks of laps that are still there are kept, as are the file's note and links.
func (s *Store) SaveTelemetryFile(f *TeleFile, laps []TeleLap) error {
	return s.Tx(func(q Q) error {
		err := q.ex.QueryRow(`SELECT id FROM telemetry_files WHERE game = ? AND name = ?`, f.Game, f.Name).Scan(&f.ID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			res, err := q.ex.Exec(`INSERT INTO telemetry_files (game, name, size, mtime, car, track, started_at, duration_ms, channels) VALUES (?,?,?,?,?,?,?,?,?)`,
				f.Game, f.Name, f.Size, ms(f.Modified), f.Car, f.Track, ms(f.StartedAt), f.DurationMs, f.Channels)
			if err != nil {
				return err
			}
			if f.ID, err = res.LastInsertId(); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			// the laps are stored again below without their links to recorded laps: link the file again
			// (a setup set by hand stays)
			if _, err := q.ex.Exec(`UPDATE telemetry_files SET size=?, mtime=?, car=?, track=?, started_at=?, duration_ms=?, channels=?, missing=0,
				session_id = NULL, match = CASE WHEN match = 'session' THEN 'none' ELSE match END WHERE id=?`,
				f.Size, ms(f.Modified), f.Car, f.Track, ms(f.StartedAt), f.DurationMs, f.Channels, f.ID); err != nil {
				return err
			}
		}
		keep := map[int][2]any{}
		rows, err := q.ex.Query(`SELECT n, reference, note FROM telemetry_laps WHERE file_id = ?`, f.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n int
			var ref bool
			var note string
			if err := rows.Scan(&n, &ref, &note); err != nil {
				rows.Close()
				return err
			}
			keep[n] = [2]any{ref, note}
		}
		rows.Close()
		if _, err := q.ex.Exec(`DELETE FROM telemetry_laps WHERE file_id = ?`, f.ID); err != nil {
			return err
		}
		for _, l := range laps {
			ref, note := any(false), any("")
			if k, ok := keep[l.N]; ok {
				ref, note = k[0], k[1]
			}
			if _, err := q.ex.Exec(`INSERT INTO telemetry_laps (file_id, n, start_ms, lap_ms, complete, full, dist_m, turns, profile, reference, note, sess_file, sess_n, dup)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0)`,
				f.ID, l.N, l.StartMs, l.LapMs, l.Complete, l.Complete, l.DistM, l.Turns, l.Profile, ref, note, f.ID, l.N); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetTelemetryMissing marks a file as gone from (or back in) the folder. Its metadata is kept, so
// notes come back if the file does.
func (q Q) SetTelemetryMissing(id int64, missing bool) error {
	_, err := q.ex.Exec(`UPDATE telemetry_files SET missing = ? WHERE id = ?`, missing, id)
	return err
}

// teleSelect reads files with the session details of the save they are shown as (k).
const teleSelect = `SELECT f.id, f.game, f.name, f.size, f.mtime, f.car, f.track, k.started_at, f.duration_ms, f.channels,
	COALESCE(k.session_id, 0), COALESCE(s.kind, ''),
	CASE WHEN k.match = 'manual' THEN COALESCE(k.setup_id, 0) ELSE COALESCE(s.setup_id, 0) END,
	CASE WHEN k.match = 'manual' THEN COALESCE(k.version_id, 0) ELSE COALESCE(s.version_id, 0) END,
	k.match, k.note, f.missing,
	(SELECT COUNT(*) FROM telemetry_laps t WHERE t.sess_file = k.id AND t.dup = 0 AND t.full = 1),
	COALESCE((SELECT MIN(t.lap_ms) FROM telemetry_laps t LEFT JOIN laps l ON l.id = t.lap_id
		WHERE t.sess_file = k.id AND t.dup = 0 AND t.full = 1 AND COALESCE(l.valid, 1) = 1), 0),
	f.kept_by, f.ext, f.pack
	FROM telemetry_files f JOIN telemetry_files k ON k.id = CASE WHEN f.kept_by > 0 THEN f.kept_by ELSE f.id END
	LEFT JOIN sessions s ON s.id = k.session_id `

func scanTele(rows *sql.Rows) ([]TeleFile, error) {
	defer rows.Close()
	out := []TeleFile{}
	for rows.Next() {
		var f TeleFile
		var mt, st int64
		if err := rows.Scan(&f.ID, &f.Game, &f.Name, &f.Size, &mt, &f.Car, &f.Track, &st, &f.DurationMs, &f.Channels,
			&f.SessionID, &f.Kind, &f.SetupID, &f.VersionID, &f.Match, &f.Note, &f.Missing, &f.Laps, &f.BestMs, &f.KeptBy, &f.Ext, &f.Pack); err != nil {
			return nil, err
		}
		f.Modified, f.StartedAt = fromMS(mt), fromMS(st)
		out = append(out, f)
	}
	return out, rows.Err()
}

// TelemetryFiles returns the sessions of a game that are in the folder (one file each), newest
// first. car and track narrow the list when not "".
func (q Q) TelemetryFiles(game, car, track string) ([]TeleFile, error) {
	where := `WHERE f.game = ? AND f.missing = 0 AND f.kept_by = 0`
	args := []any{game}
	if car != "" {
		where += ` AND f.car = ?`
		args = append(args, car)
	}
	if track != "" {
		where += ` AND f.track = ?`
		args = append(args, track)
	}
	rows, err := q.ex.Query(teleSelect+where+` ORDER BY f.started_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	return scanTele(rows)
}

// TelemetryForSetup returns the sessions driven with a setup, newest first.
func (q Q) TelemetryForSetup(setupID int64) ([]TeleFile, error) {
	rows, err := q.ex.Query(teleSelect+`WHERE f.missing = 0 AND f.kept_by = 0 AND
		((f.match = 'manual' AND f.setup_id = ?) OR (f.match <> 'manual' AND s.setup_id = ?)) ORDER BY f.started_at DESC`, setupID, setupID)
	if err != nil {
		return nil, err
	}
	return scanTele(rows)
}

// TelemetryForSession returns the telemetry of a recorded session.
func (q Q) TelemetryForSession(sessionID int64) ([]TeleFile, error) {
	rows, err := q.ex.Query(teleSelect+`WHERE f.missing = 0 AND f.kept_by = 0 AND f.session_id = ? ORDER BY f.started_at`, sessionID)
	if err != nil {
		return nil, err
	}
	return scanTele(rows)
}

// TelemetryFile returns one file, or nil.
func (q Q) TelemetryFile(id int64) (*TeleFile, error) {
	rows, err := q.ex.Query(teleSelect+`WHERE f.id = ?`, id)
	if err != nil {
		return nil, err
	}
	list, err := scanTele(rows)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// lapSelect reads laps; the recorded lap comes from the lap itself or, when it is shown from an
// earlier save, from the same lap of the save the session is shown as (kt).
const lapSelect = `SELECT t.file_id, t.n, t.sess_n, t.start_ms, t.lap_ms, t.full, t.complete, t.dist_m, t.turns, t.profile,
	COALESCE(t.lap_id, kt.lap_id, 0), t.reference, t.note,
	l.valid, COALESCE(l.pit, 0), COALESCE(l.fuel_used, 0), l.air, l.road, COALESCE(l.session_id, 0), COALESCE(l.setup_id, 0),
	(SELECT ext FROM telemetry_files WHERE id = t.file_id)`

const lapFrom = ` FROM telemetry_laps t
	LEFT JOIN telemetry_laps kt ON kt.file_id = t.sess_file AND kt.sess_n = t.sess_n AND kt.file_id <> t.file_id
	LEFT JOIN laps l ON l.id = COALESCE(t.lap_id, kt.lap_id) `

func scanLap(rows *sql.Rows, l *TeleLap, more ...any) error {
	var valid sql.NullBool
	var air, road sql.NullFloat64
	dst := append([]any{&l.FileID, &l.N, &l.Lap, &l.StartMs, &l.LapMs, &l.Complete, &l.Timed, &l.DistM, &l.Turns, &l.Profile,
		&l.LapID, &l.Reference, &l.Note, &valid, &l.Pit, &l.FuelUsed, &air, &road, &l.SessionID, &l.LapSetupID, &l.Ext}, more...)
	if err := rows.Scan(dst...); err != nil {
		return err
	}
	if valid.Valid {
		v := valid.Bool
		l.Valid = &v
	}
	if air.Valid && road.Valid {
		l.Air, l.Road, l.HasTemps = air.Float64, road.Float64, true
	}
	return nil
}

func scanLaps(rows *sql.Rows) ([]TeleLap, error) {
	defer rows.Close()
	out := []TeleLap{}
	for rows.Next() {
		var l TeleLap
		if err := scanLap(rows, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// TelemetryLaps returns the laps of one file (its own, whether shown or not).
func (q Q) TelemetryLaps(fileID int64) ([]TeleLap, error) {
	rows, err := q.ex.Query(lapSelect+lapFrom+`WHERE t.file_id = ? ORDER BY t.n`, fileID)
	if err != nil {
		return nil, err
	}
	return scanLaps(rows)
}

// TelemetrySessionLaps returns the laps of the session a file is shown as: its own and those only
// earlier saves still have, each once, in session order. For a file shown as another save it
// returns its own laps.
func (q Q) TelemetrySessionLaps(fileID int64) ([]TeleLap, error) {
	rows, err := q.ex.Query(lapSelect+lapFrom+`WHERE CASE WHEN (SELECT kept_by FROM telemetry_files WHERE id = ?1) > 0
		THEN t.file_id = ?1 ELSE t.sess_file = ?1 AND t.dup = 0 END ORDER BY t.sess_n, t.n`, fileID)
	if err != nil {
		return nil, err
	}
	return scanLaps(rows)
}

// TelemetryUnlinked returns the files not yet linked to a recorded session or setup: every save,
// also earlier saves of a session (laps only they have need their recorded laps too, D-169).
func (q Q) TelemetryUnlinked(game string) ([]TeleFile, error) {
	// with the file's own start: recorded laps are matched by their times in this file
	own := strings.Replace(teleSelect, "f.track, k.started_at,", "f.track, f.started_at,", 1)
	rows, err := q.ex.Query(own+`WHERE f.game = ? AND f.match = 'none' AND f.missing = 0 AND f.ext = ''`, game)
	if err != nil {
		return nil, err
	}
	return scanTele(rows)
}

// TeleLapNeed is a telemetry lap whose recorded lap has no tyre temperatures yet (D-189).
type TeleLapNeed struct {
	FileID int64
	N      int
	LapID  int64
}

// TeleLapsWithoutTyreTemps lists full telemetry laps linked to a recorded lap without tyre
// temperatures, newest files first.
func (q Q) TeleLapsWithoutTyreTemps(game string) ([]TeleLapNeed, error) {
	rows, err := q.ex.Query(`SELECT t.file_id, t.n, t.lap_id FROM telemetry_laps t JOIN laps l ON l.id = t.lap_id
		JOIN telemetry_files f ON f.id = t.file_id WHERE f.game = ? AND f.missing = 0 AND t.full = 1 AND l.t_fl IS NULL
		ORDER BY f.started_at DESC, t.n`, game)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeleLapNeed
	for rows.Next() {
		var n TeleLapNeed
		if err := rows.Scan(&n.FileID, &n.N, &n.LapID); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// TelemetryLinked returns the files linked to a recorded session (to link the laps of other
// sessions in the same file once, D-179).
func (q Q) TelemetryLinked(game string) ([]TeleFile, error) {
	own := strings.Replace(teleSelect, "f.track, k.started_at,", "f.track, f.started_at,", 1)
	rows, err := q.ex.Query(own+`WHERE f.game = ? AND f.match = 'session' AND f.missing = 0 AND f.ext = ''`, game)
	if err != nil {
		return nil, err
	}
	return scanTele(rows)
}

// SessionsBetween returns the recorded sessions on a car and track that started between from and to.
func (q Q) SessionsBetween(game, car, track string, from, to time.Time) ([]Session, error) {
	rows, err := q.ex.Query(`SELECT id, game, car, track, kind, started_at, COALESCE(setup_id,0), COALESCE(version_id,0), match
		FROM sessions WHERE game = ? AND car = ? AND track = ? AND started_at BETWEEN ? AND ? AND ext = '' ORDER BY started_at`,
		game, car, track, ms(from), ms(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		var at int64
		if err := rows.Scan(&s.ID, &s.Game, &s.Car, &s.Track, &s.Kind, &at, &s.SetupID, &s.VersionID, &s.Match); err != nil {
			return nil, err
		}
		s.StartedAt = fromMS(at)
		out = append(out, s)
	}
	return out, rows.Err()
}

// LinkTelemetry links a file to a recorded session and its laps to recorded laps (lap number →
// recorded lap id).
func (s *Store) LinkTelemetry(fileID, sessionID int64, laps map[int]int64) error {
	return s.Tx(func(q Q) error {
		if _, err := q.ex.Exec(`UPDATE telemetry_files SET session_id = ?, match = ? WHERE id = ?`, sessionID, TeleSession, fileID); err != nil {
			return err
		}
		for n, id := range laps {
			if _, err := q.ex.Exec(`UPDATE telemetry_laps SET lap_id = ? WHERE file_id = ? AND n = ?`, id, fileID, n); err != nil {
				return err
			}
		}
		return nil
	})
}

// changed turns "no row updated" into ErrTeleNotFound.
func changed(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrTeleNotFound
	}
	return nil
}

// SetTelemetrySetup sets the setup of a file without a recorded session (0 = not known).
func (q Q) SetTelemetrySetup(fileID, setupID, versionID int64) error {
	match := TeleManual
	if setupID == 0 {
		match = TeleNone
	}
	_, err := q.ex.Exec(`UPDATE telemetry_files SET setup_id = ?, version_id = ?, match = ? WHERE id = ? AND session_id IS NULL`,
		nullID(setupID), nullID(versionID), match, fileID)
	return err
}

// SetTelemetryNote saves the note of a file.
func (q Q) SetTelemetryNote(fileID int64, note string) error {
	return changed(q.ex.Exec(`UPDATE telemetry_files SET note = ? WHERE id = ?`, note, fileID))
}

// SetTelemetryLapNote saves the note of one lap.
func (q Q) SetTelemetryLapNote(fileID int64, n int, note string) error {
	return changed(q.ex.Exec(`UPDATE telemetry_laps SET note = ? WHERE file_id = ? AND n = ?`, note, fileID, n))
}

// SetTelemetryReference makes a lap the reference lap of its car and track (one per car and
// track), or clears it. Only a full lap, as shown (not a copy in another save), can be the reference.
func (s *Store) SetTelemetryReference(fileID int64, n int, on bool) error {
	return s.Tx(func(q Q) error {
		var full, dup bool
		err := q.ex.QueryRow(`SELECT full, dup FROM telemetry_laps WHERE file_id = ? AND n = ?`, fileID, n).Scan(&full, &dup)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && on && (!full || dup)) {
			return ErrTeleNotFound
		}
		if err != nil {
			return err
		}
		if on {
			if _, err := q.ex.Exec(`UPDATE telemetry_laps SET reference = 0 WHERE reference = 1 AND file_id IN
				(SELECT o.id FROM telemetry_files o JOIN telemetry_files f ON f.id = ? WHERE o.game = f.game AND o.car = f.car AND o.track = f.track)`, fileID); err != nil {
				return err
			}
		}
		_, err = q.ex.Exec(`UPDATE telemetry_laps SET reference = ? WHERE file_id = ? AND n = ?`, on, fileID, n)
		return err
	})
}

// TelemetryReference returns the reference lap of a car and track (ok = false when none).
func (q Q) TelemetryReference(game, car, track string) (fileID int64, n int, ok bool, err error) {
	err = q.ex.QueryRow(`SELECT t.file_id, t.n FROM telemetry_laps t JOIN telemetry_files f ON f.id = t.file_id
		WHERE t.reference = 1 AND t.dup = 0 AND f.missing = 0 AND f.game = ? AND f.car = ? AND f.track = ? LIMIT 1`, game, car, track).Scan(&fileID, &n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	return fileID, n, err == nil, err
}

// Full-lap rule (D-163, D-167), checked on the user's 2,135 files: about two thirds of all laps on
// a track are within 1 % of its usual length. ACC also restarts its lap clock when the car is sent
// back to the pits, a session restarts or the car cuts the track, which gave 16-second "laps" and
// fake fastest laps (Spa 2:12.2 where the real best is 2:15.8). Such laps are too short or turn the
// wrong amount (Spa: 0.72 turns instead of 1). A lap that cuts a chicane is barely shorter, but
// goes through it far faster than anyone (Spa: 137 km/h at the Bus Stop where laps slow to 61–66).
const (
	FullLapLength = 0.03 // a full lap is within 3 % of the track's usual length
	FullLapTurns  = 0.05 // and turns within 0.05 of the track's usual amount (±1, or 0 for a figure of eight)
	FullLapSlow   = 150  // at the track's slow points (usually below this, km/h) …
	FullLapCut    = 40   // … a full lap is no more than this much faster than usual (km/h)
	ProfileBins   = 100  // TeleLap.Profile: lowest speed in each 1 % of the lap
)

// RefreshTelemetryFull decides which laps of a game are full laps: the lap clock restarted at both
// ends, the length is within FullLapLength of the track's usual length, the car turned within
// FullLapTurns of the usual amount, and it went through the track's slow points no more than
// FullLapCut faster than usual (the middle values of the laps on that track, each lap counted
// once). Laps through the pit lane stay in; resets, cuts, spins and teleports drop out.
func (s *Store) RefreshTelemetryFull(game string) error {
	type lap struct {
		file        int64
		n           int
		dist, turns float64
		track       string
		profile     []byte
		dup         bool
	}
	rows, err := s.ex.Query(`SELECT t.file_id, t.n, t.dist_m, t.turns, lower(f.track), t.profile, t.dup FROM telemetry_laps t JOIN telemetry_files f ON f.id = t.file_id
		WHERE f.game = ? AND t.complete = 1`, game)
	if err != nil {
		return err
	}
	var laps []lap
	dists, turns := map[string][]float64{}, map[string][]float64{}
	for rows.Next() {
		var l lap
		if err := rows.Scan(&l.file, &l.n, &l.dist, &l.turns, &l.track, &l.profile, &l.dup); err != nil {
			rows.Close()
			return err
		}
		laps = append(laps, l)
		if l.dist > 300 && !l.dup {
			dists[l.track] = append(dists[l.track], l.dist)
			turns[l.track] = append(turns[l.track], l.turns)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	middle := func(v []float64) float64 {
		sort.Float64s(v)
		return v[len(v)/2]
	}
	medD, medT := map[string]float64{}, map[string]float64{}
	for t := range dists {
		medD[t], medT[t] = middle(dists[t]), middle(turns[t])
	}
	shape := func(l lap) bool {
		d := medD[l.track]
		return d > 0 && math.Abs(l.dist-d) <= FullLapLength*d && math.Abs(l.turns-medT[l.track]) <= FullLapTurns
	}
	// the usual lowest speed in each part of each track, from the laps of the right shape
	bins := map[string][][]float64{}
	for _, l := range laps {
		if l.dup || len(l.profile) != ProfileBins || !shape(l) {
			continue
		}
		b := bins[l.track]
		if b == nil {
			b = make([][]float64, ProfileBins)
			bins[l.track] = b
		}
		for i, v := range l.profile {
			b[i] = append(b[i], float64(v)*2)
		}
	}
	usual := map[string][]float64{}
	for t, b := range bins {
		if len(b[0]) < 5 {
			continue // too few laps to know the track
		}
		u := make([]float64, ProfileBins)
		for i := range b {
			u[i] = middle(b[i])
		}
		usual[t] = u
	}
	cut := func(l lap) bool {
		u := usual[l.track]
		if u == nil || len(l.profile) != ProfileBins {
			return false
		}
		for i, v := range u {
			if v >= FullLapSlow {
				continue
			}
			lo := 1e9 // the lap's lowest speed around there (laps differ a little in length)
			for j := max(0, i-1); j <= min(ProfileBins-1, i+1); j++ {
				lo = math.Min(lo, float64(l.profile[j])*2)
			}
			if lo > v+FullLapCut {
				return true
			}
		}
		return false
	}
	return s.Tx(func(q Q) error {
		for _, l := range laps {
			full := shape(l) && !cut(l)
			if _, err := q.ex.Exec(`UPDATE telemetry_laps SET full = ? WHERE file_id = ? AND n = ? AND full <> ?`, full, l.file, l.n, full); err != nil {
				return err
			}
		}
		return nil
	})
}

// RefreshTelemetrySaves puts the saves of each session together (D-163, D-167). ACC saves the
// whole session again and again (each file named after the laps so far, e.g. -0-, -2-, -6-, -59-),
// but every save is a window: later saves can lack the start of the session, and each keeps full
// data only for its last laps. Files of one car and track are one session when their times overlap
// (with 10 minutes to spare: the start worked out from a file drifts when the game was paused) and
// they share lap times to the millisecond at one fixed lap offset, with no lap contradicting it;
// or, for files without timed laps, when their first part is equally long.
//
// The longest save is the one the session is shown as; every lap of every save gets its number in
// the session, and where several saves have the same lap the one with data (then with a mark or
// note, then from the shown save) is used and the others are marked dup. Notes and marks stay on
// the lap they were made on. A file note or a setup set by hand is copied to the shown save.
func (s *Store) RefreshTelemetrySaves(game string) error {
	type lap struct {
		n, ms          int
		complete, data bool
		marked         bool // reference lap or note
	}
	type file struct {
		id, start, dur int64
		key            string
		first          int         // length of the part before the first line crossing (ms)
		laps           []lap       //
		times          map[int]int // lap number → lap time of the laps the clock timed
	}
	rows, err := s.ex.Query(`SELECT f.id, lower(f.car) || '|' || lower(f.track), f.started_at, f.duration_ms
		FROM telemetry_files f WHERE f.game = ? AND f.missing = 0 AND f.ext = '' ORDER BY 2, f.started_at, f.id`, game) // a data pack's file is a session of its own
	if err != nil {
		return err
	}
	var files []*file
	byID := map[int64]*file{}
	for rows.Next() {
		f := &file{times: map[int]int{}}
		if err := rows.Scan(&f.id, &f.key, &f.start, &f.dur); err != nil {
			rows.Close()
			return err
		}
		files = append(files, f)
		byID[f.id] = f
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	lr, err := s.ex.Query(`SELECT t.file_id, t.n, t.lap_ms, t.complete, t.dist_m >= ?, t.reference = 1 OR t.note <> ''
		FROM telemetry_laps t JOIN telemetry_files f ON f.id = t.file_id WHERE f.game = ? AND f.missing = 0 AND f.ext = '' ORDER BY t.file_id, t.n`, lapDataMinM, game)
	if err != nil {
		return err
	}
	for lr.Next() {
		var id int64
		var l lap
		if err := lr.Scan(&id, &l.n, &l.ms, &l.complete, &l.data, &l.marked); err != nil {
			lr.Close()
			return err
		}
		if f := byID[id]; f != nil {
			f.laps = append(f.laps, l)
			if l.complete {
				f.times[l.n] = l.ms
			}
			if l.n == 0 {
				f.first = l.ms
			}
		}
	}
	lr.Close()
	if err := lr.Err(); err != nil {
		return err
	}
	// shift: lap n of a is lap n+shift of b, with how many lap times agree on it. Only lap times
	// that occur once in each file count (a driver can do the same time twice in a session), one
	// offset must have the most votes, and no lap present in both at that offset may differ.
	once := func(f *file) map[int]int { // lap time → lap number, for times that occur once
		count := map[int]int{}
		for _, ms := range f.times {
			count[ms]++
		}
		out := map[int]int{}
		for n, ms := range f.times {
			if count[ms] == 1 {
				out[ms] = n
			}
		}
		return out
	}
	unique := map[*file]map[int]int{}
	for _, f := range files {
		unique[f] = once(f)
	}
	shift := func(a, b *file) (int, int, bool) {
		votes := map[int]int{}
		for ms, n := range unique[a] {
			if m, ok := unique[b][ms]; ok {
				votes[m-n]++
			}
		}
		best, top, ties := 0, 0, 0
		for sh, v := range votes {
			switch {
			case v > top:
				best, top, ties = sh, v, 0
			case v == top:
				ties++
			}
		}
		if top == 0 || ties > 0 {
			return 0, 0, false
		}
		for n, ms := range a.times {
			if other, ok := b.times[n+best]; ok && other != ms {
				return 0, 0, false
			}
		}
		return best, top, true
	}
	const spare = int64(10 * 60 * 1000)
	type edge struct {
		a, b   *file
		shift  int
		weight int // lap times agreeing; 0 = only the first part is equally long
	}
	var edges []edge
	for i, a := range files {
		for _, b := range files[i+1:] {
			if b.key != a.key || b.start > a.start+a.dur+spare {
				break
			}
			if sh, w, ok := shift(a, b); ok {
				edges = append(edges, edge{a, b, sh, w})
			} else if (len(a.times) == 0 || len(b.times) == 0) && a.first > 0 && a.first == b.first {
				// a save without timed laps (only the out-lap so far): its first part is exactly
				// as long as the other save's
				edges = append(edges, edge{a, b, 0, 0})
			}
		}
	}
	// groups, strongest evidence first; each file's offset: lap n of file f is lap n + off[f] of
	// the session. An edge that contradicts offsets already set is left out (a coincidence).
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].weight > edges[j].weight })
	off := map[*file]int{}
	group := map[*file]*[]*file{}
	for _, e := range edges {
		ga, gb := group[e.a], group[e.b]
		if ga == nil {
			ga = &[]*file{e.a}
			group[e.a], off[e.a] = ga, 0
		}
		if gb == nil {
			gb = &[]*file{e.b}
			group[e.b], off[e.b] = gb, 0
		}
		want := off[e.a] - e.shift // where e.b belongs: n + off[a] = n + shift + off[b]
		if ga == gb {
			continue // already together (when it agrees) or a contradiction (left out)
		}
		if len(*ga) < len(*gb) { // move the smaller group: express it from e.b's side
			ga, gb = gb, ga
			want = off[e.b] + e.shift
			e.a, e.b = e.b, e.a
		}
		delta := want - off[e.b]
		for _, m := range *gb {
			off[m] += delta
			group[m] = ga
		}
		*ga = append(*ga, *gb...)
	}
	var groups [][]*file
	seen := map[*[]*file]bool{}
	for _, f := range files {
		g := group[f]
		if g == nil || seen[g] {
			continue
		}
		seen[g] = true
		low := off[(*g)[0]]
		for _, m := range *g {
			low = min(low, off[m])
		}
		for _, m := range *g {
			off[m] -= low // lap 0 of the earliest save is lap 0 of the session
		}
		groups = append(groups, *g)
	}
	return s.Tx(func(q Q) error {
		if _, err := q.ex.Exec(`UPDATE telemetry_files SET kept_by = 0, superseded = 0 WHERE game = ? AND (kept_by <> 0 OR superseded <> 0)`, game); err != nil {
			return err
		}
		if _, err := q.ex.Exec(`UPDATE telemetry_laps SET sess_file = file_id, sess_n = n, dup = 0
			WHERE (sess_file <> file_id OR sess_n <> n OR dup <> 0) AND file_id IN (SELECT id FROM telemetry_files WHERE game = ? AND ext = '')`, game); err != nil { // a data pack's laps keep the sender's lap numbers
			return err
		}
		for _, g := range groups {
			keep := g[0]
			for _, m := range g {
				if m.dur > keep.dur || m.dur == keep.dur && m.id > keep.id {
					keep = m
				}
			}
			type pick struct {
				f *file
				l lap
			}
			better := func(x, y pick) bool { // x is the better copy of a lap
				rank := func(p pick) [5]bool {
					return [5]bool{p.l.complete && p.l.data, p.l.data, p.l.complete, p.l.marked, p.f == keep}
				}
				rx, ry := rank(x), rank(y)
				for i := range rx {
					if rx[i] != ry[i] {
						return rx[i]
					}
				}
				return x.f.id > y.f.id
			}
			shown := map[int]pick{}
			for _, m := range g {
				for _, l := range m.laps {
					r := l.n + off[m]
					if cur, ok := shown[r]; !ok || better(pick{m, l}, cur) {
						shown[r] = pick{m, l}
					}
				}
			}
			for _, m := range g {
				if m != keep {
					if _, err := q.ex.Exec(`UPDATE telemetry_files SET kept_by = ?, superseded = 1 WHERE id = ?`, keep.id, m.id); err != nil {
						return err
					}
					for _, stmt := range []string{
						`UPDATE telemetry_files SET note = (SELECT note FROM telemetry_files WHERE id = ?1) WHERE id = ?2 AND note = ''`,
						`UPDATE telemetry_files SET setup_id = (SELECT setup_id FROM telemetry_files WHERE id = ?1), version_id = (SELECT version_id FROM telemetry_files WHERE id = ?1), match = 'manual'
							WHERE id = ?2 AND match = 'none' AND (SELECT match FROM telemetry_files WHERE id = ?1) = 'manual'`,
					} {
						if _, err := q.ex.Exec(stmt, m.id, keep.id); err != nil {
							return err
						}
					}
				}
				for _, l := range m.laps {
					r := l.n + off[m]
					p := shown[r]
					dup := p.f != m || p.l.n != l.n
					if _, err := q.ex.Exec(`UPDATE telemetry_laps SET sess_file = ?, sess_n = ?, dup = ? WHERE file_id = ? AND n = ?`, keep.id, r, dup, m.id, l.n); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// lapDataMinM: a lap shorter than this (speed integrated) has no data of its own.
const lapDataMinM = 100

// TelemetryDisk returns the number of telemetry files of a game in the folder and their size on
// disk, earlier saves included.
func (q Q) TelemetryDisk(game string) (files int, bytes int64, err error) {
	err = q.ex.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size), 0) FROM telemetry_files WHERE game = ? AND missing = 0 AND ext = ''`, game).Scan(&files, &bytes)
	return
}

// TeleLapRow is one full lap on a track with its car and its session's details (Best laps tab).
type TeleLapRow struct {
	TeleLap
	Car         string    `json:"car"`
	SessionFile int64     `json:"sessionFile"` // the file the session is shown as
	StartedAt   time.Time `json:"startedAt"`   // the session's start
	Kind        string    `json:"kind"`
	SetupID     int64     `json:"setupId"`
	SessionID   int64     `json:"sessionId"`
}

// TelemetryTrackLaps returns every full lap on a track, of one car or ("") of every car, each lap
// once, fastest first.
func (q Q) TelemetryTrackLaps(game, car, track string) ([]TeleLapRow, error) {
	where, args := `WHERE f.game = ? AND f.track = ? AND f.missing = 0 AND t.dup = 0 AND t.full = 1`, []any{game, track}
	if car != "" {
		where += ` AND f.car = ?`
		args = append(args, car)
	}
	// A lap linked to a recorded lap takes that lap's session and setup: one ACC file can hold a
	// whole server weekend, and setups can change between stints (D-177, D-179).
	rows, err := q.ex.Query(lapSelect+`, f.car, k.id, COALESCE(ls.started_at, k.started_at), COALESCE(ls.kind, s.kind, ''), COALESCE(l.session_id, k.session_id, 0),
		CASE WHEN l.id IS NOT NULL THEN COALESCE(l.setup_id, 0) WHEN k.match = 'manual' THEN COALESCE(k.setup_id, 0) ELSE COALESCE(s.setup_id, 0) END`+lapFrom+`
		LEFT JOIN sessions ls ON ls.id = l.session_id
		JOIN telemetry_files f ON f.id = t.file_id JOIN telemetry_files k ON k.id = t.sess_file LEFT JOIN sessions s ON s.id = k.session_id
		`+where+` ORDER BY t.lap_ms, t.file_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeleLapRow{}
	for rows.Next() {
		var r TeleLapRow
		var st int64
		if err := scanLap(rows, &r.TeleLap, &r.Car, &r.SessionFile, &st, &r.Kind, &r.SessionID, &r.SetupID); err != nil {
			return nil, err
		}
		r.StartedAt = fromMS(st)
		out = append(out, r)
	}
	return out, rows.Err()
}

// TeleBest is the fastest full lap on a track (any car), to draw the track's outline from.
type TeleBest struct {
	Track  string
	FileID int64
	N      int
}

// TelemetryBestPerTrack returns the fastest full laps of every track of a game (up to per per
// track, fastest first), to draw outlines from.
func (q Q) TelemetryBestPerTrack(game string, per int) ([]TeleBest, error) {
	rows, err := q.ex.Query(`SELECT track, file_id, n FROM (
		SELECT lower(f.track) AS track, t.file_id, t.n, ROW_NUMBER() OVER (PARTITION BY lower(f.track) ORDER BY t.lap_ms, t.file_id) AS rn
		FROM telemetry_laps t JOIN telemetry_files f ON f.id = t.file_id
		WHERE f.game = ? AND f.missing = 0 AND t.dup = 0 AND t.full = 1) WHERE rn <= ? ORDER BY track, rn`, game, per)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeleBest
	for rows.Next() {
		var b TeleBest
		if err := rows.Scan(&b.Track, &b.FileID, &b.N); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// TelemetryBestPerCar returns the fastest full lap per track of one car (laps known to be invalid
// left out), by lower-case track id.
func (q Q) TelemetryBestPerCar(game, car string) (map[string]int, error) {
	rows, err := q.ex.Query(`SELECT lower(f.track), MIN(t.lap_ms)`+lapFrom+`JOIN telemetry_files f ON f.id = t.file_id
		WHERE f.game = ? AND f.car = ? AND f.missing = 0 AND t.dup = 0 AND t.full = 1 AND COALESCE(l.valid, 1) = 1 AND f.ext = ''
		GROUP BY lower(f.track)`, game, car)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var track string
		var ms int
		if err := rows.Scan(&track, &ms); err != nil {
			return nil, err
		}
		out[track] = ms
	}
	return out, rows.Err()
}
