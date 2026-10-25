package main

import (
	"context"
	"fmt"

	"cmnet/internal/cache"
	"cmnet/internal/sources/opendota"
)

// rankfillOpts configures one pass of the rank backfill.
type rankfillOpts struct {
	dbPath string
	// limit caps how many matches one pass looks up. The pass is the checkpoint:
	// every id answered is written before the next pass starts, so an interrupted
	// one costs at most the batch in flight.
	limit int
}

// defaultRankfillLimit is how many matches one rankfill pass resolves.
//
// Ten batches. The whole job is a few hundred requests spread over a couple of
// days for a mature cache, so there is no reason to make any single pass long
// enough to sit in front of a sync the user is waiting on.
const defaultRankfillLimit = 10 * opendota.MaxRankTierBatch

// opRankfill fills in the average rank tier of matches that were cached without
// one.
//
// The hole is historical: the rank column arrived after tens of thousands of
// matches were already stored, and nothing goes back for a field every other
// completeness check considers optional. The effect is quiet and bad — the rank
// filters (train's --train-min-rank-tier, the corpus breakdown) treat an unknown
// tier as "keep", so a cache that is mostly unknown makes those settings look
// configured while they select almost nothing.
//
// The tier comes back from the same public_matches table the bulk scan reads, in
// batches, without a token. Those rows were selected BY rank when they were first
// collected, so the answer is known to exist for essentially all of them; what was
// missing was somewhere to put it.
func opRankfill(ctx context.Context, o rankfillOpts, log logFunc) (string, error) {
	if log == nil {
		log = discard
	}
	store, err := cache.Open(o.dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	limit := o.limit
	if limit <= 0 {
		limit = defaultRankfillLimit
	}
	ids, err := store.MatchesMissingRank(limit)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "rankfill: every match already has a rank tier or has been asked about", nil
	}
	log(fmt.Sprintf("rankfill: %d matches without a rank tier", len(ids)))

	od := opendota.New()
	var filled, absent, done int
	var lastErr error
	for start := 0; start < len(ids); start += opendota.MaxRankTierBatch {
		if err := ctx.Err(); err != nil {
			// Cancelled between batches: what was written is written, and the next pass
			// picks up from the same query. A partial result, not an error.
			return fmt.Sprintf("rankfill: %d filled, %d not in public matches (stopped early)",
				filled, absent), nil
		}
		end := min(start+opendota.MaxRankTierBatch, len(ids))
		batch := ids[start:end]

		tiers, err := od.RankTiers(ctx, batch)
		if err != nil {
			// A failed batch is left unmarked on purpose: marking it would retire those
			// matches over what is usually a transient Explorer stall, and they would
			// never be asked about again. Stop the pass instead — the next one retries
			// the same ids from the top.
			log(fmt.Sprintf("warn: rankfill: batch of %d failed: %v", len(batch), err))
			lastErr = err
			break
		}
		if err := store.SetRankTiers(tiers, batch); err != nil {
			return "", err
		}
		filled += len(tiers)
		absent += len(batch) - len(tiers)
		done++
		log(fmt.Sprintf("rankfill: %d/%d resolved (%d filled)", end, len(ids), filled))
	}
	// A pass where the very first batch failed resolved nothing, and reporting that
	// as a completed job would hide a source that is down behind a queue full of
	// green rows. Once anything landed the pass did real work, so the failure is a
	// logged warning and the next tick continues from where this one stopped.
	if lastErr != nil && done == 0 {
		return "", lastErr
	}

	if withRank, total, err := store.RankCoverage(); err == nil && total > 0 {
		log(fmt.Sprintf("coverage: %d of %d rankable matches now carry a tier (%.0f%%)",
			withRank, total, 100*float64(withRank)/float64(total)))
	}
	return fmt.Sprintf("rankfill: %d filled, %d not in public matches", filled, absent), nil
}
