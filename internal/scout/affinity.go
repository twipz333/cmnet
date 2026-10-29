package scout

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// affinityK damps low-sample heroes: a hero with few games contributes less
// affinity even at a high win rate (comfort = plays often AND wins, §9).
const affinityK = 5.0

// ResolvedPlayer is an opponent we attempt to scout. Resolved=false means the
// nickname could not be turned into an account_id; it is skipped (graceful
// degradation) but kept for display.
type ResolvedPlayer struct {
	AccountID int64
	Nickname  string
	Resolved  bool
	// Discovered marks a player the user never named: recovered from a teammate's
	// match record (§8.1). Carried through to the snapshot so the UI can say where
	// a profile came from — a name the captain never typed appearing in the enemy
	// list is otherwise indistinguishable from a bug.
	Discovered bool
}

// BuildLayer2 fetches affinity and the additional career/match signals used for
// smurf assessment. The full path is useful for enemy analysis.
func BuildLayer2(ctx context.Context, src Source, players []ResolvedPlayer, windowDays int, progress func(string)) *Layer2 {
	return buildLayer2(ctx, src, players, windowDays, false, progress)
}

// BuildLayer2Quick is the draft-time ally path: fetching 30-day hero affinity
// needs one OpenDota call per player, not three. Smurf checks can be performed
// separately and are never worth holding up the ally roster.
func BuildLayer2Quick(ctx context.Context, src Source, players []ResolvedPlayer, windowDays int, progress func(string)) *Layer2 {
	return buildLayer2(ctx, src, players, windowDays, true, progress)
}

const quickHeroTimeout = 15 * time.Second

func buildLayer2(ctx context.Context, src Source, players []ResolvedPlayer, windowDays int, quick bool, progress func(string)) *Layer2 {
	if progress == nil {
		progress = func(string) {}
	}
	out := &Layer2{WindowDays: windowDays}
	for i, p := range players {
		if ctx.Err() != nil {
			break
		}
		pa := PlayerAffinity{AccountID: p.AccountID, Nickname: p.Nickname, Resolved: p.Resolved, Discovered: p.Discovered}
		if !p.Resolved {
			pa.Status = "unresolved"
		}
		if p.Resolved {
			queryCtx := ctx
			cancel := func() {}
			if quick {
				queryCtx, cancel = context.WithTimeout(ctx, quickHeroTimeout)
			}
			heroes, err := src.PlayerHeroes(queryCtx, p.AccountID, windowDays)
			cancel()
			if err == nil {
				pa.Heroes = heroAffinities(heroes)
				pa.Status = "ok"
				if len(pa.Heroes) == 0 {
					pa.Status = "no_stats"
				}
				if !quick {
					careerGames := 0
					if career, err := src.PlayerHeroes(ctx, p.AccountID, 0); err == nil {
						careerGames = sumHeroGames(career)
					}
					var matches []MatchRef
					if refs, err := src.RecentMatches(ctx, p.AccountID); err == nil {
						matches = refs
					}
					pa.Smurf = AssessSmurf(careerGames, matches)
				}
				progress(fmt.Sprintf("layer 2: player %d/%d (account %d) — %d heroes",
					i+1, len(players), p.AccountID, len(pa.Heroes)))
			} else {
				// The account ID was successfully resolved; an API failure is not
				// proof that the Steam account is missing or private.
				pa.Status = "error"
				progress(fmt.Sprintf("layer 2: player %d/%d (account %d) — fetch failed: %v",
					i+1, len(players), p.AccountID, err))
			}
		}
		out.Players = append(out.Players, pa)
	}
	return out
}

func sumHeroGames(rows []PlayerHero) int {
	n := 0
	for _, r := range rows {
		if r.Games > 0 {
			n += r.Games
		}
	}
	return n
}

// heroAffinities converts raw per-hero rows into smoothed affinity, sorted by
// hero id for deterministic snapshot content.
func heroAffinities(rows []PlayerHero) []HeroAffinity {
	out := make([]HeroAffinity, 0, len(rows))
	for _, r := range rows {
		if r.Games <= 0 {
			continue
		}
		winRate := float64(r.Wins) / float64(r.Games)
		// Affinity favors high win rate backed by volume.
		aff := winRate * float64(r.Games) / (float64(r.Games) + affinityK)
		out = append(out, HeroAffinity{
			HeroID:  r.HeroID,
			Games:   r.Games,
			WinRate: winRate,
			Aff:     aff,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HeroID < out[j].HeroID })
	return out
}
