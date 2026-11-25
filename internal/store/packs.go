package store

// Data packs (spec D-201): what another SetupMan sent. Imported setups, sessions and telemetry
// files carry the sender's initials (ext, the External badge) and the pack they came from (pack),
// so a pack can be removed again. External laps are left out of every statistic of the user's own
// driving (Q-DP1): queries for best laps, averages, fuel, Race prep and pit loss skip sessions with
// ext <> ''.

import (
	"database/sql"
	"errors"
	"time"
)

// packColumns are added to libraries made before data packs (at every start, like ensureTelemetry).
var packColumns = []struct{ table, name, def string }{
	{"setups", "ext", "TEXT NOT NULL DEFAULT ''"},       // sender's initials; '' = the user's own
	{"setups", "pack", "TEXT NOT NULL DEFAULT ''"},      // the pack that brought it
	{"setups", "pack_key", "TEXT NOT NULL DEFAULT ''"},  // the sender's key of this setup (a newer pack adds versions)
	{"setups", "share_key", "TEXT NOT NULL DEFAULT ''"}, // this library's key when the user exports it
	{"versions", "pack", "TEXT NOT NULL DEFAULT ''"},
	{"sessions", "ext", "TEXT NOT NULL DEFAULT ''"},
	{"sessions", "pack", "TEXT NOT NULL DEFAULT ''"},
	{"telemetry_files", "ext", "TEXT NOT NULL DEFAULT ''"},
	{"telemetry_files", "pack", "TEXT NOT NULL DEFAULT ''"},
}

