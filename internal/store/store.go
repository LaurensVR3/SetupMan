// Package store is SetupMan's own library: a SQLite database in %APPDATA%\SetupMan
// with every setup, all its versions, links between them (history) and a changelog.
// It never touches a game's setup folder (spec D-07, D-41).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)

	"setupman/internal/naming"
)

// Link kinds: how a setup's parent was decided.
const (
	LinkNone     = "none"     // starts its own family
	LinkProbable = "probable" // rebuilt from file dates + similarity (D-40)
	LinkObserved = "observed" // SetupMan saw the new file appear
	LinkManual   = "manual"   // set by the user; never changed automatically
)

// Setup is one line of versions, usually one setup file.
type Setup struct {
	ID              int64       `json:"id"`
	Game            string      `json:"game"`
	Car             string      `json:"car"`
	Track           string      `json:"track"`
	Name            string      `json:"name"`
	RelPath         string      `json:"relPath"` // "" = no longer in the game folder
	ParentVersionID int64       `json:"parentVersionId"`
	LinkKind        string      `json:"linkKind"`
	LinkScore       float64     `json:"linkScore"`
	Session         string      `json:"session"`
	SessionManual   bool        `json:"sessionManual"`
	Meta            naming.Meta `json:"meta"`
	MetaManual      bool        `json:"metaManual"`
	Note            string      `json:"note"`
	Fresh           bool        `json:"fresh"`         // new from the game, not reviewed yet (D-80)
	DiskVersionID   int64       `json:"diskVersionId"` // version in the game folder file (0 = not in the folder)
	DeletedAt       time.Time   `json:"deletedAt"`     // in the bin since (zero = not deleted, D-102)
	CreatedAt       time.Time   `json:"createdAt"`
}

// Version is one saved state of a setup.
type Version struct {
	ID           int64     `json:"id"`
	SetupID      int64     `json:"setupId"`
	Seq          int       `json:"seq"` // 1, 2, 3 … within the setup
	Fingerprint  string    `json:"fingerprint"`
	Content      []byte    `json:"-"`
	FileModified time.Time `json:"fileModified"`
	ObservedAt   time.Time `json:"observedAt"`
	Source       string    `json:"source"` // "import" or "change"
	Note         string    `json:"note"`
}

// Event is one changelog entry (spec D-18).
type Event struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	Game      string         `json:"game"`
	Kind      string         `json:"kind"` // new-setup, new-version, branch, renamed, removed, relinked
	SetupID   int64          `json:"setupId"`
	VersionID int64          `json:"versionId"`
	Details   map[string]any `json:"details"`
	Seen      bool           `json:"seen"`
}

// Store is the library database.
type Store struct {
	db *sql.DB
	Q  // queries outside a transaction
}

// Open opens (or creates) the library at path. Use ":memory:" in tests.
func Open(path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one connection: simple, and required for ":memory:"
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("library database: %w", err)
	}
	return &Store{db: db, Q: Q{db}}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// BackupTo writes a complete, consistent copy of the library to a new file (Full SetupMan
