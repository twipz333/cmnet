package scout

import (
	"context"
	"fmt"
	"time"
)

// FetchOptions configures snapshot building.
type FetchOptions struct {
	BundleVersionRef string
	WindowDays       int    // layer-2 player window (~30)
	EventWindow      string // human label, e.g. "battlecup-2026-06-02"
	EventSinceUnix   int64  // layer-3 cutoff ("today/event start"); 0 = unbounded
	MaxEventMatches  int    // cap layer-3 matches fetched per entry
}

// WithDefaults fills unset fields.
func (o FetchOptions) withDefaults() FetchOptions {
	if o.WindowDays == 0 {
		o.WindowDays = 30
	}
	if o.MaxEventMatches == 0 {
		o.MaxEventMatches = 5
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
		snap.Layer3 = BuildLayer3(ctx, src, entry.AccountID, opts.EventWindow, opts.EventSinceUnix, opts.MaxEventMatches, progress)
	}
	return snap, nil
}

func firstResolved(players []ResolvedPlayer) (ResolvedPlayer, bool) {
	for _, p := range players {
		if p.Resolved && p.AccountID > 0 {
			return p, true
		}
	}
	return ResolvedPlayer{}, false
}
