package cache

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The db path comes from a flag or config file. Characters that are URI syntax
// must not leak into the DSN, or the driver opens a different file than the user
// asked for — silently, since a missing SQLite file is simply created.
func TestDSNEscapesPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		// substrings that must NOT appear raw in the DSN's path portion
		forbidden []string
	}{
		{"question mark", `C:\dota data\cache?x.db`, []string{"?x.db"}},
		{"hash", `C:\dota#1\cmnet.db`, []string{"#1"}},
		{"percent", `C:\100%\cmnet.db`, []string{"100%\\"}},
		{"spaces are fine but encoded", `C:\Program Files\cmnet.db`, []string{"Program Files"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := dsnFor(tt.path)
			pathPart := dsn
			if i := strings.Index(dsn, "?_pragma"); i >= 0 {
				pathPart = dsn[:i]
			}
			for _, bad := range tt.forbidden {
				if strings.Contains(pathPart, bad) {
					t.Errorf("DSN path %q still contains unescaped %q", pathPart, bad)
				}
			}
			if !strings.Contains(dsn, "journal_mode") || !strings.Contains(dsn, "busy_timeout") {
				t.Errorf("DSN lost its pragmas: %q", dsn)
			}
		})
	}
}

// Opening a database must work for paths with characters that need escaping, and
// the data written through one handle must be readable through another.
func TestOpenHandlesAwkwardPaths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"plain.db", "with space.db", "with#hash.db", "with%pct.db"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			s, err := Open(path)
			if err != nil {
				t.Fatalf("open %q: %v", path, err)
			}
			if err := s.UpsertHeroes([]Hero{{ID: 1, Name: "npc_a", Localized: "A"}}); err != nil {
				t.Fatalf("write: %v", err)
			}
			s.Close()

			s2, err := Open(path)
			if err != nil {
				t.Fatalf("reopen %q: %v", path, err)
			}
			defer s2.Close()
			heroes, err := s2.Heroes()
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(heroes) != 1 || heroes[0].Localized != "A" {
				t.Errorf("data did not survive reopen: %+v (a different file may have been opened)", heroes)
			}
		})
	}
}

// A freshly created database must land on the current schema version, so the next
// open skips the DDL — and its write lock — entirely.
func TestFreshDatabaseIsAtCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmnet.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	got, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if got != schemaVersion() {
		t.Errorf("fresh database at version %d, want %d; every open would redo the DDL", got, schemaVersion())
	}
}

// legacySchema is the cache as it existed before the migration ladder: no
// user_version, no provenance columns on matches.
const legacySchema = `
CREATE TABLE heroes (id INTEGER PRIMARY KEY, name TEXT NOT NULL, localized TEXT NOT NULL DEFAULT '');
CREATE TABLE matches (
    match_id    INTEGER PRIMARY KEY,
    start_time  INTEGER NOT NULL,
    radiant_win INTEGER NOT NULL,
    avg_mmr     INTEGER,
    patch       TEXT NOT NULL DEFAULT '',
    fetched_at  INTEGER NOT NULL
);
CREATE TABLE pick_bans (
    match_id INTEGER NOT NULL, ord INTEGER NOT NULL, hero_id INTEGER NOT NULL,
    is_pick INTEGER NOT NULL, team INTEGER NOT NULL, PRIMARY KEY (match_id, ord));
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE players (account_id INTEGER PRIMARY KEY, persona TEXT NOT NULL DEFAULT '', fetched_at INTEGER NOT NULL);
CREATE TABLE match_players (
    match_id INTEGER NOT NULL, player_slot INTEGER NOT NULL, account_id INTEGER,
    hero_id INTEGER NOT NULL, is_radiant INTEGER NOT NULL, position INTEGER,
    pos_source TEXT NOT NULL DEFAULT '', PRIMARY KEY (match_id, player_slot));

-- Two matches: one with a ban row (a pro draft), one without (the bulk scan).
INSERT INTO matches(match_id, start_time, radiant_win, fetched_at) VALUES (10, 100, 1, 1), (11, 200, 0, 1);
INSERT INTO pick_bans(match_id, ord, hero_id, is_pick, team) VALUES (10, 0, 5, 0, 0);
`

// newLegacyDB writes a pre-ladder cache and returns its path.
func newLegacyDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cmnet.db")
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		t.Fatalf("create legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	return path
}

// A cache created before the ladder must migrate in place: version stamped, new
// columns added, existing matches untouched, provenance inferred. Getting this
// wrong destroys a mature cache that took days of API budget to build.
func TestLegacyDatabaseMigrates(t *testing.T) {
	path := newLegacyDB(t)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer s.Close()

	ver, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if ver != schemaVersion() {
		t.Errorf("legacy db at version %d after migration, want %d", ver, schemaVersion())
	}

	n, err := s.CountMatches()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("match count = %d after migration, want 2: the migration lost rows", n)
	}

	matches, err := s.Matches(0)
	if err != nil {
		t.Fatalf("read matches: %v", err)
	}
	got := map[int64]string{}
	for _, m := range matches {
		got[m.ID] = m.Source
	}
	if got[10] != SourcePro {
		t.Errorf("match with a ban row labelled %q, want %q", got[10], SourcePro)
	}
	if got[11] != SourceBulk {
		t.Errorf("match without ban rows labelled %q, want %q", got[11], SourceBulk)
	}
}

// The backfill infers provenance from the shape of old rows, so it is only correct
// the first time. Re-running it would relabel matches that have since been
// re-synced with real provenance, which is why it carries a meta guard.
func TestBackfillRunsOnlyOnce(t *testing.T) {
	path := newLegacyDB(t)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Simulate a later re-sync that recorded real provenance for the pro match.
	if _, err := s.db.Exec("UPDATE matches SET source = 'public-cm' WHERE match_id = 10"); err != nil {
		t.Fatalf("relabel: %v", err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s2.Close()

	var src string
	if err := s2.rd.QueryRow("SELECT source FROM matches WHERE match_id = 10").Scan(&src); err != nil {
		t.Fatalf("read source: %v", err)
	}
	if src != SourcePublicCM {
		t.Errorf("source = %q after reopen, want %q: the backfill ran a second time and overwrote real provenance", src, SourcePublicCM)
	}
}

// A cache written by a newer build must be refused with an explanation rather than
// used with columns this build cannot see.
func TestFutureSchemaVersionRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmnet.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion()+1)); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	s.Close()

	if _, err := Open(path); err == nil {
		t.Error("a database from a newer build opened without complaint")
	} else if !strings.Contains(err.Error(), "newer than this build") {
		t.Errorf("unhelpful error for a future schema: %v", err)
	}
}

// Players round-trips through the cache, including the "asked, no public profile"
// empty-name case that stops the UI from re-querying private accounts.
func TestPlayersRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cmnet.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if err := s.UpsertPlayers([]Player{
		{AccountID: 1, Persona: "Dendi"},
		{AccountID: 2, Persona: ""},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.Players([]int64{1, 2, 3})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got[1].Persona != "Dendi" {
		t.Errorf("persona = %q, want Dendi", got[1].Persona)
	}
	if p, ok := got[2]; !ok || p.Persona != "" {
		t.Error("an empty persona must be cached, otherwise private accounts are re-queried forever")
	}
	if p, ok := got[2]; ok && p.FetchedAt == 0 {
		t.Error("fetched_at was not stamped, so the TTL can never expire it")
	}
	if _, ok := got[3]; ok {
		t.Error("an uncached id must be absent from the map, not present and empty")
	}
}