// backups, D-82). It works while SetupMan is running.
func (s *Store) BackupTo(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// Tx runs fn in one transaction: everything is saved, or nothing.
func (s *Store) Tx(fn func(q Q) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(Q{tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

const schemaVersion = 8

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v >= schemaVersion {
		return nil
	}
	if v == 7 {
		return migrateV8(db)
	}
	if v == 6 {
		if err := migrateV7(db); err != nil {
			return err
		}
		return migrateV8(db)
	}
	if v == 5 {
		if err := migrateV6(db); err != nil {
			return err
		}
		if err := migrateV7(db); err != nil {
			return err
		}
		return migrateV8(db)
	}
	if v < 1 {
		if err := migrateV1(db); err != nil {
			return err
		}
	}
	if v < 2 {
		if err := migrateV2(db); err != nil {
			return err
		}
	}
	if v < 3 {
		if err := migrateV3(db); err != nil {
			return err
		}
	}
	if v < 4 {
		if err := migrateV4(db); err != nil {
			return err
		}
	}
	// Version 5 (telemetry): driving sessions and their laps, linked to the setup version driven.
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id         INTEGER PRIMARY KEY,
	game       TEXT NOT NULL,
	car        TEXT NOT NULL COLLATE NOCASE,
	track      TEXT NOT NULL COLLATE NOCASE,
	kind       TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL,
	setup_id   INTEGER,
	version_id INTEGER,
	match      TEXT NOT NULL DEFAULT 'none'
);
CREATE TABLE IF NOT EXISTS laps (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	setup_id   INTEGER,
	version_id INTEGER,
	at         INTEGER NOT NULL,
	lap_ms     INTEGER NOT NULL,
	valid      INTEGER NOT NULL,
	pit        INTEGER NOT NULL,
	fuel_used  REAL NOT NULL DEFAULT 0,
	air        REAL,
	road       REAL
);
CREATE INDEX IF NOT EXISTS laps_setup ON laps(setup_id);
CREATE INDEX IF NOT EXISTS laps_session ON laps(session_id);
PRAGMA user_version = 5;`)
	if err != nil {
		return err
	}
	if err := migrateV6(db); err != nil {
		return err
	}
	if err := migrateV7(db); err != nil {
		return err
	}
	return migrateV8(db)
}

// migrateV8 (lap telemetry comparison, D-156): distance-aligned lap traces from live ACC shared
// memory, imported MoTeC (.ld/.ldx) logs, and sample laps.
func migrateV8(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS telemetry_laps (
	id             INTEGER PRIMARY KEY,
	game           TEXT NOT NULL,
	car            TEXT NOT NULL COLLATE NOCASE,
	track          TEXT NOT NULL COLLATE NOCASE,
	source         TEXT NOT NULL,
	source_file    TEXT NOT NULL DEFAULT '',
	label          TEXT NOT NULL DEFAULT '',
	lap_ms         INTEGER NOT NULL,
	lap_number     INTEGER NOT NULL DEFAULT 1,
	valid          INTEGER NOT NULL DEFAULT 1,
	is_reference   INTEGER NOT NULL DEFAULT 0,
	track_length_m REAL NOT NULL DEFAULT 0,
	session_id     INTEGER NOT NULL DEFAULT 0,
	setup_id       INTEGER NOT NULL DEFAULT 0,
	recorded_at    INTEGER NOT NULL,
	has_coords     INTEGER NOT NULL DEFAULT 0,
	stats_json     TEXT NOT NULL DEFAULT '{}',
	trace_gz       BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS telemetry_laps_track ON telemetry_laps(game, track, car);
PRAGMA user_version = 8;`)
	return err
}

// migrateV7 (pit stop advice, D-124): average hot tyre pressures per lap, and whether rain tyres
// were fitted.
func migrateV7(db *sql.DB) error {
	_, err := db.Exec(`
ALTER TABLE laps ADD COLUMN p_fl REAL;
ALTER TABLE laps ADD COLUMN p_fr REAL;
ALTER TABLE laps ADD COLUMN p_rl REAL;
ALTER TABLE laps ADD COLUMN p_rr REAL;
ALTER TABLE laps ADD COLUMN wet INTEGER NOT NULL DEFAULT 0;
PRAGMA user_version = 7;`)
	return err
}

// migrateV6 (bin, D-102): when a setup was deleted; deleted setups stay 30 days.
func migrateV6(db *sql.DB) error {
	_, err := db.Exec(`
ALTER TABLE setups ADD COLUMN deleted_at INTEGER;
PRAGMA user_version = 6;`)
	return err
}

// migrateV4 (fuel calculator): small saved values, e.g. fuel inputs per car and track.
func migrateV4(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS kv (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
PRAGMA user_version = 4;`)
	return err
}

// migrateV3 (editing): which version is in the game folder right now. Before editing
// existed, the file always held the newest version.
func migrateV3(db *sql.DB) error {
	_, err := db.Exec(`
ALTER TABLE setups ADD COLUMN disk_version_id INTEGER;
UPDATE setups SET disk_version_id = (SELECT id FROM versions WHERE setup_id = setups.id ORDER BY seq DESC LIMIT 1)
	WHERE rel_path IS NOT NULL;
PRAGMA user_version = 3;`)
	return err
}

// migrateV2 (Phase 3, staging): "new from the game" flag, managed car/track folders, and
// changes queued while driving.
func migrateV2(db *sql.DB) error {
	_, err := db.Exec(`
ALTER TABLE setups ADD COLUMN fresh INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS managed_tracks (
	game  TEXT NOT NULL,
	car   TEXT NOT NULL COLLATE NOCASE,
	track TEXT NOT NULL COLLATE NOCASE,
	PRIMARY KEY (game, car, track)
);
CREATE TABLE IF NOT EXISTS pending_ops (
	id      INTEGER PRIMARY KEY,
	at      INTEGER NOT NULL,
	game    TEXT NOT NULL,
	kind    TEXT NOT NULL,
	payload TEXT NOT NULL DEFAULT '{}'
);
PRAGMA user_version = 2;`)
	return err
}

func migrateV1(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS setups (
	id                INTEGER PRIMARY KEY,
	game              TEXT NOT NULL,
	car               TEXT NOT NULL,
	track             TEXT NOT NULL,
	name              TEXT NOT NULL,
	rel_path          TEXT,
	parent_version_id INTEGER REFERENCES versions(id) ON DELETE SET NULL,
	link_kind         TEXT NOT NULL DEFAULT 'none',
	link_score        REAL NOT NULL DEFAULT 0,
	session           TEXT NOT NULL DEFAULT '',
	session_manual    INTEGER NOT NULL DEFAULT 0,
	meta              TEXT NOT NULL DEFAULT '{}',
	meta_manual       INTEGER NOT NULL DEFAULT 0,
	note              TEXT NOT NULL DEFAULT '',
	created_at        INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS setups_path ON setups(game, rel_path COLLATE NOCASE) WHERE rel_path IS NOT NULL;
CREATE INDEX IF NOT EXISTS setups_track ON setups(game, car, track);
CREATE TABLE IF NOT EXISTS versions (
	id            INTEGER PRIMARY KEY,
	setup_id      INTEGER NOT NULL REFERENCES setups(id) ON DELETE CASCADE,
	seq           INTEGER NOT NULL,
	fingerprint   TEXT NOT NULL,
	content       BLOB NOT NULL,
	file_modified INTEGER NOT NULL,
	observed_at   INTEGER NOT NULL,
	source        TEXT NOT NULL,
	note          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS versions_setup ON versions(setup_id, seq);
CREATE TABLE IF NOT EXISTS events (
	id         INTEGER PRIMARY KEY,
	at         INTEGER NOT NULL,
	game       TEXT NOT NULL,
	kind       TEXT NOT NULL,
	setup_id   INTEGER,
	version_id INTEGER,
	details    TEXT NOT NULL DEFAULT '{}',
	seen       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS games (
	id          TEXT PRIMARY KEY,
	imported_at INTEGER NOT NULL
);
PRAGMA user_version = 1;`)
	return err
}

// ---------- queries ----------

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Q runs queries, either directly or inside a transaction (see Store.Tx).
type Q struct{ ex execer }

func ms(t time.Time) int64 { return t.UnixMilli() }
func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

const setupCols = `id, game, car, track, name, COALESCE(rel_path,''), COALESCE(parent_version_id,0), link_kind, link_score,
	session, session_manual, meta, meta_manual, note, created_at, fresh, COALESCE(disk_version_id,0), COALESCE(deleted_at,0)`

func scanSetup(row interface{ Scan(...any) error }) (*Setup, error) {
	var s Setup
	var meta string
	var created, deleted int64
	if err := row.Scan(&s.ID, &s.Game, &s.Car, &s.Track, &s.Name, &s.RelPath, &s.ParentVersionID, &s.LinkKind, &s.LinkScore,
		&s.Session, &s.SessionManual, &meta, &s.MetaManual, &s.Note, &created, &s.Fresh, &s.DiskVersionID, &deleted); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(meta), &s.Meta)
	if s.Meta.Codes == nil {
		s.Meta.Codes = []string{}
	}
	s.CreatedAt = fromMS(created)
	if deleted != 0 {
		s.DeletedAt = fromMS(deleted)
	}
	return &s, nil
}

