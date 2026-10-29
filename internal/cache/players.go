package cache

import (
	"database/sql"
	"time"
)

// Player is a scouted account's display name. Names are cosmetic only — they
// never reach the model (§6) — so they are cached locally rather than carried in
// the shared snapshot.
type Player struct {
	AccountID int64
	Persona   string
	AvatarURL string
	FetchedAt int64 // unix seconds
}

// UpsertPlayers inserts or refreshes cached persona names. An empty persona is
// still stored: it records that the lookup ran and came back blank (private
// profile), so the next screen refresh does not re-query it immediately.
func (s *Store) UpsertPlayers(players []Player) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO players(account_id, persona, avatar_url, fetched_at) VALUES(?, ?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET
		   persona = excluded.persona, avatar_url = excluded.avatar_url, fetched_at = excluded.fetched_at`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, p := range players {
		at := p.FetchedAt
		if at == 0 {
			at = now
		}
		if _, err := stmt.Exec(p.AccountID, p.Persona, p.AvatarURL, at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Players returns the cached persona names for the given account ids, keyed by
// id. Ids with no cached row are simply absent from the map.
func (s *Store) Players(ids []int64) (map[int64]Player, error) {
	out := make(map[int64]Player, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// Prepared once and executed per id: the list is a handful of scouted players,
	// so this stays simpler than building a variadic IN clause.
	stmt, err := s.rd.Prepare("SELECT account_id, persona, avatar_url, fetched_at FROM players WHERE account_id = ?")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	for _, id := range ids {
		var p Player
		err := stmt.QueryRow(id).Scan(&p.AccountID, &p.Persona, &p.AvatarURL, &p.FetchedAt)
		if err != nil {
			continue // not cached yet (or unreadable): the caller will fetch it
		}
		out[p.AccountID] = p
	}
	return out, nil
}

// migratePlayerAvatars adds avatar metadata without invalidating older cached names.
func migratePlayerAvatars(tx *sql.Tx) error {
	return addColumns(tx, "players", []struct{ name, ddl string }{
		{"avatar_url", "ALTER TABLE players ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''"},
	})
}
