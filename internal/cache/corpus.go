package cache

import (
	"database/sql"
	"fmt"
	"time"
)

// CorpusStats describes what the cache actually holds, for the Data screen.
//
// It answers the questions a user of this tool keeps asking while collection runs
// for days: how much is there, how much of it is the scarce kind (real Captains
// Mode drafts with a ban phase), how much survives the training rank filter, and
// is anything still arriving. Those are the numbers that decide whether to keep
// collecting or go train — and none of them were visible anywhere before.
type CorpusStats struct {
	Matches int64 `json:"matches"`
	// CMDrafts counts matches that carry a ban row. Those are the only ones the
	// next-action predictor can learn a draft ORDER from, and they are perhaps 3% of
	// the cache, which is exactly why they deserve their own number.
	CMDrafts int64 `json:"cmDrafts"`
	// WithPlayers counts matches whose participants are known (needed for roles and
	// per-position meta); WithPositions, those where lane positions are known too.
	WithPlayers   int64 `json:"withPlayers"`
	WithPositions int64 `json:"withPositions"`
	Heroes        int64 `json:"heroes"`
	PickBans      int64 `json:"pickBans"`

	// OldestStart/NewestStart bound the window the cache covers (unix seconds).
	OldestStart int64 `json:"oldestStart"`
	NewestStart int64 `json:"newestStart"`

	Sources []SourceCount `json:"sources"`
	Ranks   []RankBucket  `json:"ranks"`
	// Daily is matches by the day they were PLAYED — the coverage of the training
	// window. Fetched is matches by the day they were DOWNLOADED — whether the
	// collector was running. They answer different questions and look nothing alike
	// (a single sync pass pulls games spread over weeks), so both are reported
	// rather than one being labelled as the other.
	Daily   []DayCount `json:"daily"`
	Fetched []DayCount `json:"fetched"`
	// TopPicks/TopBans are the meta as this cache sees it, most contested first.
	TopPicks []HeroCount `json:"topPicks"`
	TopBans  []HeroCount `json:"topBans"`
}

// SourceCount is how many matches came from one collection path, and how many of
// those are full CM drafts.
type SourceCount struct {
	Source   string `json:"source"`
	Matches  int64  `json:"matches"`
	CMDrafts int64  `json:"cmDrafts"`
}

// RankBucket is a rank tier decade (40 = Archon … 80 = Immortal) and its count.
// Tier -1 collects matches whose rank was never recorded.
type RankBucket struct {
	Tier    int   `json:"tier"`
	Matches int64 `json:"matches"`
}

// DayCount is one day's matches, and how many of them were CM drafts.
type DayCount struct {
	// Day is the unix timestamp of LOCAL midnight for that day — the same day
	// boundary the browser draws with, so a label never disagrees with its bucket.
	Day      int64 `json:"day"`
	Matches  int64 `json:"matches"`
	CMDrafts int64 `json:"cmDrafts"`
}

// HeroCount is one hero's appearance count in the cached drafts.
type HeroCount struct {
	HeroID int    `json:"heroId"`
	Name   string `json:"name"`
	Count  int64  `json:"count"`
}

// unknownRank is the bucket for matches whose rank tier was never recorded — the
// ~30k rows cached before the provenance columns existed. They are kept, not
// dropped, so the screen has to be able to say how many of them there are.
const unknownRank = -1

// fullRoster is how many participants a complete match has. Anything short of it
// is a partial fetch, which the role-aware features cannot use.
const fullRoster = 10

