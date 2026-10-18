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

	// How the game went for this player. All optional: a match cached before the
	// pipeline collected performance keeps NULLs, and a nil is "not collected"
	// rather than zero — a support really can end on no tower damage, and folding
	// the two together poisons every average taken over the column.
	Lane        string // SAFE_LANE / MID_LANE / OFF_LANE / ROAMING / JUNGLE, "" unknown
	IsVictory   *bool
	Kills       *int
	Deaths      *int
	Assists     *int
	GPM         *int
	XPM         *int
	LastHits    *int
	Denies      *int
	Networth    *int
	HeroDamage  *int
	TowerDamage *int
	Imp         *int
	StatsSource string // "stratz" | "" (not collected)
}

// HasStats reports whether performance was recorded for this participant.
func (p MatchPlayer) HasStats() bool { return p.StatsSource != "" }

// PosSource / StatsSource values recorded on participants. Kept apart because the
// two sources measure differently: STRATZ infers a 1-5 position, OpenDota reports
// a lane, and an aggregate that pools them without knowing which is which is
// comparing two different claims.
const (
	PosSourceStratz     = "stratz"
	PosSourceOpenDota   = "opendota"
	StatsSourceStratz   = "stratz"
	StatsSourceOpenDota = "opendota"
)

// Match is a cached match with its draft and provenance.
type Match struct {
	ID         int64
	StartTime  int64 // unix seconds
	RadiantWin bool
	AvgMMR     *int // nil if unknown
	Patch      string
	// Duration in seconds and the lobby it was played in. Both optional: only the
	// detail path reports them, and the bulk scan never will.
	Duration  *int
	LobbyType string
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
	err := s.rd.QueryRow("SELECT 1 FROM matches WHERE match_id = ?", id).Scan(&one)
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
	err := s.rd.QueryRow(
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
	if err := s.rd.QueryRow(
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

	// COALESCE on the optional columns so a cheaper source cannot erase what a
	// richer one already stored: the bulk scan re-sees matches whose detail was
	// fetched earlier, and plain assignment would blank their duration and lobby on
	// every pass.
	if _, err := tx.Exec(
		`INSERT INTO matches(match_id, start_time, radiant_win, avg_mmr, patch, fetched_at,
		                     game_mode, rank_tier, source, duration, lobby_type)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(match_id) DO UPDATE SET
		   start_time = excluded.start_time,
		   radiant_win = excluded.radiant_win,
		   avg_mmr = COALESCE(excluded.avg_mmr, matches.avg_mmr),
		   patch = CASE WHEN excluded.patch = '' THEN matches.patch ELSE excluded.patch END,
		   fetched_at = excluded.fetched_at,
		   game_mode = excluded.game_mode,
		   rank_tier = COALESCE(excluded.rank_tier, matches.rank_tier),
		   source = excluded.source,
		   duration = COALESCE(excluded.duration, matches.duration),
		   lobby_type = CASE WHEN excluded.lobby_type = '' THEN matches.lobby_type ELSE excluded.lobby_type END`,
		m.ID, m.StartTime, b2i(m.RadiantWin), m.AvgMMR, m.Patch, time.Now().Unix(),
		m.GameMode, m.RankTier, m.Source, m.Duration, m.LobbyType,
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
	// A roster written by a source that reports no performance must not erase
	// performance a richer source already stored: re-caching a match through the
	// picks-only path would otherwise silently strip its stats. Carry the existing
	// numbers forward for any participant the new roster has none for.
	players, err := mergeExistingStats(tx, m.ID, m.Players)
	if err != nil {
		return err
	}
	m.Players = players
	if _, err := tx.Exec("DELETE FROM match_players WHERE match_id = ?", m.ID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO match_players(match_id, player_slot, account_id, hero_id, is_radiant,
		                           position, pos_source, lane, is_victory, kills, deaths, assists,
		                           gpm, xpm, last_hits, denies, networth, hero_damage,
		                           tower_damage, imp, stats_source)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range m.Players {
		if _, err := stmt.Exec(m.ID, p.PlayerSlot, p.AccountID, p.HeroID,
			b2i(p.IsRadiant), p.Position, p.PosSource, p.Lane, b2iPtr(p.IsVictory),
			p.Kills, p.Deaths, p.Assists, p.GPM, p.XPM, p.LastHits, p.Denies,
			p.Networth, p.HeroDamage, p.TowerDamage, p.Imp, p.StatsSource); err != nil {
			return err
		}
	}
	return nil
}

// mergeExistingStats fills in performance fields from the rows already stored, for
// participants the incoming roster reports none for.
//
// Keyed on hero rather than slot: the two sources agree on which hero was on which
// side, but a re-fetch can legitimately renumber slots, and matching the wrong row
// would attribute one player's game to another.
func mergeExistingStats(tx *sql.Tx, matchID int64, in []MatchPlayer) ([]MatchPlayer, error) {
	needs := false
	for _, p := range in {
		if !p.HasStats() {
			needs = true
			break
		}
	}
	if !needs {
		return in, nil
	}

	rows, err := tx.Query(
		`SELECT hero_id, lane, is_victory, kills, deaths, assists, gpm, xpm, last_hits,
		        denies, networth, hero_damage, tower_damage, imp, stats_source
		 FROM match_players WHERE match_id = ? AND stats_source <> '' ORDER BY player_slot`,
		matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prev := map[int]MatchPlayer{}
	for rows.Next() {
		var hero int
		var p MatchPlayer
		var win *int
		if err := rows.Scan(&hero, &p.Lane, &win, &p.Kills, &p.Deaths, &p.Assists,
			&p.GPM, &p.XPM, &p.LastHits, &p.Denies, &p.Networth, &p.HeroDamage,
			&p.TowerDamage, &p.Imp, &p.StatsSource); err != nil {
			return nil, err
		}
		p.IsVictory = i2bPtr(win)
		prev[hero] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(prev) == 0 {
		return in, nil
	}

	out := make([]MatchPlayer, len(in))
	copy(out, in)
	for i := range out {
		if out[i].HasStats() {
			continue
		}
		old, ok := prev[out[i].HeroID]
		if !ok {
			continue
		}
		out[i].Lane, out[i].IsVictory = old.Lane, old.IsVictory
		out[i].Kills, out[i].Deaths, out[i].Assists = old.Kills, old.Deaths, old.Assists
		out[i].GPM, out[i].XPM = old.GPM, old.XPM
		out[i].LastHits, out[i].Denies = old.LastHits, old.Denies
		out[i].Networth, out[i].HeroDamage, out[i].TowerDamage = old.Networth, old.HeroDamage, old.TowerDamage
		out[i].Imp, out[i].StatsSource = old.Imp, old.StatsSource
	}
	return out, nil
}

// i2bPtr converts an optional 0/1 back to an optional bool.
func i2bPtr(v *int) *bool {
	if v == nil {
		return nil
	}
	b := *v != 0
	return &b
}

// b2iPtr converts an optional bool to an optional 0/1, preserving "unknown".
func b2iPtr(b *bool) *int {
	if b == nil {
		return nil
	}
	v := b2i(*b)
	return &v
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

// MatchesMissingStats returns ids of cached matches that carry a draft but no
// per-player performance, newest first.
//
// This is the backlog the backfill job works through. Collection changed under a
// running cache: matches stored before performance was recorded look complete to
// every other check (they have their bans, they have their roster), so nothing
// would ever go back for the missing half unless it is asked for explicitly.
func (s *Store) MatchesMissingStats(since int64, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.rd.Query(
		`SELECT m.match_id FROM matches m
		 WHERE m.start_time >= ?
		   AND EXISTS (SELECT 1 FROM pick_bans pb WHERE pb.match_id = m.match_id)
		   AND NOT EXISTS (
		         SELECT 1 FROM match_players mp
		         WHERE mp.match_id = m.match_id AND mp.stats_source <> '')
		 ORDER BY m.start_time DESC, m.match_id DESC
		 LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MatchesMissingRank returns ids of cached matches whose average rank tier was
// never stored and never looked up, newest first.
//
// Pro matches are excluded rather than merely unfilled: a professional game has no
// average rank tier at all, so NULL there is the right answer and asking about it
// would spend requests to learn nothing. What this backlog really holds is the
// public matches cached before the provenance columns existed — they came out of
// the very table the fill reads back, so their tier was known once and simply had
// nowhere to go.
//
// Ordered newest first because those are the matches the training window weights
// most, and a pass that is interrupted should have improved the part that counts.
func (s *Store) MatchesMissingRank(limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.rd.Query(
		`SELECT match_id FROM matches
		 WHERE rank_tier IS NULL AND rank_probed_at IS NULL AND source <> ?
		 ORDER BY start_time DESC, match_id DESC
		 LIMIT ?`, SourcePro, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetRankTiers records the tiers found for a batch of matches and marks every id
// in asked as looked up, including the ones nothing was found for.
//
// Deliberately not a Match upsert: UpsertMatch replaces a match's draft rows from
// the value it is given, so writing a rank-only record through it would erase the
// pick/ban list that made the match worth keeping. This touches two columns and
// nothing else.
//
// The mark is what keeps the backlog finite — see migrateRankProbe. asked must be
// the full batch that got an answer from the source, not just the rows in tiers,
// and it must not include a batch that failed in transit: marking those would
// silently retire matches over a network hiccup.
func (s *Store) SetRankTiers(tiers map[int64]int, asked []int64) error {
	if len(asked) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	set, err := tx.Prepare(
		`UPDATE matches SET rank_tier = ? WHERE match_id = ? AND rank_tier IS NULL`)
	if err != nil {
		return err
	}
	defer set.Close()
	for id, tier := range tiers {
		if _, err := set.Exec(tier, id); err != nil {
			return err
		}
	}

	now := time.Now().Unix()
	mark, err := tx.Prepare(`UPDATE matches SET rank_probed_at = ? WHERE match_id = ?`)
	if err != nil {
		return err
	}
	defer mark.Close()
	for _, id := range asked {
		if _, err := mark.Exec(now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RankCoverage reports how many cached matches carry an average rank tier, and how
// many could — pro matches excluded, since they have no tier to carry.
func (s *Store) RankCoverage() (withRank, total int64, err error) {
	err = s.rd.QueryRow(
		`SELECT COUNT(rank_tier), COUNT(*) FROM matches WHERE source <> ?`,
		SourcePro).Scan(&withRank, &total)
	return withRank, total, err
}

// StatsCoverage reports how many cached matches in the window carry per-player
// performance, and how many have a draft at all — the Data screen's answer to
// "how much of this is the richer kind".
func (s *Store) StatsCoverage(since int64) (withStats, withDraft int64, err error) {
	err = s.rd.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM matches m WHERE m.start_time >= ?
		      AND EXISTS (SELECT 1 FROM match_players mp
		                  WHERE mp.match_id = m.match_id AND mp.stats_source <> '')),
		   (SELECT COUNT(*) FROM matches m WHERE m.start_time >= ?
		      AND EXISTS (SELECT 1 FROM pick_bans pb WHERE pb.match_id = m.match_id))`,
		since, since).Scan(&withStats, &withDraft)
	return withStats, withDraft, err
}

// MaxStartTime returns the newest cached match start_time (unix seconds), or 0 if
// the cache is empty. Used as the incremental sync watermark.
func (s *Store) MaxStartTime() (int64, error) {
	var t *int64
	if err := s.rd.QueryRow("SELECT MAX(start_time) FROM matches").Scan(&t); err != nil {
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
	if err := s.rd.QueryRow("SELECT COUNT(*) FROM matches").Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CountMatchesBySource returns how many matches came from one provenance.
//
// Retraining decisions need this rather than the total: the pools are not
// interchangeable, and a retrain triggered by thousands of fresh all-pick games
// rebuilds the model on data that never changed the answer the advisor gives.
func (s *Store) CountMatchesBySource(source string) (int64, error) {
	var n int64
	if err := s.rd.QueryRow("SELECT COUNT(*) FROM matches WHERE source = ?", source).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Matches returns all cached matches with their pick/bans, ordered by start_time
// then match_id (deterministic), optionally limited to start_time >= since.
func (s *Store) Matches(since int64) ([]Match, error) {
	rows, err := s.rd.Query(
		`SELECT match_id, start_time, radiant_win, avg_mmr, patch, game_mode, rank_tier, source,
		        duration, lobby_type
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
			&m.GameMode, &m.RankTier, &m.Source, &m.Duration, &m.LobbyType); err != nil {
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
	rows, err := s.rd.Query(
		`SELECT mp.match_id, mp.player_slot, mp.account_id, mp.hero_id, mp.is_radiant,
		        mp.position, mp.pos_source, mp.lane, mp.is_victory, mp.kills, mp.deaths,
		        mp.assists, mp.gpm, mp.xpm, mp.last_hits, mp.denies, mp.networth,
		        mp.hero_damage, mp.tower_damage, mp.imp, mp.stats_source
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
		var win *int
		if err := rows.Scan(&id, &p.PlayerSlot, &p.AccountID, &p.HeroID, &isRad,
			&p.Position, &p.PosSource, &p.Lane, &win, &p.Kills, &p.Deaths, &p.Assists,
			&p.GPM, &p.XPM, &p.LastHits, &p.Denies, &p.Networth, &p.HeroDamage,
			&p.TowerDamage, &p.Imp, &p.StatsSource); err != nil {
			return err
		}
		p.IsRadiant = isRad != 0
		p.IsVictory = i2bPtr(win)
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
	rows, err := s.rd.Query(
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
