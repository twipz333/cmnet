package cache

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Per-player aggregates live in the cache and never in the bundle.
//
// They describe specific people from whatever this machine happened to collect,
// so baking them into a shared artifact would make two friends' bundles differ by
// who had scouted whom — exactly the divergence §6 exists to prevent. The bundle
// carries the meta; a snapshot carries the opponents; this table is the local
// working set that feeds both.

// PlayerHeroRow is one account's record on one hero, as stored.
type PlayerHeroRow struct {
	AccountID int64
	HeroID    int
	Matches   int
	Wins      int
	// Window is the collection window these counts were computed over, in days.
	// Stored because a cached row computed over 30 days must not answer a question
	// asked about 7 — a cache that ignores its window silently returns the wrong
	// population and looks fast doing it.
	Window    int
	UpdatedAt int64
}

// PlayerPosRow is one account's record in one lane position.
type PlayerPosRow struct {
	AccountID int64
	Position  int
	Matches   int
	Window    int
	UpdatedAt int64
}

func migratePlayerStats(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS player_hero_stats (
			account_id INTEGER NOT NULL,
			hero_id    INTEGER NOT NULL,
			matches    INTEGER NOT NULL,
			wins       INTEGER NOT NULL,
			window_days INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (account_id, hero_id, window_days)
		)`,
		`CREATE TABLE IF NOT EXISTS player_position_stats (
			account_id INTEGER NOT NULL,
			position   INTEGER NOT NULL,
			matches    INTEGER NOT NULL,
			window_days INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (account_id, position, window_days)
		)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("create player aggregates: %w", err)
		}
	}
	return nil
}

