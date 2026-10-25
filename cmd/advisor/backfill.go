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
	var filled, missed int
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			// Cancelled mid-pass: what was stored is stored, and the next pass finds
			// the rest by the same query. Report the partial result rather than an
			// error, which is what the queue records as "cancelled".
			return fmt.Sprintf("backfill: %d filled, %d without data (stopped early)", filled, missed), nil
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
			missed++
			continue
		}
		if !hasAnyStats(m.Players) {
			// STRATZ answered but had no performance for this match: counting it as
			// filled would hide a match that will be re-tried forever.
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
	return fmt.Sprintf("backfill: %d filled, %d without data", filled, missed), nil
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