func ensurePacks(db *sql.DB) error {
	for _, c := range packColumns {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.name + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS packs (
	id       TEXT PRIMARY KEY,
	by       TEXT NOT NULL,
	created  INTEGER NOT NULL,
	imported INTEGER NOT NULL,
	app      TEXT NOT NULL DEFAULT '',
	summary  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS setups_pack_key ON setups(pack_key) WHERE pack_key <> '';`)
	return err
}

// Pack is an imported data pack.
type Pack struct {
	ID       string    `json:"id"`
	By       string    `json:"by"`
	Created  time.Time `json:"created"`
	Imported time.Time `json:"imported"`
	App      string    `json:"app"`
	Summary  string    `json:"summary"` // e.g. "3 setups, 5 laps (Imola, Spa)"
	Setups   int       `json:"setups"`  // still in the library
	Laps     int       `json:"laps"`
}

// SavePack records an imported pack (again: the summary is updated).
func (q Q) SavePack(p Pack) error {
	_, err := q.ex.Exec(`INSERT INTO packs (id, by, created, imported, app, summary) VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET imported = excluded.imported, summary = excluded.summary`,
		p.ID, p.By, ms(p.Created), ms(p.Imported), p.App, p.Summary)
	return err
}

// Packs lists the imported packs, newest import first.
func (q Q) Packs() ([]Pack, error) {
	rows, err := q.ex.Query(`SELECT p.id, p.by, p.created, p.imported, p.app, p.summary,
		(SELECT COUNT(*) FROM setups s WHERE s.pack = p.id AND s.deleted_at IS NULL),
		(SELECT COUNT(*) FROM laps l JOIN sessions x ON x.id = l.session_id WHERE x.pack = p.id)
		FROM packs p ORDER BY p.imported DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pack{}
	for rows.Next() {
		var p Pack
		var c, i int64
		if err := rows.Scan(&p.ID, &p.By, &c, &i, &p.App, &p.Summary, &p.Setups, &p.Laps); err != nil {
			return nil, err
		}
		p.Created, p.Imported = fromMS(c), fromMS(i)
		out = append(out, p)
	}
	return out, rows.Err()
}

// PackKnown reports whether a pack was imported before.
func (q Q) PackKnown(id string) (bool, error) {
	var n int
	err := q.ex.QueryRow(`SELECT COUNT(*) FROM packs WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// SetupByPackKey returns the external setup with this sender's key, or nil.
func (q Q) SetupByPackKey(key string) (*Setup, error) {
	if key == "" {
		return nil, nil
	}
	s, err := scanSetup(q.ex.QueryRow(`SELECT `+setupCols+` FROM setups WHERE pack_key = ? AND deleted_at IS NULL ORDER BY id LIMIT 1`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// ShareKey returns the setup's key for data packs, making one the first time.
func (q Q) ShareKey(setupID int64, newKey func() string) (string, error) {
	var k string
	if err := q.ex.QueryRow(`SELECT share_key FROM setups WHERE id = ?`, setupID).Scan(&k); err != nil {
		return "", err
	}
	if k != "" {
		return k, nil
	}
	k = newKey()
	_, err := q.ex.Exec(`UPDATE setups SET share_key = ? WHERE id = ?`, k, setupID)
	return k, err
}

// MarkTelemetryExternal marks an imported telemetry file with the sender and the pack.
func (q Q) MarkTelemetryExternal(fileID int64, ext, pack string) error {
	_, err := q.ex.Exec(`UPDATE telemetry_files SET ext = ?, pack = ? WHERE id = ?`, ext, pack, fileID)
	return err
}

// SetPackLapNumber gives a lap of a data pack's file the number it had in the sender's session.
func (q Q) SetPackLapNumber(fileID int64, n, lap int) error {
	_, err := q.ex.Exec(`UPDATE telemetry_laps SET sess_n = ? WHERE file_id = ? AND n = ?`, lap, fileID, n)
	return err
}

// PackRemoval is what removing a pack took out of the library: the sessions (their extra logs go
// too) and whether anything was left behind.
type PackRemoval struct {
	Sessions map[int64]string `json:"-"` // session → game
	Setups   int              `json:"setups"`
	Kept     int              `json:"kept"` // setups the user put in the game folder: they stay (as external setups)
	Laps     int              `json:"laps"`
}

// RemovePack removes everything a pack brought: its sessions with their laps and telemetry
// files, the versions it added and the setups it made. A setup from the pack that is in the game
// folder stays (with every version), and laps the user drove with a removed setup stay, without
// a setup (as when a setup record is removed, D-138).
func (s *Store) RemovePack(id string) (PackRemoval, error) {
	r := PackRemoval{Sessions: map[int64]string{}}
	err := s.Tx(func(q Q) error {
		rows, err := q.ex.Query(`SELECT id, game FROM sessions WHERE pack = ?`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sid int64
			var g string
			if err := rows.Scan(&sid, &g); err != nil {
				rows.Close()
				return err
			}
			r.Sessions[sid] = g
		}
		rows.Close()
		if err := q.ex.QueryRow(`SELECT COUNT(*) FROM laps l JOIN sessions x ON x.id = l.session_id WHERE x.pack = ?`, id).Scan(&r.Laps); err != nil {
			return err
		}
		if err := q.ex.QueryRow(`SELECT COUNT(*) FROM setups WHERE pack = ? AND rel_path IS NOT NULL`, id).Scan(&r.Kept); err != nil {
			return err
		}
		if err := q.ex.QueryRow(`SELECT COUNT(*) FROM setups WHERE pack = ? AND rel_path IS NULL`, id).Scan(&r.Setups); err != nil {
			return err
		}
		gone := `(SELECT id FROM setups WHERE pack = ?1 AND rel_path IS NULL)`
		for _, stmt := range []string{
			`DELETE FROM telemetry_files WHERE pack = ?1`,
			`DELETE FROM laps WHERE session_id IN (SELECT id FROM sessions WHERE pack = ?1)`,
			`DELETE FROM sessions WHERE pack = ?1`,
			// versions this pack added to setups that stay (a setup kept in the game folder keeps them)
			`DELETE FROM versions WHERE pack = ?1 AND setup_id NOT IN (SELECT id FROM setups WHERE rel_path IS NOT NULL)
				AND setup_id NOT IN ` + gone,
			`UPDATE laps SET setup_id = NULL, version_id = NULL WHERE setup_id IN ` + gone,
			`UPDATE sessions SET setup_id = NULL, version_id = NULL, match = 'none' WHERE setup_id IN ` + gone,
			`UPDATE telemetry_files SET setup_id = NULL, version_id = NULL, match = 'none' WHERE match = 'manual' AND setup_id IN ` + gone,
			`UPDATE setups SET parent_version_id = NULL, link_kind = 'none' WHERE parent_version_id IN (SELECT v.id FROM versions v WHERE v.setup_id IN ` + gone + `)`,
			`DELETE FROM events WHERE setup_id IN ` + gone,
			`DELETE FROM setups WHERE pack = ?1 AND rel_path IS NULL`,
			`DELETE FROM packs WHERE id = ?1`,
		} {
			if _, err := q.ex.Exec(stmt, id); err != nil {
				return err
			}
		}
		return nil
	})
	return r, err
}
