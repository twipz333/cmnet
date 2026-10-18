package main

import (
	"context"
	"fmt"
	"time"

	"cmnet/internal/cache"
	"cmnet/internal/sources/stratz"
)

// backfillOpts configures one pass of the stats backfill.
type backfillOpts struct {
	dbPath     string
	token      string
	windowDays int
	// limit caps how many matches one pass re-fetches. Small on purpose: the pass
	// is the checkpoint, and each match already stored is one the next pass skips.
	limit int
}

// defaultBackfillLimit is how many matches one backfill pass re-fetches.
//
// Sized against the STRATZ per-minute allowance rather than the daily one: a pass
// this size finishes in a couple of minutes, and the queue runs another as soon as
// there is room. A larger pass would hold the merge resource for longer while
// collecting exactly as much per hour.
const defaultBackfillLimit = 150

// opBackfillStats re-fetches matches that were cached before per-player
// performance was collected.
//
// The cache changed shape under matches that were already in it: they have their
// draft and their roster, so every completeness check the sync makes says they are
// done, and the lane/player aggregates would simply never see them. Rather than
// re-syncing the whole window — which would re-download drafts we already have and
// burn the day's quota doing it — this walks the specific backlog of matches
// missing stats, newest first, since those are the ones the aggregates weight most.
func opBackfillStats(ctx context.Context, o backfillOpts, log logFunc) (string, error) {
	if o.token == "" {
		// Nothing to do rather than an error: a build without a token collects
		// through OpenDota, whose detail path now stores performance directly.
		return "backfill skipped: no STRATZ token", nil
	}
	store, err := cache.Open(o.dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	since := int64(0)
	if o.windowDays > 0 {
		since = time.Now().AddDate(0, 0, -o.windowDays).Unix()
	}
	limit := o.limit
	if limit <= 0 {
		limit = defaultBackfillLimit
	}

	ids, err := store.MatchesMissingStats(since, limit)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "backfill: nothing missing per-player stats", nil
	}
	log(fmt.Sprintf("backfill: %d matches without per-player stats", len(ids)))

	cl := stratz.New(o.token)
	// ranked counts matches this pass could not fill with performance but did learn
	// a rank tier for — salvage, reported separately so it is not read as success.
	var filled, missed, ranked int
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			// Cancelled mid-pass: what was stored is stored, and the next pass finds
			// the rest by the same query. Report the partial result rather than an
			// error, which is what the queue records as "cancelled".
			return backfillSummary(filled, missed, ranked) + " (stopped early)", nil
		}
		md, err := cl.Match(ctx, id)
		if err != nil {
			// One unavailable match must not end the pass: private profiles and
			// unparsed matches are routine, and the backlog is worked through over
			// many passes anyway.
			missed++
			continue
		}
		if md == nil {
			missed++
			continue
		}
		m, ok := stratzDetailToCacheMatch(md)
		if !ok {
			// The draft did not convert, but the rank tier in the same answer is a
			// match-level fact that does not depend on it — see salvageRank.
			saved, err := salvageRank(store, md.ID, md.Rank)
			if err != nil {
				return "", err
			}
			if saved {
				ranked++
			}
			missed++
			continue
		}
		if !hasAnyStats(m.Players) {
			// STRATZ answered but had no performance for this match: counting it as
			// filled would hide a match that will be re-tried forever.
			saved, err := salvageRank(store, m.ID, m.RankTier)
			if err != nil {
				return "", err
			}
			if saved {
				ranked++
			}
			missed++
			continue
		}
		if err := store.UpsertMatch(m); err != nil {
			return "", err
		}
		filled++
		if (i+1)%25 == 0 {
			log(fmt.Sprintf("backfill: %d/%d done (%d filled)", i+1, len(ids), filled))
		}
	}

	withStats, withDraft, err := store.StatsCoverage(since)
	if err == nil && withDraft > 0 {
		log(fmt.Sprintf("coverage: %d of %d drafts now carry per-player stats (%.0f%%)",
			withStats, withDraft, 100*float64(withStats)/float64(withDraft)))
	}
	return backfillSummary(filled, missed, ranked), nil
}

// salvageRank keeps the rank tier out of an answer this pass could not otherwise
// use, reporting whether anything was stored.
//
// A response that fails to become a usable match is not empty: the average rank is
// a property of the match, not of the draft or the roster that failed to convert,
// and the request has already been paid for. Written through the narrow update
// rather than a full upsert, which would replace the match's draft rows with
// whatever the failed conversion produced.
func salvageRank(store *cache.Store, id int64, tier *int) (bool, error) {
	if tier == nil {
		// Nothing known. Deliberately not marked as looked up either — the rank fill
		// asks a different source and may well find it.
		return false, nil
	}
	if err := store.SetRankTiers(map[int64]int{id: *tier}, []int64{id}); err != nil {
		return false, err
	}
	return true, nil
}

// backfillSummary words one pass's result. The rank salvage is mentioned only when
// it happened, so an ordinary pass reads exactly as it always did.
func backfillSummary(filled, missed, ranked int) string {
	s := fmt.Sprintf("backfill: %d filled, %d without data", filled, missed)
	if ranked > 0 {
		s += fmt.Sprintf(" (%d of those gave a rank tier)", ranked)
	}
	return s
}

// hasAnyStats reports whether a converted roster carries performance for anyone.
func hasAnyStats(ps []cache.MatchPlayer) bool {
	for _, p := range ps {
		if p.HasStats() {
			return true
		}
	}
	return false
}
