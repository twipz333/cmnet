package scout

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// FetchOptions configures snapshot building.
type FetchOptions struct {
	BundleVersionRef string
	WindowDays       int    // layer-2 player window (~30)
	EventWindow      string // human label, e.g. "battlecup-2026-06-02"
	EventSinceUnix   int64  // layer-3 cutoff ("today/event start"); 0 = unbounded
	MaxEventMatches  int    // cap layer-3 matches fetched per entry
	// MaxDiscovered caps how many teammates recovered from layer 3 get their own
	// affinity fetched. Capped because each one is another API call made while the
	// user waits between rounds, and because an event roster that shuffled can name
	// more than four people. Negative disables discovery entirely.
	MaxDiscovered int
}

// WithDefaults fills unset fields.
func (o FetchOptions) withDefaults() FetchOptions {
	if o.WindowDays == 0 {
		o.WindowDays = 30
	}
	if o.MaxEventMatches == 0 {
		o.MaxEventMatches = 5
	}
	if o.MaxDiscovered == 0 {
		o.MaxDiscovered = 4 // one entry player plus four teammates is a full squad
	}
	return o
}

// Fetch builds a frozen opponent snapshot (layers 2-3) from up to five players.
// Layer 2 covers every resolved player; layer 3 uses the first resolved player as
// the entry point to recover the team's event drafts. Either layer may be nil
// (graceful degradation) — the caller blends with whatever is present. progress,
// if non-nil, receives human-readable step lines.
func Fetch(ctx context.Context, src Source, players []ResolvedPlayer, opts FetchOptions, progress func(string)) (*OpponentSnapshot, error) {
	opts = opts.withDefaults()
	if progress == nil {
		progress = func(string) {}
	}
	if len(players) == 0 {
		return nil, fmt.Errorf("no players to scout")
	}

	progress("layer 2: fetching player affinities…")
	snap := &OpponentSnapshot{
		FrozenAt:         time.Now().UTC().Format(time.RFC3339),
		BundleVersionRef: opts.BundleVersionRef,
		SourceTag:        "opendota",
		Layer2:           BuildLayer2(ctx, src, players, opts.WindowDays, progress),
	}

	if entry, ok := firstResolved(players); ok {
		progress(fmt.Sprintf("layer 3: recovering team drafts via account %d…", entry.AccountID))
		l3 := BuildLayer3(ctx, src, entry.AccountID, opts.EventWindow, opts.EventSinceUnix, opts.MaxEventMatches, progress)

		// Layer 3 also handed us the rest of the squad. Fetch their affinity too:
		// without it the snapshot describes one opponent out of five and every
		// per-player judgement downstream is drawn from a quarter of the team.
		if discovered := discoveredPlayers(l3, players, opts.MaxDiscovered); len(discovered) > 0 {
			progress(fmt.Sprintf("layer 2: %d teammate(s) recovered from the team's matches…", len(discovered)))
			snap.Layer2 = appendPlayers(snap.Layer2, BuildLayer2(ctx, src, discovered, opts.WindowDays, progress))
		}
		// Publish layer 3 only when it actually carries event drafts. A drafts-less
		// shell exists solely so discovery can run; blending code treats any non-nil
		// Layer3 as "we have event evidence".
		if l3 != nil && l3.DraftsObserved > 0 {
			snap.Layer3 = l3
		}
	}
	return snap, nil
}

// discoveredPlayers picks the teammates recovered by layer 3 that the user did not
// already name, capped at max (negative disables discovery).
func discoveredPlayers(l3 *Layer3, known []ResolvedPlayer, max int) []ResolvedPlayer {
	if l3 == nil || max < 0 {
		return nil
	}
	have := make(map[int64]bool, len(known))
	for _, p := range known {
		if p.AccountID > 0 {
			have[p.AccountID] = true
		}
	}
	var out []ResolvedPlayer
	for _, id := range l3.TeamAccountIDs {
		if have[id] || id == l3.EntryAccountID {
			continue
		}
		out = append(out, ResolvedPlayer{AccountID: id, Resolved: true, Discovered: true})
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

// appendPlayers concatenates two layer-2 blocks, keeping the order they were
// built in (named players first, then discovered) so the snapshot hash is a
// function of the data and not of when a lookup happened to return.
func appendPlayers(dst, extra *Layer2) *Layer2 {
	if extra == nil || len(extra.Players) == 0 {
		return dst
	}
	if dst == nil {
		return extra
	}
	dst.Players = append(dst.Players, extra.Players...)
	return dst
}

func firstResolved(players []ResolvedPlayer) (ResolvedPlayer, bool) {
	for _, p := range players {
		if p.Resolved && p.AccountID > 0 {
			return p, true
		}
	}
	return ResolvedPlayer{}, false
}


// ExpandWithTeammates fetches affinity for named players, then recovers co-players
// from the first resolved account's recent matches and fetches those too. Used for
// ally scout: naming one stack-mate should surface the rest of the five so
// capability (§6.5) is not computed on a one-row roster.
//
// Event drafts are not published here — that is enemy layer 3. Only the teammate
// account_ids from the same match records are reused.
func ExpandWithTeammates(ctx context.Context, src Source, players []ResolvedPlayer, windowDays, maxDiscovered int, progress func(string)) *Layer2 {
	if progress == nil {
		progress = func(string) {}
	}
	if windowDays == 0 {
		windowDays = 30
	}
	if maxDiscovered == 0 {
		maxDiscovered = 4
	}
	progress("allies: quick hero affinity (extra career/smurf requests skipped)")
	out := BuildLayer2Quick(ctx, src, players, windowDays, progress)
	_, ok := firstResolved(players)
	if !ok || maxDiscovered < 0 {
		return out
	}
	// Recent matches often have NO Captain's Mode draft. The old code walked
	// every match looking for five drafts, even though all we need are teammate
	// account IDs. Probe only two matches under a short deadline.
	discoveryCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
	discovered := discoverRecentTeammates(discoveryCtx, src, players, maxDiscovered, progress)
	cancel()
	if len(discovered) == 0 {
		return out
	}
	progress(fmt.Sprintf("allies: %d possible teammate(s) from 2 recent matches", len(discovered)))
	return appendPlayers(out, BuildLayer2Quick(ctx, src, discovered, windowDays, progress))
}

// discoverRecentTeammates intentionally does not inspect drafts. It fetches at
// most two full match records (rather than scanning indefinitely for CM drafts),
// and ranks account IDs by occurrences alongside the entry player.
func discoverRecentTeammates(ctx context.Context, src Source, named []ResolvedPlayer, maxDiscovered int, progress func(string)) []ResolvedPlayer {
	entry, ok := firstResolved(named)
	if !ok || maxDiscovered < 0 || ctx.Err() != nil {
		return nil
	}
	refs, err := src.RecentMatches(ctx, entry.AccountID)
	if err != nil {
		progress(fmt.Sprintf("allies: teammate lookup skipped: %v", err))
		return nil
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].StartTime > refs[j].StartTime })
	seen := map[int64]int{}
	for i, ref := range refs {
		if i >= 2 || ctx.Err() != nil {
			break
		}
		md, err := src.MatchDraft(ctx, ref.MatchID)
		if err != nil || md == nil {
			continue
		}
		for _, id := range teammatesOf(md, entry.AccountID) {
			seen[id]++
		}
	}
	return discoveredPlayers(&Layer3{
		EntryAccountID: entry.AccountID,
		TeamAccountIDs: rankTeammates(seen),
	}, named, maxDiscovered)
}
