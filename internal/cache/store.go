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
//
// It holds two pools over the same file. modernc serializes writes, so the writer
// is pinned to a single connection; readers get their own pool because under WAL a
// reader neither blocks nor is blocked by the writer. Sharing one connection for
// both — which is what a single pool with SetMaxOpenConns(1) amounts to — made
// every read queue behind whatever long sync happened to be writing, which is the
// stall the background collection was blamed for.
type Store struct {
	db *sql.DB // writer: serialized, owns every Exec and Begin
	rd *sql.DB // readers: concurrent, owns every Query and QueryRow
}

// busyTimeout is how long a connection waits for a lock held by another one
// before giving up. The tool routinely has two writers on the same file — a long
// sync in the background while the user opens the Statistics screen — and schema
// work needs the write lock. Without this the second one fails instantly with
// "database is locked"; with it, it simply waits its turn.
const busyTimeout = 10 * time.Second

// maxReaders caps the reader pool. Reads are short and the workload is a handful
// of concurrent screens plus the background worker, so this is about bounding file
// handles rather than throughput.
const maxReaders = 4

// Open opens (creating if needed) the cache at path and brings its schema up to
// date.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// modernc serializes writes; a single connection avoids "database is locked".
	db.SetMaxOpenConns(1)
	if err := applySchema(db); err != nil {
		db.Close()
		return nil, err
	}

	rd, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open sqlite reader %q: %w", path, err)
	}
	rd.SetMaxOpenConns(maxReaders)
	return &Store{db: db, rd: rd}, nil
}

// dsnFor builds the connection URI.
//
// WAL lets readers work while a sync is writing instead of each side locking the
// whole file; busy_timeout covers the write-vs-write case that WAL does not.
// foreign_keys is here rather than in a PRAGMA statement because it is per
// connection: run as a statement it would apply only to whichever connection
// happened to execute it, which silently disables cascade deletes on every other
// one in the pool.
//
// The path comes from a flag or config file and may contain characters that are
// syntax in a URI ("?", "#", "%") or Windows separators, so it is escaped rather
// than interpolated — an unescaped "?" would turn the rest of the path into query
// parameters and silently open a different database.
func dsnFor(path string) string {
	esc := (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
	return "file:" + esc +
		"?_pragma=" + url.QueryEscape(fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds())) +
		"&_pragma=" + url.QueryEscape("journal_mode(WAL)") +
		"&_pragma=" + url.QueryEscape("foreign_keys(1)")
}

// migration is one ordered, versioned change to the schema.
//
// The ladder replaced a flat list of ALTER statements guarded by "does this column
// exist" probes. That worked while there were three columns; it cannot answer
// "which version is this database" or "has this change been applied", and with a
// dozen columns and several new tables arriving it stops being checkable at all.
// PRAGMA user_version is SQLite's own slot for exactly this and costs no table.
type migration struct {
	version int
	name    string
	apply   func(*sql.Tx) error
}

// baselineVersion is the schema version that schema.sql alone produces. A freshly
// created database jumps straight to it instead of replaying the migrations whose
// end state the baseline DDL already contains.
const baselineVersion = 1

// migrations must stay ordered by version and append-only: released versions are
// recorded in user databases, so editing one in place makes the number lie.
//
// Anything that adds a column or a table after baselineVersion belongs here.
// schema.sql stays the baseline for a brand-new file and is not the place to add
// changes: it never runs against a database that already has its tables.
var migrations = []migration{
	{1, "provenance columns on matches", migrateProvenance},
}

// schemaVersion is the version a fully migrated database reports.
func schemaVersion() int {
	if len(migrations) == 0 {
		return baselineVersion
	}
	return migrations[len(migrations)-1].version
}

// applySchema brings the database up to schemaVersion().
//
// A database already at the current version is left completely alone: no DDL, no
// write lock, which matters because opening the cache is something the UI does
// while a long sync holds that lock.
func applySchema(db *sql.DB) error {
	have, err := userVersion(db)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if have == schemaVersion() {
		return nil
	}
	if have > schemaVersion() {
		return fmt.Errorf(
			"cache schema version %d is newer than this build understands (%d): "+
				"the file was written by a later advisor", have, schemaVersion())
	}

	if have == 0 {
		// user_version 0 means either a brand-new file or a cache created before the
		// ladder existed. Both need the baseline DDL — every statement in it is
		// CREATE ... IF NOT EXISTS, so on the mature cache it is a no-op — but only
		// the brand-new one may skip the migrations, because the mature one predates
		// them.
		fresh, err := isFresh(db)
		if err != nil {
			return err
		}
		if _, err := db.Exec(schemaSQL); err != nil {
			return fmt.Errorf("apply baseline schema: %w", err)
		}
		if fresh {
			have = baselineVersion
		}
	}

	for _, m := range migrations {
		if m.version <= have {
			continue
		}
		if err := runMigration(db, m); err != nil {
			return err
		}
		have = m.version
	}
	return setUserVersion(db, schemaVersion())
}

