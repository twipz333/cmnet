package scout

import (
	"context"
	"fmt"
	"sort"
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
}

// BuildLayer2 fetches per-player hero affinity within windowDays and assembles
// layer 2. Players that fail to fetch are recorded as unresolved rather than
// failing the whole snapshot.
func BuildLayer2(ctx context.Context, src Source, players []ResolvedPlayer, windowDays int, progress func(string)) *Layer2 {
	if progress == nil {
		progress = func(string) {}
	}
	out := &Layer2{WindowDays: windowDays}
	for i, p := range players {
		if ctx.Err() != nil {
			break
		}
		pa := PlayerAffinity{AccountID: p.AccountID, Nickname: p.Nickname, Resolved: p.Resolved}
		if p.Resolved {
			if heroes, err := src.PlayerHeroes(ctx, p.AccountID, windowDays); err == nil {
				pa.Heroes = heroAffinities(heroes)
				progress(fmt.Sprintf("layer 2: player %d/%d (account %d) — %d heroes",
					i+1, len(players), p.AccountID, len(pa.Heroes)))
			} else {
				pa.Resolved = false // fetch failed -> treat as unresolved
				progress(fmt.Sprintf("layer 2: player %d/%d (account %d) — fetch failed: %v",
					i+1, len(players), p.AccountID, err))
			}
		}
		out.Players = append(out.Players, pa)
	}
	return out
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