// ReplacePlayerHeroStats rewrites one account's hero rows for a window.
//
// Replace rather than merge: these are recomputed from the corpus, so a stale row
// for a hero the player has not touched in the window must disappear rather than
// linger at its old count.
func (s *Store) ReplacePlayerHeroStats(accountID int64, window int, rows []PlayerHeroRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM player_hero_stats WHERE account_id = ? AND window_days = ?`,
		accountID, window); err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO player_hero_stats(account_id, hero_id, matches, wins, window_days, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, r := range rows {
		if _, err := stmt.Exec(accountID, r.HeroID, r.Matches, r.Wins, window, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplacePlayerPositionStats rewrites one account's position rows for a window.
func (s *Store) ReplacePlayerPositionStats(accountID int64, window int, rows []PlayerPosRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM player_position_stats WHERE account_id = ? AND window_days = ?`,
		accountID, window); err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO player_position_stats(account_id, position, matches, window_days, updated_at)
		 VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, r := range rows {
		if _, err := stmt.Exec(accountID, r.Position, r.Matches, window, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PlayerHeroStats returns an account's hero rows for a window, most played first.
//
// The window must match exactly. A row collected over 30 days is not an answer to
// a question about 7: returning it would be a cache hit that quietly changes the
// population being described.
func (s *Store) PlayerHeroStats(accountID int64, window int) ([]PlayerHeroRow, error) {
	rows, err := s.rd.Query(
		`SELECT account_id, hero_id, matches, wins, window_days, updated_at
		 FROM player_hero_stats WHERE account_id = ? AND window_days = ?
		 ORDER BY matches DESC, hero_id`, accountID, window)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlayerHeroRow
	for rows.Next() {
		var r PlayerHeroRow
		if err := rows.Scan(&r.AccountID, &r.HeroID, &r.Matches, &r.Wins, &r.Window, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlayerPositionStats returns an account's position rows for a window.
func (s *Store) PlayerPositionStats(accountID int64, window int) ([]PlayerPosRow, error) {
	rows, err := s.rd.Query(
		`SELECT account_id, position, matches, window_days, updated_at
		 FROM player_position_stats WHERE account_id = ? AND window_days = ?
		 ORDER BY matches DESC, position`, accountID, window)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlayerPosRow
	for rows.Next() {
		var r PlayerPosRow
		if err := rows.Scan(&r.AccountID, &r.Position, &r.Matches, &r.Window, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlayerStatsAge returns how old an account's cached rows are for a window, and
// whether any exist. Callers decide their own freshness policy; the store only
// reports the fact.
func (s *Store) PlayerStatsAge(accountID int64, window int) (time.Duration, bool, error) {
	var updated int64
	err := s.rd.QueryRow(
		`SELECT MAX(updated_at) FROM player_hero_stats WHERE account_id = ? AND window_days = ?`,
		accountID, window).Scan(&updated)
	if err == sql.ErrNoRows || updated == 0 {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return time.Since(time.Unix(updated, 0)), true, nil
}

// RebuildPlayerStats recomputes the aggregates for every account seen in the
// window and stores them.
//
// One pass over the participant rows rather than a query per account: on a mature
// cache there are tens of thousands of accounts, and the per-account version would
// be tens of thousands of queries to answer one screen.
func (s *Store) RebuildPlayerStats(window int) (accounts int, err error) {
	if window <= 0 {
		return 0, fmt.Errorf("window must be positive")
	}
	since := time.Now().AddDate(0, 0, -window).Unix()

	rows, err := s.rd.Query(
		`SELECT mp.account_id, mp.hero_id, mp.position, mp.is_radiant, m.radiant_win
		 FROM match_players mp
		 JOIN matches m ON m.match_id = mp.match_id
		 WHERE mp.account_id IS NOT NULL AND m.start_time >= ?
		 ORDER BY mp.account_id, mp.match_id, mp.player_slot`, since)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type heroKey struct {
		acct int64
		hero int
	}
	type posKey struct {
		acct int64
		pos  int
	}
	heroCounts := map[heroKey]*PlayerHeroRow{}
	posCounts := map[posKey]*PlayerPosRow{}
	seen := map[int64]bool{}

	for rows.Next() {
		var acct int64
		var hero int
		var pos *int
		var isRad, radWin int
		if err := rows.Scan(&acct, &hero, &pos, &isRad, &radWin); err != nil {
			return 0, err
		}
		seen[acct] = true
		win := (isRad != 0) == (radWin != 0)

		hk := heroKey{acct: acct, hero: hero}
		if heroCounts[hk] == nil {
			heroCounts[hk] = &PlayerHeroRow{AccountID: acct, HeroID: hero}
		}
		heroCounts[hk].Matches++
		if win {
			heroCounts[hk].Wins++
		}
		if pos != nil {
			pk := posKey{acct: acct, pos: *pos}
			if posCounts[pk] == nil {
				posCounts[pk] = &PlayerPosRow{AccountID: acct, Position: *pos}
			}
			posCounts[pk].Matches++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	byAcctHero := map[int64][]PlayerHeroRow{}
	for k, v := range heroCounts {
		byAcctHero[k.acct] = append(byAcctHero[k.acct], *v)
	}
	byAcctPos := map[int64][]PlayerPosRow{}
	for k, v := range posCounts {
		byAcctPos[k.acct] = append(byAcctPos[k.acct], *v)
	}

	// Accounts are written in id order so a run that fails halfway leaves a
	// predictable prefix rather than an arbitrary subset.
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		hr := byAcctHero[id]
		sort.SliceStable(hr, func(i, j int) bool {
			if hr[i].Matches != hr[j].Matches {
				return hr[i].Matches > hr[j].Matches
			}
			return hr[i].HeroID < hr[j].HeroID
		})
		if err := s.ReplacePlayerHeroStats(id, window, hr); err != nil {
			return accounts, err
		}
		pr := byAcctPos[id]
		sort.SliceStable(pr, func(i, j int) bool {
			if pr[i].Matches != pr[j].Matches {
				return pr[i].Matches > pr[j].Matches
			}
			return pr[i].Position < pr[j].Position
		})
		if err := s.ReplacePlayerPositionStats(id, window, pr); err != nil {
			return accounts, err
		}
		accounts++
	}
	return accounts, nil
}

// ForgetPlayerStats drops every cached row for an account, in every window.
func (s *Store) ForgetPlayerStats(accountID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM player_hero_stats WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM player_position_stats WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}
