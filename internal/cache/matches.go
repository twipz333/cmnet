package cache

import (
	"database/sql"
	"time"
)

// PickBan is one draft action within a cached match, in draft order.
type PickBan struct {
	Ord     int
	HeroID  int
	IsPick  bool
	Team    int // 0 radiant / 1 dire
}

// MatchPlayer is one participant of a match: who played which hero, where.
//
// Position is the lane role 1-5 when the source reported one, nil otherwise. A nil
// position means "not known", never "position 5": aggregates must skip such rows
// rather than fold them into some default, or the least-covered role silently
// collects every unlabeled player.
type MatchPlayer struct {
	PlayerSlot int    // Valve slot: 0-4 Radiant, 128-132 Dire
	AccountID  *int64 // nil = anonymous / hidden profile
	HeroID     int
	IsRadiant  bool
	Position   *int   // 1..5, nil unknown
	PosSource  string // "stratz" | "" unknown
}

// PosSource values recorded on participants.
const PosSourceStratz = "stratz"

// Match is a cached match with its draft and provenance.
type Match struct {
	ID         int64
	StartTime  int64 // unix seconds
	RadiantWin bool
	AvgMMR     *int // nil if unknown
	Patch      string
	PickBans   []PickBan
	// Players is the participant list (roles + accounts). Empty for matches cached
	// before the pipeline recorded it, and for sources that do not report it — the
	// role-aware features degrade per match rather than requiring a full re-sync.
	Players []MatchPlayer

	// Provenance. Without it the trainer cannot separate populations that must
	// not be pooled: a pro Captains Mode draft, a public high-rank CM game and an
	// Archon all-pick are three different distributions.
	GameMode int    // Valve game_mode (2 = Captains Mode); 0 unknown
	RankTier *int   // avg rank tier (40 Archon … 80 Immortal); nil unknown
	Source   string // "pro", "public-cm", "bulk", "" unknown
}

// Source values recorded on cached matches.
const (
	SourcePro      = "pro"
	SourcePublicCM = "public-cm"
	SourceBulk     = "bulk"
)

// IsCaptainsMode reports a recorded Captains Mode game_mode.
func (m Match) IsCaptainsMode() bool { return m.GameMode == 2 }

// HasMatch reports whether a match is already cached.
func (s *Store) HasMatch(id int64) (bool, error) {
	var one int
	err := s.db.QueryRow("SELECT 1 FROM matches WHERE match_id = ?", id).Scan(&one)
	switch {
	case err == nil:
		return true, nil
	case isNoRows(err):
		return false, nil
	default:
		return false, err
	}
}

// MatchHasBans reports whether a cached match already carries ban rows (i.e. it
// was enriched from full /matches/{id} detail rather than the picks-only
// public_matches bulk). Used by sync to skip re-fetching CM drafts it already has.
func (s *Store) MatchHasBans(id int64) (bool, error) {
	var one int
	err := s.db.QueryRow(
		"SELECT 1 FROM pick_bans WHERE match_id = ? AND is_pick = 0 LIMIT 1", id,
	).Scan(&one)
	switch {
	case err == nil:
		return true, nil
	case isNoRows(err):
		return false, nil
	default:
		return false, err
	}
}

// MatchIsComplete reports whether a cached match carries everything the current
// pipeline can record: ban rows (i.e. real draft detail, not the picks-only bulk)
// and, when requirePlayers is set, a full participant roster.
//
// Sync uses this to decide what it can skip. Bans alone would permanently freeze
// out every match cached before participants were recorded: the draft looks
// complete, the match is skipped forever, and role data never arrives for drafts
// already in the cache.
//
// requirePlayers exists because only the STRATZ path returns participants. Demanding
// them from a run that cannot fetch them would mark every cached match incomplete,
// re-download it, store the same thing back and burn the run's budget on drafts it
// already has — a re-fetch loop that never terminates and starves new collection.
// So the caller passes false when it has no STRATZ client.
func (s *Store) MatchIsComplete(id int64, requirePlayers bool) (bool, error) {
	hasBans, err := s.MatchHasBans(id)
	if err != nil || !hasBans {
		return false, err
	}
	if !requirePlayers {
		return true, nil
	}
	// A full roster, not merely some rows: a half-written participant list is what
	// this check exists to catch, and "at least one" would call it complete.
	var n int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM match_players WHERE match_id = ?", id).Scan(&n); err != nil {
		return false, err
	}
	return n == playersPerMatch, nil
}

