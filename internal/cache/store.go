// Package cache is the local SQLite match cache backed by the pure-Go
// modernc.org/sqlite driver (no CGO, keeps the static Windows build intact).
package cache

import (
	"database/sql"
	_ "embed"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the SQLite database holding cached matches and metadata.
type Store struct {
	db *sql.DB
}

// busyTimeout is how long a connection waits for a lock held by another one
// before giving up. The tool routinely has two writers on the same file — a long
// sync in the background while the user opens the Statistics screen — and every
// Open below runs DDL, which needs the write lock. Without this the second one
// fails instantly with "database is locked"; with it, it simply waits its turn.
const busyTimeout = 10 * time.Second

// Open opens (creating if needed) the cache at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// modernc serializes writes; a single connection avoids "database is locked".
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	// Applying the schema is DDL and takes the write lock, so on a database that a
	// long sync is busy writing it means waiting out busyTimeout — for something as
	// ordinary as opening the Statistics screen. A database that already has the
	// full schema skips it.
	if !schemaCurrent(db) {
		if _, err := db.Exec(schemaSQL); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
		if err := migrate(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate schema: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// dsnFor builds the connection URI.
//
// WAL lets readers work while a sync is writing instead of each side locking the
// whole file; busy_timeout covers the write-vs-write case that WAL does not. Both
// go in the DSN so they apply to every connection in the pool, not just the one
// that happened to run a PRAGMA statement.
//
// The path comes from a flag or config file and may contain characters that are
// syntax in a URI ("?", "#", "%") or Windows separators, so it is escaped rather
// than interpolated — an unescaped "?" would turn the rest of the path into query
// parameters and silently open a different database.
func dsnFor(path string) string {
	esc := (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
	return "file:" + esc +
		"?_pragma=" + url.QueryEscape(fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds())) +
		"&_pragma=" + url.QueryEscape("journal_mode(WAL)")
}

// schemaTables lists every table schema.sql creates; their presence is what makes
// a database "already set up".
var schemaTables = []string{"heroes", "matches", "pick_bans", "meta", "players", "match_players"}

// schemaCurrent reports whether the database already carries the full schema, so
// the DDL — and the write lock it needs — can be skipped. It is a pure read, which
// under WAL neither blocks nor is blocked by a concurrent writer. Any doubt answers
// false: re-applying the schema is idempotent, wrongly skipping it is not.
func schemaCurrent(db *sql.DB) bool {
	for _, t := range schemaTables {
		var name string
		if err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", t).Scan(&name); err != nil {
			return false
		}
	}
	// The provenance columns arrive through migrate(), not schema.sql, so a cache
	// predating them still has to take the migration path.
	have, err := columns(db, "matches")
	if err != nil {
		return false
	}
	for _, c := range []string{"game_mode", "rank_tier", "source"} {
		if !have[c] {
			return false
		}
	}
	return true
}

// migrate adds columns introduced after a cache was first created. SQLite has no
// "ADD COLUMN IF NOT EXISTS", so existing columns are read first and only the
// missing ones are added. Existing rows keep the column defaults, which read as
// "unknown provenance" — correct, since those matches were cached before the
// pipeline recorded it.
func migrate(db *sql.DB) error {
	have, err := columns(db, "matches")
	if err != nil {
		return err
	}
	adds := []struct{ name, ddl string }{
		{"game_mode", "ALTER TABLE matches ADD COLUMN game_mode INTEGER NOT NULL DEFAULT 0"},
		{"rank_tier", "ALTER TABLE matches ADD COLUMN rank_tier INTEGER"},
		{"source", "ALTER TABLE matches ADD COLUMN source TEXT NOT NULL DEFAULT ''"},
	}
	for _, a := range adds {
		if have[a.name] {
			continue
		}
		if _, err := db.Exec(a.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", a.name, err)
		}
	}
	return backfillProvenance(db)
}

// backfillProvenance labels matches cached before provenance columns existed.
//
// Without this the columns are technically present and practically empty: ~92% of
// a mature cache carries source='' and the training weights that key off source
// (pro drafts as auxiliary data) silently apply to almost nothing. The settings
// would look configured while doing nothing at all — worse than not having them.
//
// The labels are inferred, not guessed. Until these columns existed there were
// exactly two ways a match entered the cache: full draft detail fetched from
// OpenDota's /proMatches (those carry ban rows), and the picks-only public_matches
// bulk scan (those never do). So "has a ban row" identifies the pro pool exactly,
// for rows written before this point. Rank tier cannot be recovered and stays
// NULL, which the training filters treat as "unknown, keep".
//
// Runs once, guarded by a meta key, and never touches rows already labeled.
func backfillProvenance(db *sql.DB) error {
	const doneKey = "provenance_backfilled"
	var done string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", doneKey).Scan(&done)
	if err == nil {
		return nil // already applied
	}
	if err != sql.ErrNoRows {
		return err
	}

	if _, err := db.Exec(`
		UPDATE matches SET source = 'pro', game_mode = 2
		WHERE source = ''
		  AND EXISTS (SELECT 1 FROM pick_bans WHERE pick_bans.match_id = matches.match_id AND is_pick = 0)`,
	); err != nil {
		return fmt.Errorf("backfill pro provenance: %w", err)
	}
	if _, err := db.Exec(`UPDATE matches SET source = 'bulk' WHERE source = ''`); err != nil {
		return fmt.Errorf("backfill bulk provenance: %w", err)
	}
	_, err = db.Exec(`INSERT INTO meta(key, value) VALUES(?, '1')
	                  ON CONFLICT(key) DO UPDATE SET value = '1'`, doneKey)
	return err
}

// columns returns the column names of a table.
func columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
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
		out[name] = true
	}
	return out, rows.Err()
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func isNoRows(err error) bool { return err == sql.ErrNoRows }

// GetMeta returns the value for key, or ("", false) if absent.
func (s *Store) GetMeta(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetMeta upserts a metadata key/value.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}