// runMigration applies one migration in its own transaction, so a failure leaves
// the database at the previous version rather than half-changed.
func runMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d (%s): begin: %w", m.version, m.name, err)
	}
	defer tx.Rollback()
	if err := m.apply(tx); err != nil {
		return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %d (%s): commit: %w", m.version, m.name, err)
	}
	return nil
}

// isFresh reports whether the file has no cache tables yet, i.e. nothing predating
// the migration ladder can be hiding in it.
func isFresh(db *sql.DB) (bool, error) {
	var name string
	err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='matches'").Scan(&name)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func userVersion(db *sql.DB) (int, error) {
	var v int
	// PRAGMA takes no bind parameters, and this value never comes from user input.
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

func setUserVersion(db *sql.DB, v int) error {
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
		return fmt.Errorf("set schema version %d: %w", v, err)
	}
	return nil
}

// migrateProvenance adds the game_mode / rank_tier / source columns and labels the
// matches cached before they existed.
//
// Without the backfill the columns are technically present and practically empty:
// ~92% of a mature cache carries source='' and the training weights that key off
// source (pro drafts as auxiliary data) silently apply to almost nothing. The
// settings would look configured while doing nothing at all — worse than not
// having them.
//
// The labels are inferred, not guessed. Until these columns existed there were
// exactly two ways a match entered the cache: full draft detail fetched from
// OpenDota's /proMatches (those carry ban rows), and the picks-only public_matches
// bulk scan (those never do). So "has a ban row" identifies the pro pool exactly,
// for rows written before this point. Rank tier cannot be recovered and stays
// NULL, which the training filters treat as "unknown, keep".
//
// The meta guard predates the ladder and is kept: databases carrying it already
// ran the backfill under the old flat migration, and re-running it would relabel
// matches that have since been re-synced with real provenance.
func migrateProvenance(tx *sql.Tx) error {
	have, err := txColumns(tx, "matches")
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
		if _, err := tx.Exec(a.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", a.name, err)
		}
	}

	const doneKey = "provenance_backfilled"
	var done string
	err = tx.QueryRow("SELECT value FROM meta WHERE key = ?", doneKey).Scan(&done)
	if err == nil {
		return nil // already applied under the pre-ladder migration
	}
	if err != sql.ErrNoRows {
		return err
	}

	if _, err := tx.Exec(`
		UPDATE matches SET source = 'pro', game_mode = 2
		WHERE source = ''
		  AND EXISTS (SELECT 1 FROM pick_bans WHERE pick_bans.match_id = matches.match_id AND is_pick = 0)`,
	); err != nil {
		return fmt.Errorf("backfill pro provenance: %w", err)
	}
	if _, err := tx.Exec(`UPDATE matches SET source = 'bulk' WHERE source = ''`); err != nil {
		return fmt.Errorf("backfill bulk provenance: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO meta(key, value) VALUES(?, '1')
	                  ON CONFLICT(key) DO UPDATE SET value = '1'`, doneKey)
	return err
}

// txColumns returns the column names of a table, inside a migration transaction.
func txColumns(tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.Query("SELECT name FROM pragma_table_info(?)", table)
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

// addColumns adds the columns a migration needs, skipping ones already present.
// SQLite has no "ADD COLUMN IF NOT EXISTS", and a migration may run against a
// database that took the legacy path where some columns already exist.
func addColumns(tx *sql.Tx, table string, cols []struct{ name, ddl string }) error {
	have, err := txColumns(tx, table)
	if err != nil {
		return err
	}
	for _, c := range cols {
		if have[c.name] {
			continue
		}
		if _, err := tx.Exec(c.ddl); err != nil {
			return fmt.Errorf("add column %s.%s: %w", table, c.name, err)
		}
	}
	return nil
}

// Close closes both pools.
func (s *Store) Close() error {
	rerr := s.rd.Close()
	werr := s.db.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

func isNoRows(err error) bool { return err == sql.ErrNoRows }

// GetMeta returns the value for key, or ("", false) if absent.
func (s *Store) GetMeta(key string) (string, bool, error) {
	var v string
	err := s.rd.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v)
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

// SchemaVersion reports the migration version the open database is at. Exposed for
// diagnostics — the Statistics screen shows it next to the cache path so a "why is
// this column empty" question can be answered without a SQLite client.
func (s *Store) SchemaVersion() (int, error) { return userVersion(s.rd) }