// CorpusStats aggregates the cache. Every query is a full scan of a table that
// holds tens of thousands of rows, which SQLite does in milliseconds; the screen
// asks for this on demand rather than on every poll all the same.
func (s *Store) CorpusStats(windowDays int) (CorpusStats, error) {
	var st CorpusStats
	var err error

	if st.Matches, err = s.scalar("SELECT COUNT(*) FROM matches"); err != nil {
		return st, err
	}
	if st.Heroes, err = s.scalar("SELECT COUNT(*) FROM heroes"); err != nil {
		return st, err
	}
	if st.PickBans, err = s.scalar("SELECT COUNT(*) FROM pick_bans"); err != nil {
		return st, err
	}
	// "Has a ban" is what makes a draft a Captains Mode one in this cache: the bulk
	// pool is picks-only by construction, so a ban row cannot come from anywhere else.
	if st.CMDrafts, err = s.scalar("SELECT COUNT(DISTINCT match_id) FROM pick_bans WHERE is_pick = 0"); err != nil {
		return st, err
	}
	// A FULL roster, not "at least one row". A match with three known participants
	// is useless to the role-aware features, and counting it would report the cache
	// as far better equipped for per-position meta than it is.
	if st.WithPlayers, err = s.scalar(`
		SELECT COUNT(*) FROM (
			SELECT match_id FROM match_players GROUP BY match_id HAVING COUNT(*) >= ?
		)`, fullRoster); err != nil {
		return st, err
	}
	if st.WithPositions, err = s.scalar(`
		SELECT COUNT(*) FROM (
			SELECT match_id FROM match_players WHERE position IS NOT NULL
			GROUP BY match_id HAVING COUNT(*) >= ?
		)`, fullRoster); err != nil {
		return st, err
	}
	if err = s.rd.QueryRow("SELECT COALESCE(MIN(start_time), 0), COALESCE(MAX(start_time), 0) FROM matches").
		Scan(&st.OldestStart, &st.NewestStart); err != nil {
		return st, err
	}

	if st.Sources, err = s.sourceCounts(); err != nil {
		return st, err
	}
	if st.Ranks, err = s.rankBuckets(); err != nil {
		return st, err
	}
	if st.Daily, err = s.dailyCounts("start_time", windowDays); err != nil {
		return st, err
	}
	if st.Fetched, err = s.dailyCounts("fetched_at", windowDays); err != nil {
		return st, err
	}
	if st.TopPicks, err = s.topHeroes(true, 12); err != nil {
		return st, err
	}
	if st.TopBans, err = s.topHeroes(false, 12); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Store) scalar(query string, args ...any) (int64, error) {
	var n int64
	if err := s.rd.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("%s: %w", query, err)
	}
	return n, nil
}

