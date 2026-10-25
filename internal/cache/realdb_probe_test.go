package cache

import (
	"os"
	"testing"
)

// TestRealDatabaseMigrates runs the migration ladder against a copy of a real,
// mature cache. Opt-in: set CMNET_REAL_DB to a copy (never the original).
func TestRealDatabaseMigrates(t *testing.T) {
	path := os.Getenv("CMNET_REAL_DB")
	if path == "" {
		t.Skip("set CMNET_REAL_DB to a copy of a real cache to run this")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open real cache: %v", err)
	}
	defer s.Close()

	ver, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	n, err := s.CountMatches()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	var backfilled string
	if err := s.rd.QueryRow("SELECT value FROM meta WHERE key = 'provenance_backfilled'").
		Scan(&backfilled); err != nil {
		t.Logf("provenance guard absent: %v", err)
	}
	var srcCounts string
	rows, err := s.rd.Query("SELECT source, COUNT(*) FROM matches GROUP BY source ORDER BY source")
	if err != nil {
		t.Fatalf("source counts: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var src string
		var c int64
		if err := rows.Scan(&src, &c); err != nil {
			t.Fatal(err)
		}
		srcCounts += " " + src + "=" + itoa(c)
	}
	t.Logf("version=%d matches=%d guard=%q sources:%s", ver, n, backfilled, srcCounts)
	if n == 0 {
		t.Error("real cache reports zero matches after migration")
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
