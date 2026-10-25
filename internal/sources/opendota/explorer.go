package opendota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// explorerTimeout bounds a single Explorer call. The endpoint occasionally stalls
// (holds the connection open with no response); a healthy bounded query returns in
// a few seconds, so this short per-call deadline turns a stall into a fast failure
// the caller can retry/narrow, independent of the client's much longer hard cap
// (which exists for the heavy /matches/{id} endpoint).
const explorerTimeout = 12 * time.Second

// explorerAttempts is how many times a single Explorer query is tried before
// giving up. The endpoint intermittently stalls and the short per-call deadline
// (explorerTimeout) turns each stall into a fast failure; retrying with a fresh
// deadline rides over a transient stall instead of failing the whole sync.
const explorerAttempts = 3

// explorerRows runs an arbitrary SQL query against OpenDota's Explorer endpoint
// (read-only Postgres over the public data) and decodes the "rows" array into out.
// Heavy full-table scans time out server-side (HTTP 400); keep queries bounded
// (e.g. by an indexed match_id range). Each attempt gets its own explorerTimeout
// child deadline so a single stall fails fast and is retried (a stalled-then-
// timed-out call must not abort the run); a server-side SQL error is returned
// immediately since retrying the same query will not fix it.
func (c *Client) explorerRows(ctx context.Context, sql string, out any) error {
	var lastErr error
	for attempt := 0; attempt < explorerAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err // parent canceled: stop retrying
		}
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		err := c.explorerRowsOnce(ctx, sql, out)
		if err == nil {
			return nil
		}
		if errors.Is(err, errExplorerSQL) {
			return err // server rejected the query itself; retry is pointless
		}
		lastErr = err
	}
	return lastErr
}

// errExplorerSQL marks a server-side SQL/query error (as opposed to a transport
// stall), so explorerRows knows not to retry it.
var errExplorerSQL = errors.New("explorer sql error")

func (c *Client) explorerRowsOnce(ctx context.Context, sql string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, explorerTimeout)
	defer cancel()
	var resp struct {
		Rows json.RawMessage `json:"rows"`
		Err  *string         `json:"err"`
	}
	if err := c.getJSON(ctx, "/explorer?sql="+url.QueryEscape(sql), &resp); err != nil {
		return err
	}
	if resp.Err != nil && *resp.Err != "" {
		return fmt.Errorf("%w: %s", errExplorerSQL, *resp.Err)
	}
	// A 200 with no rows array is a malformed answer, not an empty one. This
	// endpoint stalls and truncates under load, and a caller that records what it
	// asked about — the rank fill marks a whole batch as looked up — would read the
	// silence as "none of these exist" and retire them permanently. An empty ARRAY
	// is a real answer and passes through: a match_id band with nothing in it is
	// exactly what the bulk scan expects to see.
	if !isJSONArray(resp.Rows) {
		return fmt.Errorf("explorer: response carried no rows array")
	}
	if out != nil {
		if err := json.Unmarshal(resp.Rows, out); err != nil {
			return fmt.Errorf("explorer decode rows: %w", err)
		}
	}
	return nil
}

// isJSONArray reports whether raw is present and holds a JSON array, telling an
// absent or null field apart from an empty list.
func isJSONArray(raw json.RawMessage) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			return true
		default:
			return false
		}
	}
	return false
}

// MaxPublicMatchID returns the newest match_id in the public_matches sample. It
// uses the REST /publicMatches route (newest-first, server-side indexed) instead
// of an Explorer "select max(match_id)" aggregate: the aggregate scans the whole
// table and is exactly the query the Explorer endpoint stalls on, whereas the REST
// route is a cheap indexed read that the standard getJSON retry already covers.
func (c *Client) MaxPublicMatchID(ctx context.Context) (int64, error) {
	var rows []struct {
		MatchID int64 `json:"match_id"`
	}
	if err := c.getJSON(ctx, "/publicMatches", &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("publicMatches empty")
	}
	// /publicMatches is ordered newest-first, but take the max defensively rather
	// than trusting rows[0] in case the ordering ever changes.
	max := rows[0].MatchID
	for _, r := range rows[1:] {
		if r.MatchID > max {
			max = r.MatchID
		}
	}
	return max, nil
}

