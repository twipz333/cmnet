package cache

import (
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

// A freshly created database must be reported as current on the next open, so the
// schema DDL (and its write lock) is skipped on the common path.
func TestSchemaCurrentAfterFirstOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmnet.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !schemaCurrent(s.db) {
		t.Error("a database just created by Open is not recognised as current; every open would redo the DDL")
	}
	s.Close()
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
