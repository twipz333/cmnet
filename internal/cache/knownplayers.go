package cache

import (
	"database/sql"
	"fmt"
	"time"
)

// Known players are the people this machine has scouted or played with.
//
// They live in the cache, never in the snapshot. The snapshot is hashed and shared
// so a whole team blends on identical numbers; adding a nickname to it would change
// that hash and split the file teammates are supposed to agree on (the same reason
// the players table already gives for keeping personas out). This table is the
// local address book: it makes "scout my usual five" a click instead of five
// profile URLs, and it survives a player being removed from the snapshot.

// KnownRole says how we have seen this account.
type KnownRole string

const (
	// RoleAlly is someone who plays with us — the teammates list.
	RoleAlly KnownRole = "ally"
	// RoleEnemy is someone we have scouted as an opponent.
	RoleEnemy KnownRole = "enemy"
)

// KnownPlayer is one remembered account.
type KnownPlayer struct {
	AccountID int64
	Persona   string
	Role      KnownRole
	// Note is the user's own label ("Egor, usually mid"), never touched by the app.
	Note string
	// LastSeen is when this account was last scouted or picked from the list.
	LastSeen int64
	// Pinned keeps an entry out of the automatic pruning: the regular five stay,
	// the hundred opponents scouted once do not.
	Pinned bool
}

func migrateKnownPlayers(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS known_players (
			account_id INTEGER PRIMARY KEY,
			persona    TEXT    NOT NULL DEFAULT '',
			role       TEXT    NOT NULL DEFAULT 'enemy',
			note       TEXT    NOT NULL DEFAULT '',
			last_seen  INTEGER NOT NULL,
			pinned     INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_known_players_role ON known_players(role, last_seen DESC)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("create known players: %w", err)
		}
	}
	return nil
}

// RememberPlayer records or refreshes an account.
//
// A persona or note already stored is not overwritten by an empty one: scouting
// refreshes last_seen and may not resolve the nickname, and losing a label the
// user typed because a lookup came back blank is exactly the kind of quiet data
// loss that makes a feature untrustworthy.
func (s *Store) RememberPlayer(p KnownPlayer) error {
	if p.LastSeen == 0 {
		p.LastSeen = time.Now().Unix()
	}
	if p.Role == "" {
		p.Role = RoleEnemy
	}
	_, err := s.db.Exec(
		`INSERT INTO known_players(account_id, persona, role, note, last_seen, pinned)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET
		   persona = CASE WHEN excluded.persona = '' THEN known_players.persona ELSE excluded.persona END,
		   role = excluded.role,
		   note = CASE WHEN excluded.note = '' THEN known_players.note ELSE excluded.note END,
		   last_seen = MAX(known_players.last_seen, excluded.last_seen),
		   pinned = MAX(known_players.pinned, excluded.pinned)`,
		p.AccountID, p.Persona, string(p.Role), p.Note, p.LastSeen, b2i(p.Pinned))
	return err
}

// KnownPlayers returns remembered accounts, most recently seen first. An empty
// role returns both kinds.
func (s *Store) KnownPlayers(role KnownRole) ([]KnownPlayer, error) {
	q := `SELECT account_id, persona, role, note, last_seen, pinned FROM known_players`
	var args []any
	if role != "" {
		q += ` WHERE role = ?`
		args = append(args, string(role))
	}
	// Pinned first, then by recency, then by id: a total order, so the list does
	// not reshuffle between two loads that happened in the same second.
	q += ` ORDER BY pinned DESC, last_seen DESC, account_id`

	rows, err := s.rd.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnownPlayer
	for rows.Next() {
		var p KnownPlayer
		var role string
		var pinned int
		if err := rows.Scan(&p.AccountID, &p.Persona, &role, &p.Note, &p.LastSeen, &pinned); err != nil {
			return nil, err
		}
		p.Role = KnownRole(role)
		p.Pinned = pinned != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetPlayerPinned marks an account as kept or prunable.
func (s *Store) SetPlayerPinned(accountID int64, pinned bool) error {
	_, err := s.db.Exec(`UPDATE known_players SET pinned = ? WHERE account_id = ?`,
		b2i(pinned), accountID)
	return err
}

// SetPlayerNote stores the user's own label for an account.
func (s *Store) SetPlayerNote(accountID int64, note string) error {
	_, err := s.db.Exec(
		`INSERT INTO known_players(account_id, note, last_seen) VALUES(?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET note = excluded.note`,
		accountID, note, time.Now().Unix())
	return err
}

// ForgetPlayer removes an account from the address book and drops its cached
// aggregates. This is the "really forget them" path, distinct from removing them
// from the current snapshot.
func (s *Store) ForgetPlayer(accountID int64) error {
	if _, err := s.db.Exec(`DELETE FROM known_players WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	// A forgotten player must also lose cached Steam persona/avatar metadata.
	if _, err := s.db.Exec(`DELETE FROM players WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	return s.ForgetPlayerStats(accountID)
}

// PruneKnownPlayers deletes unpinned entries older than maxAge, keeping at most
// keep of the rest. Bounded on purpose: a season of Battle Cup produces a long
// tail of opponents scouted once and never seen again.
func (s *Store) PruneKnownPlayers(maxAge time.Duration, keep int) (int64, error) {
	if keep <= 0 {
		keep = 200
	}
	cutoff := time.Now().Add(-maxAge).Unix()
	res, err := s.db.Exec(
		`DELETE FROM known_players
		 WHERE pinned = 0 AND last_seen < ? AND account_id NOT IN (
		   SELECT account_id FROM known_players WHERE pinned = 0 ORDER BY last_seen DESC LIMIT ?
		 )`, cutoff, keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