// PublicMatch is a public match from public_matches, including the final team
// compositions and outcome. This is enough for win-prob and synergy/counter
// training without any per-match detail fetch; only draft order and bans (needed
// by the enemy next-pick predictor) are missing.
type PublicMatch struct {
	MatchID     int64 `json:"match_id"`
	StartTime   int64 `json:"start_time"`
	RadiantWin  bool  `json:"radiant_win"`
	RadiantTeam []int `json:"radiant_team"` // 5 radiant hero ids
	DireTeam    []int `json:"dire_team"`    // 5 dire hero ids
	AvgRankTier int   `json:"avg_rank_tier"`
}

// MaxRankTierBatch is the largest number of match ids worth putting in one
// RankTiers call.
//
// Measured, not guessed: batches of 100-300 answer in a third of a second, while
// 500 wedges the endpoint — it holds the connection open and then returns a
// TRUNCATED body under HTTP 200, which is a corrupt-JSON failure rather than an
// honest error. explorerRows retries around that, but there is no reason to walk
// into it.
const MaxRankTierBatch = 150

// RankTiers looks up the average rank tier of specific matches, keyed by match id.
//
// Ids the table has no row for are simply absent from the result — tournament
// lobbies and unranked games never enter public_matches — so a caller must treat
// "asked" and "answered" as different sets rather than assuming a tier per id.
//
// This is the cheap way to fill a rank the cache never stored: one request covers
// a whole batch, where per-match detail would cost a request each. It is also the
// only OpenDota route that knows the answer. /matches/{id} carries no match-level
// tier at all, and averaging its players' rank_tier is not a substitute: that
// field is each account's rank TODAY, not at the time of the match, and only for
// public profiles.
func (c *Client) RankTiers(ctx context.Context, ids []int64) (map[int64]int, error) {
	if len(ids) == 0 {
		return map[int64]int{}, nil
	}
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(id, 10))
	}
	sql := "select match_id, avg_rank_tier from public_matches where match_id in (" +
		b.String() + ")"
	var rows []struct {
		MatchID int64 `json:"match_id"`
		// Nullable in the table (an unranked row carries no tier), so a pointer:
		// decoding into an int would turn "no tier" into Herald.
		AvgRankTier *int `json:"avg_rank_tier"`
	}
	if err := c.explorerRows(ctx, sql, &rows); err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		if r.AvgRankTier == nil {
			continue
		}
		out[r.MatchID] = *r.AvgRankTier
	}
	return out, nil
}

// PublicMatches returns public matches whose avg_rank_tier is within
// [minTier,maxTier] and whose match_id is in (loID,hiID], including team
// compositions and outcome. gameMode filters by Dota game_mode (e.g. 2=Captains
// Mode, 22=Ranked All Pick); gameMode<=0 returns all modes. Bounding by the
// indexed match_id range keeps the scan small enough to dodge the Explorer read
// timeout. Newest-first.
func (c *Client) PublicMatches(ctx context.Context, loID, hiID int64, minTier, maxTier, gameMode int) ([]PublicMatch, error) {
	modeClause := ""
	if gameMode > 0 {
		modeClause = fmt.Sprintf("game_mode = %d and ", gameMode)
	}
	sql := fmt.Sprintf(
		"select match_id, start_time, radiant_win, radiant_team, dire_team, avg_rank_tier "+
			"from public_matches "+
			"where %savg_rank_tier between %d and %d "+
			"and match_id > %d and match_id <= %d order by match_id desc",
		modeClause, minTier, maxTier, loID, hiID)
	var rows []PublicMatch
	if err := c.explorerRows(ctx, sql, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