// playersPerMatch is the participant count of a complete Dota match.
const playersPerMatch = 10

// UpsertMatch stores a match and its pick/bans atomically, replacing any prior
// draft rows for that match.
func (s *Store) UpsertMatch(m Match) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO matches(match_id, start_time, radiant_win, avg_mmr, patch, fetched_at, game_mode, rank_tier, source)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(match_id) DO UPDATE SET
		   start_time = excluded.start_time,
		   radiant_win = excluded.radiant_win,
		   avg_mmr = excluded.avg_mmr,
		   patch = excluded.patch,
		   fetched_at = excluded.fetched_at,
		   game_mode = excluded.game_mode,
		   rank_tier = excluded.rank_tier,
		   source = excluded.source`,
		m.ID, m.StartTime, b2i(m.RadiantWin), m.AvgMMR, m.Patch, time.Now().Unix(),
		m.GameMode, m.RankTier, m.Source,
	); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM pick_bans WHERE match_id = ?", m.ID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO pick_bans(match_id, ord, hero_id, is_pick, team) VALUES(?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, pb := range m.PickBans {
		if _, err := stmt.Exec(m.ID, pb.Ord, pb.HeroID, b2i(pb.IsPick), pb.Team); err != nil {
			return err
		}
	}

	if err := upsertPlayers(tx, m); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertPlayers replaces the participant rows of a match, but only when the list
// is complete and self-consistent.
//
// A partial roster is worse than none: per-position aggregates computed over four
// of five players are quietly biased, and nothing downstream can tell the
// difference between "this team had no support" and "we failed to parse one row".
// So an invalid list leaves the match stored with no participants — the role
// features degrade for that match and the draft data stays usable.
//
// An empty list is not an error either: most sources do not report participants,
// and re-caching such a match must not wipe rows a better source already wrote.
func upsertPlayers(tx *sql.Tx, m Match) error {
	if len(m.Players) == 0 {
		return nil
	}
	if !validRoster(m.Players) {
		return nil
	}
	if _, err := tx.Exec("DELETE FROM match_players WHERE match_id = ?", m.ID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO match_players(match_id, player_slot, account_id, hero_id, is_radiant, position, pos_source)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range m.Players {
		if _, err := stmt.Exec(m.ID, p.PlayerSlot, p.AccountID, p.HeroID,
			b2i(p.IsRadiant), p.Position, p.PosSource); err != nil {
			return err
		}
	}
	return nil
}

// validRoster reports whether a participant list is a complete 5v5 on Valve's own
// slot numbering, with distinct heroes and side matching slot.
//
// Checked here rather than trusted from the caller: the rows come from a remote API
// whose responses are routinely truncated mid-flight. Slots are pinned to the exact
// set {0..4, 128..132} instead of merely being counted, because "ten distinct
// numbers, five of them flagged Radiant" also describes a response whose fields got
// shuffled — and side is derivable from the slot, so a disagreement between the two
// means the row is not what it claims to be.
func validRoster(ps []MatchPlayer) bool {
	if len(ps) != 10 {
		return false
	}
	slots := make(map[int]bool, 10)
	heroes := make(map[int]bool, 10)
	for _, p := range ps {
		if p.HeroID <= 0 || heroes[p.HeroID] || slots[p.PlayerSlot] {
			return false
		}
		radiantSlot := p.PlayerSlot >= 0 && p.PlayerSlot <= 4
		direSlot := p.PlayerSlot >= 128 && p.PlayerSlot <= 132
		if !radiantSlot && !direSlot {
			return false
		}
		if p.IsRadiant != radiantSlot {
			return false
		}
		if p.Position != nil && (*p.Position < 1 || *p.Position > 5) {
			return false
		}
		slots[p.PlayerSlot] = true
		heroes[p.HeroID] = true
	}
	return true
}

// MaxStartTime returns the newest cached match start_time (unix seconds), or 0 if
// the cache is empty. Used as the incremental sync watermark.
func (s *Store) MaxStartTime() (int64, error) {
	var t *int64
	if err := s.db.QueryRow("SELECT MAX(start_time) FROM matches").Scan(&t); err != nil {
		return 0, err
	}
	if t == nil {
		return 0, nil
	}
	return *t, nil
}

// CountMatches returns how many matches the cache holds. The background worker
// compares it against the count at the last training run to decide whether enough
// new data has arrived to be worth retraining on.
func (s *Store) CountMatches() (int64, error) {
	var n int64
	if err := s.db.QueryRow("SELECT COUNT(*) FROM matches").Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Matches returns all cached matches with their pick/bans, ordered by start_time
// then match_id (deterministic), optionally limited to start_time >= since.
func (s *Store) Matches(since int64) ([]Match, error) {
	rows, err := s.db.Query(
		`SELECT match_id, start_time, radiant_win, avg_mmr, patch, game_mode, rank_tier, source
		 FROM matches WHERE start_time >= ? ORDER BY start_time, match_id`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []Match
	for rows.Next() {
		var m Match
		var rw int
		if err := rows.Scan(&m.ID, &m.StartTime, &rw, &m.AvgMMR, &m.Patch,
			&m.GameMode, &m.RankTier, &m.Source); err != nil {
			return nil, err
		}
		m.RadiantWin = rw != 0
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range matches {
		pbs, err := s.pickBans(matches[i].ID)
		if err != nil {
			return nil, err
		}
		matches[i].PickBans = pbs
	}
	if err := s.attachPlayers(matches, since); err != nil {
		return nil, err
	}
	return matches, nil
}

// attachPlayers fills in the participant lists for already-loaded matches.
//
// One query for the whole window rather than one per match: participants are only
// present for recently synced matches, so on a mature cache the per-match version
// would issue tens of thousands of queries to find nothing. Rows arrive ordered so
// each match's list is in a fixed order regardless of SQLite's plan (§6).
func (s *Store) attachPlayers(matches []Match, since int64) error {
	if len(matches) == 0 {
		return nil
	}
	rows, err := s.db.Query(
		`SELECT mp.match_id, mp.player_slot, mp.account_id, mp.hero_id, mp.is_radiant,
		        mp.position, mp.pos_source
		 FROM match_players mp
		 JOIN matches m ON m.match_id = mp.match_id
		 WHERE m.start_time >= ?
		 ORDER BY mp.match_id, mp.player_slot`,
		since,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	byMatch := map[int64][]MatchPlayer{}
	for rows.Next() {
		var id int64
		var p MatchPlayer
		var isRad int
		if err := rows.Scan(&id, &p.PlayerSlot, &p.AccountID, &p.HeroID, &isRad,
			&p.Position, &p.PosSource); err != nil {
			return err
		}
		p.IsRadiant = isRad != 0
		byMatch[id] = append(byMatch[id], p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range matches {
		matches[i].Players = byMatch[matches[i].ID]
	}
	return nil
}

func (s *Store) pickBans(matchID int64) ([]PickBan, error) {
	rows, err := s.db.Query(
		`SELECT ord, hero_id, is_pick, team FROM pick_bans WHERE match_id = ? ORDER BY ord`,
		matchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PickBan
	for rows.Next() {
		var pb PickBan
		var ip int
		if err := rows.Scan(&pb.Ord, &pb.HeroID, &ip, &pb.Team); err != nil {
			return nil, err
		}
		pb.IsPick = ip != 0
		out = append(out, pb)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
