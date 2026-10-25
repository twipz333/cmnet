package cache

import (
	"fmt"
)

// PatchWindow is one [Start,End) interval labelled with a human patch tag.
// End=0 means open-ended (current patch).
type PatchWindow struct {
	Name  string
	Start int64
	End   int64
}

// BackfillPatches labels matches that still have an empty patch using start_time
// windows from OpenDota's constant table. Also rewrites pure-numeric patch ids
// (legacy sync stored strconv.Itoa of OpenDota's id) when idToName is provided.
//
// Returns how many rows were updated.
func (s *Store) BackfillPatches(windows []PatchWindow, idToName map[int]string) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("nil store")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	updated := 0
	for _, w := range windows {
		if w.Name == "" {
			continue
		}
		var res interface{ RowsAffected() (int64, error) }
		var e error
		if w.End > 0 {
			res, e = tx.Exec(
				`UPDATE matches SET patch = ? WHERE (patch = '' OR patch IS NULL)
				   AND start_time >= ? AND start_time < ?`,
				w.Name, w.Start, w.End,
			)
		} else {
			res, e = tx.Exec(
				`UPDATE matches SET patch = ? WHERE (patch = '' OR patch IS NULL)
				   AND start_time >= ?`,
				w.Name, w.Start,
			)
		}
		if e != nil {
			return updated, e
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}

	// Legacy: patch column holds OpenDota numeric id as decimal string.
	for id, name := range idToName {
		if name == "" || id <= 0 {
			continue
		}
		res, e := tx.Exec(`UPDATE matches SET patch = ? WHERE patch = ?`, name, fmt.Sprintf("%d", id))
		if e != nil {
			return updated, e
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}

	if err := tx.Commit(); err != nil {
		return updated, err
	}
	return updated, nil
}

// DominantPatch returns the most common non-empty patch tag among matches.
// Tie-break: lexicographically smaller tag (deterministic).
func DominantPatch(matches []Match) string {
	counts := map[string]int{}
	for _, m := range matches {
		if m.Patch == "" {
			continue
		}
		counts[m.Patch]++
	}
	best, bestN := "", 0
	for tag, n := range counts {
		if n > bestN || (n == bestN && (best == "" || tag < best)) {
			best, bestN = tag, n
		}
	}
	return best
}