// sourceCounts breaks the cache down by collection path, with the CM share of
// each: 'bulk' is picks-only volume, 'pro' and 'public-cm' are where the drafts
// with a ban phase come from, and they are not interchangeable populations.
func (s *Store) sourceCounts() ([]SourceCount, error) {
	// EXISTS per match, not a join to pick_bans: joining multiplies each match by
	// its ban rows, so COUNT(*) counts draft ACTIONS and the "matches" column comes
	// out several times the size of the table. (It did, on the first run of this
	// screen: three sources summed to 117k rows over a 51k-row table.)
	rows, err := s.rd.Query(`
		SELECT COALESCE(NULLIF(m.source, ''), 'unknown') AS src,
		       COUNT(*),
		       SUM(CASE WHEN EXISTS(
		             SELECT 1 FROM pick_bans b WHERE b.match_id = m.match_id AND b.is_pick = 0
		           ) THEN 1 ELSE 0 END)
		FROM matches m
		GROUP BY src
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SourceCount
	for rows.Next() {
		var c SourceCount
		if err := rows.Scan(&c.Source, &c.Matches, &c.CMDrafts); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// rankBuckets groups matches by rank tier decade. The training filter is a floor
// on this number, so seeing the shape of the distribution is what tells the user
// whether raising it would leave anything to train on.
func (s *Store) rankBuckets() ([]RankBucket, error) {
	rows, err := s.rd.Query(`
		SELECT CASE WHEN rank_tier IS NULL THEN ? ELSE (rank_tier / 10) * 10 END AS bucket,
		       COUNT(*)
		FROM matches
		GROUP BY bucket
		ORDER BY bucket`, unknownRank)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RankBucket
	for rows.Next() {
		var b RankBucket
		if err := rows.Scan(&b.Tier, &b.Matches); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// dailyCounts buckets matches by day over the last windowDays, keyed on the given
// timestamp column: start_time for "when these games were played", fetched_at for
// "when we downloaded them".
//
// Days with nothing are returned as zeroes rather than skipped: a gap is exactly
// what the user needs to see, and a chart that silently closes over missing days
// hides it.
//
// The column is chosen from a fixed set by the caller and interpolated, since a
// column name cannot be a bound parameter; nothing user-supplied reaches it.
func (s *Store) dailyCounts(column string, windowDays int) ([]DayCount, error) {
	switch column {
	case "start_time", "fetched_at":
	default:
		return nil, fmt.Errorf("dailyCounts: unsupported column %q", column)
	}
	if windowDays <= 0 {
		windowDays = 31
	}
	newest, err := s.scalar("SELECT COALESCE(MAX(" + column + "), 0) FROM matches")
	if err != nil {
		return nil, err
	}
	if newest == 0 {
		return nil, nil // empty cache: no window to describe
	}
	// Days are LOCAL calendar days, bucketed in Go rather than by arithmetic in SQL.
	//
	// Two reasons. Bucketing on UTC midnight files an evening's collecting under
	// yesterday for anyone east of Greenwich — the chart would report the last
	// download as yesterday while the user watches it happen. And a fixed offset
	// applied to every row (today's offset) drifts by an hour across a daylight
	// saving boundary, so days either side of it end up an hour off. Go's calendar
	// arithmetic handles both; the cost is one pass over the rows in the window.
	//
	// The window ends TODAY, not at the newest row. Anchoring it to the data would
	// slide the chart back with the data and draw a comfortably full window even
	// when nothing has arrived for a week — hiding the one thing the chart is for.
	// An empty tail is the honest picture of a collector that stopped.
	now := time.Now()
	lastDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	firstDay := lastDay.AddDate(0, 0, -(windowDays - 1))

	// EXISTS rather than a join, for the same reason as sourceCounts: a join would
	// count ban rows and report a day as several times busier than it was.
	rows, err := s.rd.Query(`
		SELECT m.`+column+`,
		       EXISTS(SELECT 1 FROM pick_bans b WHERE b.match_id = m.match_id AND b.is_pick = 0)
		FROM matches m
		WHERE m.`+column+` >= ?`, firstDay.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := map[int64]DayCount{}
	for rows.Next() {
		var ts int64
		var isCM bool
		if err := rows.Scan(&ts, &isCM); err != nil {
			return nil, err
		}
		t := time.Unix(ts, 0)
		key := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()
		c := byDay[key]
		c.Day = key
		c.Matches++
		if isCM {
			c.CMDrafts++
		}
		byDay[key] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]DayCount, 0, windowDays)
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		key := d.Unix()
		c, ok := byDay[key]
		if !ok {
			c = DayCount{Day: key}
		}
		out = append(out, c)
	}
	return out, nil
}

// topHeroes returns the most picked (or most banned) heroes in the cache.
func (s *Store) topHeroes(picks bool, limit int) ([]HeroCount, error) {
	isPick := 0
	if picks {
		isPick = 1
	}
	rows, err := s.rd.Query(`
		SELECT b.hero_id, COALESCE(NULLIF(h.localized, ''), h.name, ''), COUNT(*) AS n
		FROM pick_bans b
		LEFT JOIN heroes h ON h.id = b.hero_id
		WHERE b.is_pick = ?
		GROUP BY b.hero_id
		ORDER BY n DESC, b.hero_id
		LIMIT ?`, isPick, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HeroCount
	for rows.Next() {
		var c HeroCount
		var name sql.NullString
		if err := rows.Scan(&c.HeroID, &name, &c.Count); err != nil {
			return nil, err
		}
		c.Name = name.String
		if c.Name == "" {
			c.Name = fmt.Sprintf("hero %d", c.HeroID)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