func (q Q) setups(where string, args ...any) ([]Setup, error) {
	rows, err := q.ex.Query(`SELECT `+setupCols+` FROM setups `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Setup{}
	for rows.Next() {
		s, err := scanSetup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// SetupsForGame returns all setups of a game.
func (q Q) SetupsForGame(game string) ([]Setup, error) {
	return q.setups(`WHERE game = ? AND deleted_at IS NULL ORDER BY id`, game)
}

// SetupsForTrack returns all setups of one car on one track.
func (q Q) SetupsForTrack(game, car, track string) ([]Setup, error) {
	return q.setups(`WHERE game = ? AND car = ? COLLATE NOCASE AND track = ? COLLATE NOCASE AND deleted_at IS NULL ORDER BY id`, game, car, track)
}

// DeletedSetups returns the setups in the bin (D-102), most recently deleted first.
func (q Q) DeletedSetups() ([]Setup, error) {
	return q.setups(`WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
}

// SetDeleted moves a setup into the bin (t non-zero) or takes it out (zero).
func (q Q) SetDeleted(id int64, t time.Time) error {
	var v any
	if !t.IsZero() {
		v = ms(t)
	}
	_, err := q.ex.Exec(`UPDATE setups SET deleted_at = ? WHERE id = ?`, v, id)
	return err
}

// RemoveSetupRecord deletes one setup with its versions and changelog lines straight away (no
// bin). Only for a record SetupMan made by mistake, e.g. a game save that turned out to be a new
// version of another setup (D-138). Laps recorded with it stay, without a setup.
func (q Q) RemoveSetupRecord(id int64) error {
	for _, stmt := range []string{
		`UPDATE laps SET setup_id = NULL, version_id = NULL WHERE setup_id = ?`,
		`UPDATE sessions SET setup_id = NULL, version_id = NULL, match = 'none' WHERE setup_id = ?`,
		`DELETE FROM events WHERE setup_id = ?`,
		`DELETE FROM setups WHERE id = ?`,
	} {
		if _, err := q.ex.Exec(stmt, id); err != nil {
			return err
		}
	}
	return nil
}

// PurgeDeleted removes setups that have been in the bin since before t, with their versions.
// Laps recorded with them stay, without a setup. Setups made from them start their own family.
func (q Q) PurgeDeleted(before time.Time) (int, error) {
	if _, err := q.ex.Exec(`UPDATE laps SET setup_id = NULL, version_id = NULL WHERE setup_id IN (SELECT id FROM setups WHERE deleted_at < ?)`, ms(before)); err != nil {
		return 0, err
	}
	if _, err := q.ex.Exec(`UPDATE sessions SET setup_id = NULL, version_id = NULL, match = 'none' WHERE setup_id IN (SELECT id FROM setups WHERE deleted_at < ?)`, ms(before)); err != nil {
		return 0, err
	}
	res, err := q.ex.Exec(`DELETE FROM setups WHERE deleted_at < ?`, ms(before))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Setup returns one setup, or nil.
func (q Q) Setup(id int64) (*Setup, error) {
	s, err := scanSetup(q.ex.QueryRow(`SELECT `+setupCols+` FROM setups WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// SetupByPath returns the setup currently stored at a file path, or nil.
func (q Q) SetupByPath(game, relPath string) (*Setup, error) {
	s, err := scanSetup(q.ex.QueryRow(`SELECT `+setupCols+` FROM setups WHERE game = ? AND rel_path = ? COLLATE NOCASE AND deleted_at IS NULL`, game, relPath))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// InsertSetup stores a new setup and sets its ID.
func (q Q) InsertSetup(s *Setup) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.LinkKind == "" {
		s.LinkKind = LinkNone
	}
	meta, _ := json.Marshal(s.Meta)
	res, err := q.ex.Exec(`INSERT INTO setups (game, car, track, name, rel_path, parent_version_id, link_kind, link_score,
		session, session_manual, meta, meta_manual, note, created_at, fresh, disk_version_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.Game, s.Car, s.Track, s.Name, nullStr(s.RelPath), nullID(s.ParentVersionID), s.LinkKind, s.LinkScore,
		s.Session, s.SessionManual, string(meta), s.MetaManual, s.Note, ms(s.CreatedAt), s.Fresh, nullID(s.DiskVersionID))
	if err != nil {
		return err
	}
	s.ID, err = res.LastInsertId()
	return err
}

// UpdateSetup saves all changeable fields of a setup.
func (q Q) UpdateSetup(s *Setup) error {
	meta, _ := json.Marshal(s.Meta)
	_, err := q.ex.Exec(`UPDATE setups SET name=?, rel_path=?, parent_version_id=?, link_kind=?, link_score=?,
		session=?, session_manual=?, meta=?, meta_manual=?, note=?, fresh=?, disk_version_id=? WHERE id=?`,
		s.Name, nullStr(s.RelPath), nullID(s.ParentVersionID), s.LinkKind, s.LinkScore,
		s.Session, s.SessionManual, string(meta), s.MetaManual, s.Note, s.Fresh, nullID(s.DiskVersionID), s.ID)
	return err
}

const versionCols = `id, setup_id, seq, fingerprint, content, file_modified, observed_at, source, note`

func scanVersion(row interface{ Scan(...any) error }) (*Version, error) {
	var v Version
	var fm, ob int64
	if err := row.Scan(&v.ID, &v.SetupID, &v.Seq, &v.Fingerprint, &v.Content, &fm, &ob, &v.Source, &v.Note); err != nil {
		return nil, err
	}
	v.FileModified, v.ObservedAt = fromMS(fm), fromMS(ob)
	return &v, nil
}

// InsertVersion stores a new version (Seq is filled in automatically) and sets its ID.
func (q Q) InsertVersion(v *Version) error {
	if v.ObservedAt.IsZero() {
		v.ObservedAt = time.Now()
	}
	if err := q.ex.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM versions WHERE setup_id = ?`, v.SetupID).Scan(&v.Seq); err != nil {
		return err
	}
	res, err := q.ex.Exec(`INSERT INTO versions (setup_id, seq, fingerprint, content, file_modified, observed_at, source, note)
		VALUES (?,?,?,?,?,?,?,?)`, v.SetupID, v.Seq, v.Fingerprint, v.Content, ms(v.FileModified), ms(v.ObservedAt), v.Source, v.Note)
	if err != nil {
		return err
	}
	v.ID, err = res.LastInsertId()
	return err
}

// UpdateVersionContent replaces the stored file content (same scored values, D-52).
func (q Q) UpdateVersionContent(id int64, content []byte, fileModified time.Time) error {
	_, err := q.ex.Exec(`UPDATE versions SET content=?, file_modified=? WHERE id=?`, content, ms(fileModified), id)
	return err
}

// UpdateVersionNote saves a note on one version.
func (q Q) UpdateVersionNote(id int64, note string) error {
	_, err := q.ex.Exec(`UPDATE versions SET note=? WHERE id=?`, note, id)
	return err
}

// LatestVersion returns the newest version of a setup, or nil.
func (q Q) LatestVersion(setupID int64) (*Version, error) {
	v, err := scanVersion(q.ex.QueryRow(`SELECT `+versionCols+` FROM versions WHERE setup_id = ? ORDER BY seq DESC LIMIT 1`, setupID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// Version returns one version, or nil.
func (q Q) Version(id int64) (*Version, error) {
	v, err := scanVersion(q.ex.QueryRow(`SELECT `+versionCols+` FROM versions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// Versions returns all versions of a setup, oldest first.
func (q Q) Versions(setupID int64) ([]Version, error) {
	rows, err := q.ex.Query(`SELECT `+versionCols+` FROM versions WHERE setup_id = ? ORDER BY seq`, setupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// InsertEvent adds a changelog entry.
func (q Q) InsertEvent(e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	d, _ := json.Marshal(e.Details)
	_, err := q.ex.Exec(`INSERT INTO events (at, game, kind, setup_id, version_id, details) VALUES (?,?,?,?,?,?)`,
		ms(e.At), e.Game, e.Kind, nullID(e.SetupID), nullID(e.VersionID), string(d))
	return err
}

// Events returns the newest changelog entries (only unseen ones when unseenOnly).
func (q Q) Events(unseenOnly bool, limit int) ([]Event, error) {
	where := ""
	if unseenOnly {
		where = "WHERE seen = 0"
	}
	rows, err := q.ex.Query(`SELECT id, at, game, kind, COALESCE(setup_id,0), COALESCE(version_id,0), details, seen
		FROM events `+where+` ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var at int64
		var d string
		if err := rows.Scan(&e.ID, &at, &e.Game, &e.Kind, &e.SetupID, &e.VersionID, &d, &e.Seen); err != nil {
			return nil, err
		}
		e.At = fromMS(at)
		_ = json.Unmarshal([]byte(d), &e.Details)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountUnseen returns the number of unseen changelog entries.
func (q Q) CountUnseen() (int, error) {
	var n int
	err := q.ex.QueryRow(`SELECT COUNT(*) FROM events WHERE seen = 0`).Scan(&n)
	return n, err
}

// MarkAllSeen marks every changelog entry as seen.
func (q Q) MarkAllSeen() error {
	_, err := q.ex.Exec(`UPDATE events SET seen = 1 WHERE seen = 0`)
	return err
}

// Imported reports whether the first import of a game was done.
func (q Q) Imported(game string) (bool, error) {
	var n int
	err := q.ex.QueryRow(`SELECT COUNT(*) FROM games WHERE id = ?`, game).Scan(&n)
	return n > 0, err
}

// SetImported records that the first import of a game was done.
func (q Q) SetImported(game string) error {
	_, err := q.ex.Exec(`INSERT OR IGNORE INTO games (id, imported_at) VALUES (?, ?)`, game, ms(time.Now()))
	return err
}

// ---------- staging (schema v2) ----------

// SetManagedTrack marks a car/track folder as managed: it holds only staged setups (D-75).
func (q Q) SetManagedTrack(game, car, track string) error {
	_, err := q.ex.Exec(`INSERT OR IGNORE INTO managed_tracks (game, car, track) VALUES (?,?,?)`, game, car, track)
	return err
}

// IsManagedTrack reports whether a car/track folder is managed.
func (q Q) IsManagedTrack(game, car, track string) (bool, error) {
	var n int
	err := q.ex.QueryRow(`SELECT COUNT(*) FROM managed_tracks WHERE game = ? AND car = ? AND track = ?`, game, car, track).Scan(&n)
	return n > 0, err
}

// PendingOp is a staging change queued while the player was driving (D-79).
type PendingOp struct {
	ID      int64          `json:"id"`
	At      time.Time      `json:"at"`
	Game    string         `json:"game"`
	Kind    string         `json:"kind"` // "stage", "unstage", "keep-only"
	Payload map[string]any `json:"payload"`
}

// AddPendingOp queues a change.
func (q Q) AddPendingOp(op PendingOp) error {
	p, _ := json.Marshal(op.Payload)
	_, err := q.ex.Exec(`INSERT INTO pending_ops (at, game, kind, payload) VALUES (?,?,?,?)`, ms(time.Now()), op.Game, op.Kind, string(p))
	return err
}

// PendingOps returns queued changes, oldest first.
func (q Q) PendingOps() ([]PendingOp, error) {
	rows, err := q.ex.Query(`SELECT id, at, game, kind, payload FROM pending_ops ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingOp{}
	for rows.Next() {
		var op PendingOp
		var at int64
		var p string
		if err := rows.Scan(&op.ID, &at, &op.Game, &op.Kind, &p); err != nil {
			return nil, err
		}
		op.At = fromMS(at)
		_ = json.Unmarshal([]byte(p), &op.Payload)
		out = append(out, op)
	}
	return out, rows.Err()
}

// DeletePendingOp removes a queued change (after it ran).
func (q Q) DeletePendingOp(id int64) error {
	_, err := q.ex.Exec(`DELETE FROM pending_ops WHERE id = ?`, id)
	return err
}

// CountFresh returns how many new in-game saves of a game are still in the folder, unreviewed.
func (q Q) CountFresh(game string) (int, error) {
	var n int
	err := q.ex.QueryRow(`SELECT COUNT(*) FROM setups WHERE game = ? AND fresh = 1 AND rel_path IS NOT NULL`, game).Scan(&n)
	return n, err
}

// ---------- saved values (schema v4) ----------

// GetKV returns a saved value ("" when not set).
func (q Q) GetKV(key string) (string, error) {
	var v string
	err := q.ex.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetKV saves a value.
func (q Q) SetKV(key, value string) error {
	_, err := q.ex.Exec(`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---------- telemetry (schema v5) ----------

// Session is one driving session (practice, qualifying, race …) on a car and track.
type Session struct {
	ID        int64     `json:"id"`
	Game      string    `json:"game"`
	Car       string    `json:"car"`
	Track     string    `json:"track"`
	Kind      string    `json:"kind"` // "practice", "qualifying", "race", …
	StartedAt time.Time `json:"startedAt"`
	SetupID   int64     `json:"setupId"`   // 0 = not known
	VersionID int64     `json:"versionId"` //
	Match     string    `json:"match"`     // "auto" (guessed), "manual" (set by the user) or "none"
	Laps      int       `json:"laps"`      // filled in by SessionsForSetup
}

// Lap is one completed lap.
type Lap struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"sessionId"`
	SetupID   int64     `json:"setupId"`
	VersionID int64     `json:"versionId"`
	At        time.Time `json:"at"`
	LapMs     int       `json:"lapMs"`
	Valid     bool      `json:"valid"`
	Pit       bool      `json:"pit"`      // out-lap, in-lap or a lap with a stop (not counted in the average, D-48)
	FuelUsed  float64   `json:"fuelUsed"` // litres; 0 = unknown
	Air       float64   `json:"air"`
	Road      float64   `json:"road"`
	HasTemps  bool      `json:"hasTemps"`
	// Average hot tyre pressures over the lap, psi, FL FR RL RR (D-124); only with HasPressures.
	Pressures    [4]float64 `json:"pressures"`
	HasPressures bool       `json:"hasPressures"`
	Wet          bool       `json:"wet"` // rain tyres fitted
}

// InsertSession stores a new session and sets its ID.
func (q Q) InsertSession(s *Session) error {
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}
	if s.Match == "" {
		s.Match = "none"
	}
	res, err := q.ex.Exec(`INSERT INTO sessions (game, car, track, kind, started_at, setup_id, version_id, match) VALUES (?,?,?,?,?,?,?,?)`,
		s.Game, s.Car, s.Track, s.Kind, ms(s.StartedAt), nullID(s.SetupID), nullID(s.VersionID), s.Match)
	if err != nil {
		return err
	}
	s.ID, err = res.LastInsertId()
	return err
}

// SetSessionSetup records which setup was driven in a session (and moves its laps along when
// the user corrects it).
func (q Q) SetSessionSetup(sessionID, setupID, versionID int64, match string, moveLaps bool) error {
	if _, err := q.ex.Exec(`UPDATE sessions SET setup_id=?, version_id=?, match=? WHERE id=?`, nullID(setupID), nullID(versionID), match, sessionID); err != nil {
		return err
	}
	if moveLaps {
		_, err := q.ex.Exec(`UPDATE laps SET setup_id=?, version_id=? WHERE session_id=?`, nullID(setupID), nullID(versionID), sessionID)
		return err
	}
	return nil
}

// Session returns one session, or nil.
func (q Q) Session(id int64) (*Session, error) {
	var s Session
	var at int64
	err := q.ex.QueryRow(`SELECT id, game, car, track, kind, started_at, COALESCE(setup_id,0), COALESCE(version_id,0), match FROM sessions WHERE id=?`, id).
		Scan(&s.ID, &s.Game, &s.Car, &s.Track, &s.Kind, &at, &s.SetupID, &s.VersionID, &s.Match)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	s.StartedAt = fromMS(at)
	return &s, err
}

// InsertLap stores a lap.
func (q Q) InsertLap(l *Lap) error {
	if l.At.IsZero() {
		l.At = time.Now()
	}
	var air, road any
	if l.HasTemps {
		air, road = l.Air, l.Road
	}
	var pr [4]any
	if l.HasPressures {
		for i, v := range l.Pressures {
			pr[i] = v
		}
	}
	res, err := q.ex.Exec(`INSERT INTO laps (session_id, setup_id, version_id, at, lap_ms, valid, pit, fuel_used, air, road, p_fl, p_fr, p_rl, p_rr, wet) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.SessionID, nullID(l.SetupID), nullID(l.VersionID), ms(l.At), l.LapMs, l.Valid, l.Pit, l.FuelUsed, air, road, pr[0], pr[1], pr[2], pr[3], l.Wet)
	if err != nil {
		return err
	}
	l.ID, err = res.LastInsertId()
	return err
}

func (q Q) laps(where string, args ...any) ([]Lap, error) {
	rows, err := q.ex.Query(`SELECT l.id, l.session_id, COALESCE(l.setup_id,0), COALESCE(l.version_id,0), l.at, l.lap_ms, l.valid, l.pit, l.fuel_used,
		l.air, l.road, l.p_fl, l.p_fr, l.p_rl, l.p_rr, l.wet FROM laps l JOIN sessions s ON s.id = l.session_id `+where+` ORDER BY l.at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lap{}
	for rows.Next() {
		var l Lap
		var at int64
		var air, road sql.NullFloat64
		var pr [4]sql.NullFloat64
		if err := rows.Scan(&l.ID, &l.SessionID, &l.SetupID, &l.VersionID, &at, &l.LapMs, &l.Valid, &l.Pit, &l.FuelUsed, &air, &road,
			&pr[0], &pr[1], &pr[2], &pr[3], &l.Wet); err != nil {
			return nil, err
		}
		if pr[0].Valid && pr[1].Valid && pr[2].Valid && pr[3].Valid {
			l.HasPressures = true
			for i := range pr {
				l.Pressures[i] = pr[i].Float64
			}
		}
		l.At = fromMS(at)
		if air.Valid && road.Valid {
			l.Air, l.Road, l.HasTemps = air.Float64, road.Float64, true
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LapsForSession returns the laps of one session, oldest first (pit stop advice, D-124).
func (q Q) LapsForSession(sessionID int64) ([]Lap, error) {
	return q.laps(`WHERE l.session_id = ?`, sessionID)
}

// LapsForSetup returns every lap driven with a setup (any version), oldest first.
func (q Q) LapsForSetup(setupID int64) ([]Lap, error) {
	return q.laps(`WHERE l.setup_id = ?`, setupID)
}

// LapsForTrack returns every lap on a car and track (for the fuel calculator).
func (q Q) LapsForTrack(game, car, track string) ([]Lap, error) {
	return q.laps(`WHERE s.game = ? AND s.car = ? AND s.track = ?`, game, car, track)
}

// SessionsForSetup returns the sessions in which a setup was driven, newest first.
func (q Q) SessionsForSetup(setupID int64) ([]Session, error) {
	rows, err := q.ex.Query(`SELECT s.id, s.game, s.car, s.track, s.kind, s.started_at, COALESCE(s.setup_id,0), COALESCE(s.version_id,0), s.match,
		(SELECT COUNT(*) FROM laps l WHERE l.session_id = s.id)
		FROM sessions s WHERE s.id IN (SELECT session_id FROM laps WHERE setup_id = ?) OR s.setup_id = ? ORDER BY s.started_at DESC`, setupID, setupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		var at int64
		if err := rows.Scan(&s.ID, &s.Game, &s.Car, &s.Track, &s.Kind, &at, &s.SetupID, &s.VersionID, &s.Match, &s.Laps); err != nil {
			return nil, err
		}
		s.StartedAt = fromMS(at)
		out = append(out, s)
	}
	return out, rows.Err()
}

// UnassignedSessions returns sessions on a car and track with laps but no known setup, newest first.
func (q Q) UnassignedSessions(game, car, track string) ([]Session, error) {
	rows, err := q.ex.Query(`SELECT s.id, s.game, s.car, s.track, s.kind, s.started_at, s.match,
		(SELECT COUNT(*) FROM laps l WHERE l.session_id = s.id) AS n
		FROM sessions s WHERE s.game = ? AND s.car = ? AND s.track = ? AND s.setup_id IS NULL
		AND EXISTS (SELECT 1 FROM laps l WHERE l.session_id = s.id) ORDER BY s.started_at DESC`, game, car, track)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		var at int64
		if err := rows.Scan(&s.ID, &s.Game, &s.Car, &s.Track, &s.Kind, &at, &s.Match, &s.Laps); err != nil {
			return nil, err
		}
		s.StartedAt = fromMS(at)
		out = append(out, s)
	}
	return out, rows.Err()
}
