// Package outcome finds the finished match that belongs to a logged draft and
// records whether we won — used when the captain skips the post-draft survey.
//
// Prefer GSI match id when present; otherwise scan recent matches of scouted
// allies and match the final pick boards. Polling is the caller's job: Resolve
// is one attempt against OpenDota as it stands right now.
package outcome

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"cmnet/internal/draft"
	"cmnet/internal/sources/opendota"
)

// ErrPending means the match exists but has no result yet (still parsing / live).
var ErrPending = errors.New("match result not ready yet")

// ErrNotFound means nothing in the recent window matches this draft.
var ErrNotFound = errors.New("no matching match found")

// Source is the OpenDota surface the resolver needs. *opendota.Client satisfies it.
type Source interface {
	Match(ctx context.Context, id int64) (*opendota.Match, error)
	RecentMatches(ctx context.Context, accountID int64) ([]opendota.PlayerMatch, error)
}

// Query is everything known about the draft we want an outcome for.
type Query struct {
	DraftID string
	// MatchID is the GSI map.matchid when known; 0 means "search by board".
	MatchID int64
	OurTeam draft.Team
	RadiantPicks []int
	DirePicks    []int
	AllyIDs      []int64
	// NotBefore is a unix start_time floor (optional). Matches older than this
	// are ignored when searching by board.
	NotBefore int64
}

// Result is a resolved match outcome for our side.
type Result struct {
	MatchID int64
	Win     bool
	Note    string
}

// ParseMatchID turns a GSI matchid string into an int64. Empty / "0" → 0, nil error.
func ParseMatchID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("match id %q: %w", s, err)
	}
	if id < 0 {
		return 0, fmt.Errorf("match id %q: negative", s)
	}
	return id, nil
}

// Resolve tries once to find the match and our win/loss.
func Resolve(ctx context.Context, src Source, q Query) (Result, error) {
	if src == nil {
		return Result{}, fmt.Errorf("outcome: nil source")
	}
	if q.MatchID > 0 {
		return fromMatchID(ctx, src, q)
	}
	if len(q.AllyIDs) == 0 {
		return Result{}, fmt.Errorf("%w: no GSI match id and no ally accounts to search", ErrNotFound)
	}
	if len(q.RadiantPicks) == 0 && len(q.DirePicks) == 0 {
		return Result{}, fmt.Errorf("%w: empty draft board", ErrNotFound)
	}
	return fromAllies(ctx, src, q)
}

func fromMatchID(ctx context.Context, src Source, q Query) (Result, error) {
	m, err := src.Match(ctx, q.MatchID)
	if err != nil {
		// Match often 404s for a few minutes after the game ends — keep polling.
		return Result{}, ErrPending
	}
	if m == nil || m.MatchID == 0 {
		return Result{}, ErrPending
	}
	// Duration 0 usually means the payload is still a stub / live match.
	if m.Duration <= 0 {
		return Result{}, ErrPending
	}
	if len(q.RadiantPicks) > 0 || len(q.DirePicks) > 0 {
		rp, dp := picksOf(m)
		if !sameHeroes(rp, q.RadiantPicks) || !sameHeroes(dp, q.DirePicks) {
			// Wrong id or sides swapped in the logged board — still trust GSI id
			// for the result, but note the mismatch.
			return Result{
				MatchID: m.MatchID,
				Win:     weWon(q.OurTeam, m.RadiantWin),
				Note:    "auto:opendota matchid (board mismatch)",
			}, nil
		}
	}
	return Result{
		MatchID: m.MatchID,
		Win:     weWon(q.OurTeam, m.RadiantWin),
		Note:    "auto:opendota matchid",
	}, nil
}

func fromAllies(ctx context.Context, src Source, q Query) (Result, error) {
	seen := map[int64]bool{}
	var pending bool
	for _, aid := range q.AllyIDs {
		if aid <= 0 {
			continue
		}
		recents, err := src.RecentMatches(ctx, aid)
		if err != nil {
			return Result{}, err
		}
		for _, pm := range recents {
			if pm.MatchID <= 0 || seen[pm.MatchID] {
				continue
			}
			seen[pm.MatchID] = true
			if q.NotBefore > 0 && pm.StartTime > 0 && pm.StartTime < q.NotBefore {
				continue
			}
			// Cheap prefilter: ally's hero should be on our side's picks.
			if pm.HeroID > 0 {
				our := q.RadiantPicks
				if q.OurTeam == draft.Dire {
					our = q.DirePicks
				}
				if !contains(our, pm.HeroID) {
					continue
				}
			}
			m, err := src.Match(ctx, pm.MatchID)
			if err != nil {
				return Result{}, err
			}
			if m == nil || m.Duration <= 0 {
				pending = true
				continue
			}
			rp, dp := picksOf(m)
			if !sameHeroes(rp, q.RadiantPicks) || !sameHeroes(dp, q.DirePicks) {
				continue
			}
			return Result{
				MatchID: m.MatchID,
				Win:     weWon(q.OurTeam, m.RadiantWin),
				Note:    "auto:opendota allies+board",
			}, nil
		}
	}
	if pending {
		return Result{}, ErrPending
	}
	return Result{}, ErrNotFound
}

func weWon(our draft.Team, radiantWin bool) bool {
	if our == draft.Radiant {
		return radiantWin
	}
	return !radiantWin
}

func picksOf(m *opendota.Match) (radiant, dire []int) {
	for _, pb := range m.PickBans {
		if !pb.IsPick || pb.HeroID <= 0 {
			continue
		}
		if pb.Team == 0 {
			radiant = append(radiant, pb.HeroID)
		} else {
			dire = append(dire, pb.HeroID)
		}
	}
	return radiant, dire
}

func sameHeroes(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]int(nil), a...)
	bb := append([]int(nil), b...)
	sort.Ints(aa)
	sort.Ints(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func contains(hs []int, id int) bool {
	for _, h := range hs {
		if h == id {
			return true
		}
	}
	return false
}
