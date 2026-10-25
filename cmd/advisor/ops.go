package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmnet/internal/alerts"
	"cmnet/internal/blend"
	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/capability"
	"cmnet/internal/declog"
	"cmnet/internal/heroagg"
	"cmnet/internal/model"
	"cmnet/internal/objectives"
	"cmnet/internal/recommender"
	"cmnet/internal/roles"
	"cmnet/internal/rules"
	"cmnet/internal/scout"
	"cmnet/internal/session"
	"cmnet/internal/sources/opendota"
	"cmnet/internal/sources/stratz"
	"cmnet/internal/startup"
	"cmnet/internal/train"
	"cmnet/internal/tui"
)

// logFunc receives human-readable progress lines from a long operation.
type logFunc func(string)

func discard(string) {}

var activePatchIdx *opendota.PatchIndex

// ensurePatchLabels loads OpenDota /constants/patch and backfills empty (and
// legacy numeric) patch columns from match start_time. Without this the status
// bar stays on "patch —" forever: STRATZ/bulk ingest never wrote the field, and
// train had nothing to stamp into the bundle.
func ensurePatchLabels(ctx context.Context, od *opendota.Client, store *cache.Store, log logFunc) *opendota.PatchIndex {
	if log == nil {
		log = discard
	}
	rows, err := od.Patches(ctx)
	if err != nil {
		log(fmt.Sprintf("patch labels: skipped (%v)", err))
		return nil
	}
	idx := opendota.NewPatchIndex(rows)
	var windows []cache.PatchWindow
	for _, r := range idx.Ranges() {
		windows = append(windows, cache.PatchWindow{Name: r.Name, Start: r.Start, End: r.End})
	}
	if len(windows) > 0 {
		windows[0].Start = 0 // anything older than the first known patch still gets a label
	}
	n, err := store.BackfillPatches(windows, idx.ByID())
	if err != nil {
		log(fmt.Sprintf("patch labels: backfill failed (%v)", err))
		return idx
	}
	if n > 0 {
		log(fmt.Sprintf("patch labels: labelled %d matches (latest %s)", n, idx.Latest()))
	} else {
		log(fmt.Sprintf("patch labels: up to date (latest %s)", idx.Latest()))
	}
	activePatchIdx = idx
	return idx
}

// opSync runs an incremental sync and returns a summary. Progress lines go to log.
func opSync(ctx context.Context, o syncOpts, log logFunc) (string, error) {
	if log == nil {
		log = discard
	}
	store, err := cache.Open(o.dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	od := opendota.New()

	heroes, err := od.Heroes(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch heroes: %w", err)
	}
	cached := make([]cache.Hero, len(heroes))
	for i, h := range heroes {
		cached[i] = cache.Hero{ID: h.ID, Name: h.Name, Localized: h.LocalizedName}
	}
	if err := store.UpsertHeroes(cached); err != nil {
		return "", fmt.Errorf("store heroes: %w", err)
	}
	log(fmt.Sprintf("heroes: %d cached", len(cached)))

	patchIdx := ensurePatchLabels(ctx, od, store, log)
	_ = patchIdx // converters use time backfill; index kept for future detail writes

	since := time.Now().AddDate(0, 0, -o.windowDays).Unix()
	log(fmt.Sprintf("scanning bulk (game_mode %d, rank %d-%d), last %d days, cap %d matches",
		o.bulkGameMode, o.minTier, o.maxTier, o.windowDays, o.maxMatches))

	// Picks-only bulk sync, all OpenDota (no STRATZ token, no per-match detail, no
	// rate wall). We read matches at our rank tier straight from the public_matches
	// table via the Explorer SQL endpoint, which already carries the final team
	// compositions and outcome — enough for the win-prob head and the synergy/counter
	// matrices. The default game_mode is Ranked All Pick (22): that is where the
	// VOLUME is, and synergy/counter only need final comps + outcome, not the draft
	// mode. Full-column scans time out, so we page backwards in narrow indexed
	// match_id bands and filter by avg_rank_tier + window.
	//
	// What this pool lacks: draft order and bans. Those come from the separate CM
	// drafts pass below (/proMatches → /matches/{id}), which feeds the next-pick
	// predictor. Synergy/counter + win-prob use both pools combined.
	//
	// Band width is the key tuning knob: the Explorer query is a sequential scan of
	// the band, so a too-wide band times out while a small one returns fast. The
	// right width depends on the mode's DENSITY in public_matches: Captains Mode is
	// rare (a 250k-id band returns in ~2s), but Ranked All Pick is ~30-40x denser, so
	// the same band would return a huge result set and time out. We pick a mode-aware
	// starting width and additionally narrow adaptively on any failure (see fetchBand).
	const idsPerDay = 1_300_000 // rough match_id growth per day, for the lookback floor
	bandWidth, minBand := int64(20_000), int64(4_000)
	if o.bulkGameMode == 2 { // Captains Mode is sparse → wide bands are fine
		bandWidth, minBand = 250_000, 40_000
	}

	maxID, err := od.MaxPublicMatchID(ctx)
	if err != nil {
		return "", fmt.Errorf("opendota explorer (max id): %w", err)
	}
	// Bound how far back we page by an id estimate of the window (the start_time
	// check inside scanBands is the real stop; this is just a safety floor).
	floor := maxID - int64(o.windowDays+3)*idsPerDay
	if floor < 0 {
		floor = 0
	}

	// Incremental watermarks (#6): instead of re-walking the whole window every run
	// (which felt like "going over the same games"), remember the newest and oldest
	// match_ids already scanned. Each run then does a FORWARD pass (games newer than
	// last sync) plus a BACKFILL pass (older than what we've reached), so re-running
	// gathers new territory rather than rescanning the covered middle.
	prevNewest := metaInt(store, "sync_newest_id")
	prevOldest := metaInt(store, "sync_oldest_id")

	capVal := o.maxMatches
	if capVal <= 0 {
		capVal = 1 << 30 // 0 / negative => effectively unlimited (whole window)
	}
	acc := &bandScan{
		ctx: ctx, od: od, store: store, log: log,
		minTier: o.minTier, maxTier: o.maxTier, since: since,
		bandWidth: bandWidth, minBand: minBand,
		maxBands: (o.windowDays+5)*int(idsPerDay/bandWidth) + 20,
		cap:      capVal, gameMode: o.bulkGameMode,
	}

	// Forward pass: newer-than-last-sync games (skip on first run).
	if prevNewest > 0 && maxID > prevNewest {
		log("forward: scanning new games since last sync…")
		acc.run(maxID, prevNewest)
	}
	// Backfill pass: continue older than we've reached (or full window on first run).
	if !acc.done() {
		start := maxID
		if prevOldest > 0 {
			start = prevOldest
		}
		if start > floor {
			log("backfill: scanning older history…")
			acc.run(start, floor)
		}
	}

	// Real CM drafts (with bans + true draft order) for the next-pick predictor.
	// The bulk band scan above is picks-only; pro/league matches give the actual
	// "pick given these bans" sequence the predictor learns from. Separate pool,
	// separate source — same cache (matches with bans are detected at train time).
	cmStored, cmSkipped, cmScanned := 0, 0, 0
	if o.cmMatches > 0 {
		// Detail source: STRATZ when a token is present (its draft-only payload is tiny
		// and reliable), else OpenDota's /matches/{id} as a fallback (heavy document that
		// frequently times out / truncates mid-body on the public endpoint).
		var sc *stratz.Client
		if o.token != "" {
			sc = stratz.New(o.token)
			log(fmt.Sprintf("cm: fetching real CM drafts (with bans) via STRATZ detail, cap %d…", o.cmMatches))
		} else {
			log(fmt.Sprintf("cm: fetching real CM drafts (with bans) via OpenDota detail (no token), cap %d…", o.cmMatches))
		}
		cmStored, cmSkipped, cmScanned = fetchProCMDrafts(ctx, od, sc, store, since, o.cmMatches, log)
	}

	// Public high-rank Captains Mode drafts — the population the advisor is
	// actually used in. Pro CM (above) is a different game: coordinated teams,
	// fixed roles, bans aimed at specific players. Works without a token (OpenDota
	// detail), but a STRATZ token makes the per-match detail call far lighter.
	pubStored, pubSkipped, pubScanned := 0, 0, 0
	pubNote := ""
	if o.publicCM > 0 {
		var psc *stratz.Client
		if o.token != "" {
			psc = stratz.New(o.token)
		}
		log(fmt.Sprintf("public-cm: fetching public CM drafts (rank %d-%d), cap %d…", o.minTier, o.maxTier, o.publicCM))
		pubStored, pubSkipped, pubScanned, pubNote = fetchPublicCMDrafts(
			ctx, od, psc, store, since, floor, o.publicCM, o.minTier, o.maxTier, log)
	}

	// Update watermarks: we've now covered up to maxID, and down to the lowest band.
	newOldest := acc.lowest
	if prevOldest > 0 && (newOldest == 0 || prevOldest < newOldest) {
		newOldest = prevOldest
	}
	newNewest := maxID
	if prevNewest > newNewest {
		newNewest = prevNewest
	}
	_ = store.SetMeta("sync_newest_id", fmt.Sprint(newNewest))
	if newOldest > 0 {
		_ = store.SetMeta("sync_oldest_id", fmt.Sprint(newOldest))
	}
	if err := store.SetMeta("last_sync_unix", fmt.Sprint(time.Now().Unix())); err != nil {
		return "", err
	}
	summary := fmt.Sprintf("sync done: %d picks-only stored, %d cached, %d malformed; %d pro CM drafts stored (%d cached, %d scanned); %d public CM drafts stored (%d cached, %d scanned)",
		acc.stored, acc.skipped, acc.malformed,
		cmStored, cmSkipped, cmScanned,
		pubStored, pubSkipped, pubScanned)
	// A run that collected less than asked because the source misbehaved must say so.
	// Reporting a bare "done" after the pass gave up is how a sync that stopped 400
	// drafts into a cap of 8000 looked like a finished job for two days running.
	if pubNote != "" {
		summary += " — " + pubNote
	}
	return summary, nil
}

// fetchPublicCMDrafts collects PUBLIC Captains Mode drafts — the population the
// advisor is actually used in, unlike the pro games /proMatches returns.
//
// Two sources, because neither alone is enough. OpenDota's public_matches table
// can be filtered by game_mode=2 AND avg_rank_tier, which is exactly the selection
// we want, but it stores only final compositions — no bans, no draft order, and
// those are the whole signal for the next-action predictor. So the Explorer scan
// supplies the match IDs and the rank tier, and per-match detail supplies the
// draft. The detail call is one request per match; that is the real cost of this
// pass and the reason it is capped.
//
// (An earlier version tried to pull whole drafts from STRATZ in pages of 100 via
// matches(request:{...}). That field does not exist — STRATZ's root `matches` takes
// no `request` argument — and the query failed with a 400 on the first page.
// Guessing at an undocumented schema is what produced that; this path uses only
// endpoints the codebase already exercises.)
func fetchPublicCMDrafts(ctx context.Context, od *opendota.Client, sc *stratz.Client, store *cache.Store, since int64, floor int64, cap, minTier, maxTier int, log logFunc) (stored, skipped, scanned int, note string) {
	// Captains Mode is sparse in public_matches, so a wide id band still returns a
	// manageable result set (the same reasoning as the bulk scan's mode-aware width).
	const bandWidth = int64(250_000)
	const maxBands = 400

	maxID, err := od.MaxPublicMatchID(ctx)
	if err != nil {
		log(fmt.Sprintf("public-cm: explorer max id failed: %v", err))
		return 0, 0, 0, fmt.Sprintf("public-cm did not run: %v", err)
	}

	// Two passes over two watermarks, the same shape as the bulk scan.
	//
	// A single backward-only pass (what this used to be) walks away from the present
	// and never comes back: once it has covered the window, every later run starts at
	// the oldest id it reached and finds nothing but games it already has, while the
	// ~270 new drafts a day pile up above the watermark forever unseen. Worse, when
	// the oldest watermark sinks below the recency floor the loop stops entering at
	// all and the pass silently becomes a no-op.
	prevNewest := metaInt(store, "public_cm_newest_id")
	prevOldest := metaInt(store, "public_cm_oldest_id")

	s := &cmScan{
		ctx: ctx, od: od, sc: sc, store: store, log: log,
		since: since, cap: cap, minTier: minTier, maxTier: maxTier,
		bandWidth: bandWidth, maxBands: maxBands,
		beat: heartbeat(log),
	}

	// Forward pass: drafts newer than the last run. Skipped on a first run, where
	// there is no "newer" yet and the backfill below starts from the top anyway.
	forwardComplete := true
	if prevNewest > 0 && maxID > prevNewest {
		log("public-cm: scanning new drafts since last sync…")
		forwardComplete = s.run(maxID, prevNewest)
	}

	// Backfill pass: continue older than we have reached (or the whole window on a
	// first run).
	if !s.done() {
		start := maxID
		if prevOldest > 0 && prevOldest < start {
			start = prevOldest
		}
		if start > floor {
			s.oldestSeen = 0 // forget whatever the forward pass covered above
			s.run(start, floor)
			// Persist the backfill frontier only. The forward pass runs above it by
			// definition, so letting it write here would drag the frontier back toward
			// the present and orphan every draft below it.
			//
			// frontier(), not oldestSeen: a pass that stepped past a band it could not
			// fetch has collected below the gap but not covered it, and moving the
			// watermark past the gap is exactly how drafts get orphaned.
			if f := s.frontier(); f > 0 {
				_ = store.SetMeta("public_cm_oldest_id", fmt.Sprint(f))
			}
		}
	}

	// Advance the newest watermark only when the forward pass actually reached the
	// previous one. Moving it after a run cut short by the cap would leave a gap of
	// drafts no pass ever looks at again.
	if forwardComplete && maxID > prevNewest {
		_ = store.SetMeta("public_cm_newest_id", fmt.Sprint(maxID))
	}
	if s.skippedBands > 0 {
		note = fmt.Sprintf("%d band(s) the Explorer would not answer were skipped; the next run retries them", s.skippedBands)
	}
	return s.stored, s.skipped, s.scanned, note
}

// cmScan is one public-CM band walk: shared counters and the cap that stops it,
// so the forward and backfill passes are the same code over different ranges.
type cmScan struct {
	ctx     context.Context
	od      *opendota.Client
	sc      *stratz.Client
	store   *cache.Store
	log     logFunc
	beat    func(string)
	since   int64
	cap     int
	minTier int
	maxTier int

	bandWidth int64
	maxBands  int

	stored  int
	skipped int
	scanned int
	// oldestSeen is the lowest band boundary this scan has covered (0 = none). Only
	// the backfill pass persists it; see the note where it is written.
	oldestSeen int64
	// skippedBands counts bands skipped across every pass, for the run summary: a
	// sync that quietly collected less than asked because the Explorer was flaky
	// should say so rather than report a clean "done".
	skippedBands int
	// holeAt is the top of the highest band this pass had to skip (0 = none). The
	// pass keeps going below a hole, but the persisted frontier stops here: whatever
	// was collected further down is already in the cache, while the gap itself gets
	// another attempt on the next run instead of being orphaned forever.
	holeAt int64
}

// cmMaxConsecFails is how many bands in a row may fail before the pass concedes.
// A handful of skips is the Explorer being flaky; a dozen in a row is the Explorer
// (or the network) being down, and stepping through the whole window 40k ids at a
// time would just be a slow way to fail.
const cmMaxConsecFails = 8

func (s *cmScan) done() bool { return s.stored >= s.cap }

// frontier is the lowest id this pass may claim as covered: the bottom of what it
// walked, or the top of the first gap it had to skip.
func (s *cmScan) frontier() int64 {
	if s.holeAt > 0 {
		return s.holeAt
	}
	return s.oldestSeen
}

// cmMinBand is the narrowest id band the public-CM scan will retry with before
// giving up on a range.
const cmMinBand = int64(40_000)

// cmNarrowBackoff are the waits before re-trying a band that failed even at
// cmMinBand.
//
// Narrowing answers one kind of failure — a band too wide to answer inside the
// per-call deadline. It does nothing for the other kind: the Explorer being
// unwell for a minute or two, where every width fails alike. explorerRows already
// rides over a momentary stall (3 attempts, ~12s each), so anything reaching here
// has outlasted that and needs waiting out in minutes, not seconds. Without this
// the narrowest band failing was terminal for the pass — and at ~3000 drafts a day
// (the STRATZ coverage ceiling), one unlucky query cost a day of collection.
var cmNarrowBackoff = []time.Duration{30 * time.Second, 2 * time.Minute}

// fetchBand queries the Explorer for one band ending at hi, halving the width down
// to cmMinBand on failure and returning the lower bound actually covered. At the
// narrowest width it waits out cmNarrowBackoff before conceding the band.
func (s *cmScan) fetchBand(hi, stopAt int64) ([]opendota.PublicMatch, int64, error) {
	width := s.bandWidth
	narrowRetries := 0
	for {
		lo := hi - width
		if lo < stopAt {
			lo = stopAt
		}
		rows, err := s.od.PublicMatches(s.ctx, lo, hi, s.minTier, s.maxTier, 2)
		if err == nil {
			return rows, lo, nil
		}
		// A cancelled context is the user stopping the run, not a band too wide to
		// answer: retrying narrower would just fail again, slower.
		if s.ctx.Err() != nil {
			return nil, lo, err
		}
		if width <= cmMinBand {
			if narrowRetries >= len(cmNarrowBackoff) {
				return nil, lo, err
			}
			wait := cmNarrowBackoff[narrowRetries]
			narrowRetries++
			s.log(fmt.Sprintf("public-cm: explorer unwell at %d (%v), waiting %v before retry", hi, err, wait))
			if err := sleepCtx(s.ctx, wait); err != nil {
				return nil, lo, err
			}
			continue
		}
		width /= 2
		if width < cmMinBand {
			width = cmMinBand
		}
		s.log(fmt.Sprintf("public-cm: band too wide, retrying at %d ids", width))
	}
}

// sleepCtx waits for d, or returns early if ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// run walks bands downward from hi to stopAt, storing whatever complete Captains
// Mode drafts it finds. It reports whether it covered the whole range, as opposed
// to stopping early on the cap, the recency window or an Explorer error — the
// caller needs that distinction to decide whether a watermark may move.
func (s *cmScan) run(hi, stopAt int64) bool {
	log, od, ctx := s.log, s.od, s.ctx
	fails := 0
	s.holeAt = 0 // gaps are per-pass: the caller reads it right after this returns
	for band := 0; band < s.maxBands && !s.done() && hi > stopAt; band++ {
		s.beat(fmt.Sprintf("public-cm: band %d — %d stored, %d already cached, %d fetched…",
			band+1, s.stored, s.skipped, s.scanned))
		rows, lo, err := s.fetchBand(hi, stopAt)
		if err != nil {
			if ctx.Err() != nil {
				log("public-cm: cancelled")
				return false
			}
			// One band the Explorer will not answer must not end the pass: step past it
			// and keep collecting, the way the bulk scan does. What it must not do is
			// let the frontier move below the gap — see holeAt, which pins the watermark
			// above it so a later run comes back and tries this range again.
			fails++
			log(fmt.Sprintf("public-cm: band %d failed, skipping past it: %v", band, err))
			if fails >= cmMaxConsecFails {
				log("public-cm: too many consecutive band failures — stopping")
				return false
			}
			if s.holeAt == 0 {
				s.holeAt = hi
			}
			s.skippedBands++
			hi -= cmMinBand
			continue
		}
		fails = 0
		reachedWindow := false
		for _, r := range rows {
			if s.done() {
				break
			}
			if r.StartTime < s.since {
				reachedWindow = true // rows are newest-first: everything older follows
				break
			}
			// Participants only come from STRATZ, so only a run that has a client
			// insists on them; otherwise every cached draft would look incomplete and
			// the run would spend itself re-downloading what it already has.
			has, err := s.store.MatchIsComplete(r.MatchID, s.sc != nil)
			if err == nil && has {
				s.skipped++
				// Same quiet path as the pro pass: a populated cache skips every match
				// and would otherwise print nothing for minutes.
				s.beat(fmt.Sprintf("public-cm: %d stored, %d already cached, %d fetched…", s.stored, s.skipped, s.scanned))
				continue
			}
			s.scanned++
			s.beat(fmt.Sprintf("public-cm: %d stored, %d already cached, %d fetched…", s.stored, s.skipped, s.scanned))
			m, ok := fetchCMDetail(ctx, od, s.sc, r.MatchID, log)
			if !ok {
				continue // detail fetch failed, or the match carries no pick/ban rows
			}
			if !isCompleteCMDraft(m) {
				// A truncated draft is worse than a missing one. OpenDota's heavy
				// /matches/{id} document regularly times out mid-body, and what
				// survives can parse into a handful of picks with no bans — which
				// would then be stored as a Captains Mode game that had no ban
				// phase, teaching the predictor a draft order that never happened.
				continue
			}
			tier := r.AvgRankTier
			m.GameMode = 2
			m.Source = cache.SourcePublicCM
			m.RankTier = &tier
			if err := s.store.UpsertMatch(m); err != nil {
				log(fmt.Sprintf("public-cm: store %d: %v", r.MatchID, err))
				continue
			}
			s.stored++
			if s.stored%50 == 0 {
				log(fmt.Sprintf("public-cm: %d stored, %d fetched, %d already cached…", s.stored, s.scanned, s.skipped))
			}
		}
		// The oldest watermark tracks the backfill frontier only. A forward pass runs
		// above it by definition, and letting it write here would drag the frontier
		// back to the present and orphan everything below it.
		if lo < s.oldestSeen || s.oldestSeen == 0 {
			s.oldestSeen = lo
		}
		if reachedWindow {
			log("public-cm: reached the recency window edge")
			return false
		}
		hi = lo
	}
	// Walked the range only if neither the cap stopped us nor a band was skipped.
	return !s.done() && s.holeAt == 0
}

// minCMActions is the smallest action count accepted as a real Captains Mode
// draft. A full CM draft is 24 actions (7 bans + 5 picks per side); a couple can
// go missing to remakes or unparsed rows, but a "CM draft" with ten actions and
// no bans is a truncated fetch, not a game.
const minCMActions = 20

// isCompleteCMDraft reports whether a fetched match looks like a whole CM draft.
func isCompleteCMDraft(m cache.Match) bool {
	if len(m.PickBans) < minCMActions {
		return false
	}
	for _, pb := range m.PickBans {
		if !pb.IsPick {
			return true // has a ban phase
		}
	}
	return false
}

// heartbeatInterval is the longest a long phase may go without saying anything.
// Progress that only fires on "work completed" goes silent whenever the work is
// being skipped — a full cache makes every match a cache hit — and a log that has
// not moved for minutes is indistinguishable from a hang.
const heartbeatInterval = 5 * time.Second

// heartbeat returns a logging function that emits at most once per
// heartbeatInterval, for use inside tight loops. The first call always emits.
func heartbeat(log logFunc) func(string) {
	var last time.Time
	return func(msg string) {
		if time.Since(last) < heartbeatInterval {
			return
		}
		last = time.Now()
		log(msg)
	}
}

// fetchProCMDrafts pages OpenDota /proMatches backward (newest-first) and fetches
// full pick/ban detail per match into the cache. Pro/league matches are
// predominantly Captains Mode (game_mode 2), so this yields REAL draft sequences —
// bans and true pick order — which is the signal the next-pick predictor needs and
// the picks-only public_matches bulk lacks. Bounded by the recency window (since)
// and the cap. Returns (stored, skipped-already-have-bans, scanned).
func fetchProCMDrafts(ctx context.Context, od *opendota.Client, sc *stratz.Client, store *cache.Store, since int64, cap int, log logFunc) (stored, skipped, scanned int) {
	const maxPages = 300 // safety bound (~100 matches/page)
	var lessThan int64
	beat := heartbeat(log)
	for page := 0; page < maxPages && stored < cap; page++ {
		if ctx.Err() != nil {
			log("cm: cancelled")
			return
		}
		beat(fmt.Sprintf("cm: page %d — %d stored, %d already cached, %d fetched…",
			page+1, stored, skipped, scanned))
		pros, err := od.ProMatches(ctx, lessThan)
		if err != nil {
			log(fmt.Sprintf("cm: proMatches page %d failed: %v", page, err))
			return
		}
		if len(pros) == 0 {
			return
		}
		pageOldestStart := int64(1) << 62
		pageMinID := int64(1) << 62
		for _, pm := range pros {
			if pm.MatchID < pageMinID {
				pageMinID = pm.MatchID
			}
			if pm.StartTime < pageOldestStart {
				pageOldestStart = pm.StartTime
			}
			if pm.StartTime < since {
				continue // outside the recency window
			}
			if stored >= cap {
				break
			}
			has, err := store.MatchIsComplete(pm.MatchID, sc != nil)
			if err != nil {
				log(fmt.Sprintf("cm: cache check failed: %v", err))
				return
			}
			if has {
				skipped++
				// Cache hits are the quiet path: without this a run over an already
				// populated cache prints nothing at all between pages.
				beat(fmt.Sprintf("cm: %d stored, %d already cached, %d fetched…", stored, skipped, scanned))
				continue
			}
			scanned++
			beat(fmt.Sprintf("cm: %d stored, %d already cached, %d fetched…", stored, skipped, scanned))
			cm, ok := fetchCMDetail(ctx, od, sc, pm.MatchID, log)
			if !ok {
				continue // fetch failed, not CM, or no usable pick/ban detail
			}
			// Same completeness gate as the public CM path. A timed-out detail
			// fetch yields a match with a handful of actions and no bans, which
			// looks like a CM draft to everything downstream: it teaches the
			// predictor a draft with no ban phase, and — since bans are what marks
			// a real action order — it would also silently lose its prefix rows.
			if !isCompleteCMDraft(cm) {
				continue
			}
			// Provenance: these come from /proMatches, a different population
			// from public Captains Mode (see fetchPublicCMDrafts).
			cm.GameMode = 2
			cm.Source = cache.SourcePro
			if err := store.UpsertMatch(cm); err != nil {
				log(fmt.Sprintf("cm: store %d failed: %v", pm.MatchID, err))
				return
			}
			stored++
			if stored%25 == 0 {
				log(fmt.Sprintf("cm: %d CM drafts stored (%d cached, %d fetched)", stored, skipped, scanned))
			}
		}
		if pageOldestStart < since {
			return // walked past the window
		}
		lessThan = pageMinID // older page next
	}
	return
}

// fetchCMDetail fetches one match's full draft (with bans + true order) and returns
// it as a cache.Match, keeping Captains Mode only. It prefers STRATZ when sc != nil
// (a tiny draft-only payload that is fast and reliable), falling back to OpenDota's
// heavy /matches/{id}. ok=false on any fetch error, non-CM, or missing pick/ban data
// — the caller just skips and moves on.
func fetchCMDetail(ctx context.Context, od *opendota.Client, sc *stratz.Client, id int64, log logFunc) (cache.Match, bool) {
	if sc != nil {
		md, err := sc.Match(ctx, id)
		if err != nil {
			log(fmt.Sprintf("cm: match %d STRATZ detail failed: %v", id, err))
			return cache.Match{}, false
		}
		if md == nil || !md.IsCaptainsMode() {
			return cache.Match{}, false
		}
		return stratzDetailToCacheMatch(md)
	}
	detail, err := od.Match(ctx, id)
	if err != nil {
		log(fmt.Sprintf("cm: match %d detail failed: %v", id, err))
		return cache.Match{}, false
	}
	if detail.GameMode != 2 { // keep Captains Mode only
		return cache.Match{}, false
	}
	return detailToCacheMatch(detail)
}

// stratzDetailToCacheMatch converts a STRATZ match draft into a cached match,
// preserving real order and ban flags. STRATZ encodes side as IsRadiant; the cache
// uses Team (0 radiant / 1 dire) to match OpenDota's convention.
func stratzDetailToCacheMatch(m *stratz.MatchDetail) (cache.Match, bool) {
	if len(m.PickBans) == 0 {
		return cache.Match{}, false
	}
	out := cache.Match{
		ID:         m.ID,
		StartTime:  m.StartDateTime,
		RadiantWin: m.DidRadiantWin,
		LobbyType:  m.LobbyType,
	}
	if m.DurationSeconds > 0 {
		d := m.DurationSeconds
		out.Duration = &d
	}
	if activePatchIdx != nil {
		out.Patch = activePatchIdx.NameByTime(m.StartDateTime)
	}
	for _, pb := range m.PickBans {
		team := 1
		if pb.IsRadiant {
			team = 0
		}
		out.PickBans = append(out.PickBans, cache.PickBan{
			Ord:    pb.Order,
			HeroID: pb.HeroID,
			IsPick: pb.IsPick,
			Team:   team,
		})
	}
	out.Players = stratzPlayers(m.Players)
	if m.Rank != nil {
		out.RankTier = m.Rank
	}
	return out, true
}

// stratzPlayers converts STRATZ participants into cache rows.
//
// Anonymous accounts arrive as id 0 and are stored as NULL: a zero would look like
// a real account to any JOIN, quietly merging every hidden profile in the cache
// into one imaginary player. Positions STRATZ could not infer stay NULL with no
// pos_source, so aggregates can skip them instead of treating them as a role.
func stratzPlayers(ps []stratz.MatchPlayer) []cache.MatchPlayer {
	if len(ps) == 0 {
		return nil
	}
	out := make([]cache.MatchPlayer, 0, len(ps))
	for _, p := range ps {
		row := cache.MatchPlayer{
			PlayerSlot: p.PlayerSlot,
			HeroID:     p.HeroID,
			IsRadiant:  p.IsRadiant,
		}
		if p.SteamAccountID > 0 {
			id := p.SteamAccountID
			row.AccountID = &id
		}
		if p.Position >= 1 && p.Position <= 5 {
			pos := p.Position
			row.Position = &pos
			row.PosSource = cache.PosSourceStratz
		}
		// Performance is stamped with its source only when the source actually sent
		// it. A match whose detail predates this collection keeps NULLs, and
		// stats_source is what tells the two apart downstream.
		if p.HasStats() {
			win := p.IsVictory
			row.Lane = p.Lane
			row.IsVictory = &win
			row.Kills, row.Deaths, row.Assists = p.Kills, p.Deaths, p.Assists
			row.GPM, row.XPM = p.GoldPerMinute, p.ExperiencePerMin
			row.LastHits, row.Denies = p.NumLastHits, p.NumDenies
			row.Networth, row.HeroDamage, row.TowerDamage = p.Networth, p.HeroDamage, p.TowerDamage
			row.Imp = p.Imp
			row.StatsSource = cache.StatsSourceStratz
		}
		out = append(out, row)
	}
	return out
}

// detailToCacheMatch converts a full OpenDota match (with picks_bans) into a cached
// match preserving real draft order, ban flags, and team. Returns ok=false when the
// match has no pick/ban data (parsed/abandoned matches sometimes omit it).
func detailToCacheMatch(m *opendota.Match) (cache.Match, bool) {
	if len(m.PickBans) == 0 {
		return cache.Match{}, false
	}
	out := cache.Match{
		ID:         m.MatchID,
		StartTime:  m.StartTime,
		RadiantWin: m.RadiantWin,
		RankTier:   m.AvgRankTier,
	}
	if m.Duration > 0 {
		d := m.Duration
		out.Duration = &d
	}
	if m.Patch > 0 {
		if activePatchIdx != nil {
			if name := activePatchIdx.NameByID(m.Patch); name != "" {
				out.Patch = name
			} else {
				out.Patch = strconv.Itoa(m.Patch)
			}
		} else {
			out.Patch = strconv.Itoa(m.Patch)
		}
	} else if activePatchIdx != nil {
		out.Patch = activePatchIdx.NameByTime(m.StartTime)
	}
	if m.LobbyType > 0 {
		out.LobbyType = strconv.Itoa(m.LobbyType)
	}
	for _, pb := range m.PickBans {
		out.PickBans = append(out.PickBans, cache.PickBan{
			Ord:    pb.Order,
			HeroID: pb.HeroID,
			IsPick: pb.IsPick,
			Team:   pb.Team,
		})
	}
	out.Players = opendotaPlayers(m.Players)
	return out, true
}

// opendotaPlayers converts OpenDota participants into cache rows.
//
// This path used to drop them entirely: the response was decoded and the roster
// thrown away, so every match collected without a STRATZ token was stored as a
// draft with no players — no roles, no accounts, nothing for the per-player
// aggregates to read. Matches whose detail arrived this way were systematically
// incomplete and nothing said so.
func opendotaPlayers(ps []opendota.MatchPlayer) []cache.MatchPlayer {
	if len(ps) == 0 {
		return nil
	}
	out := make([]cache.MatchPlayer, 0, len(ps))
	for _, p := range ps {
		row := cache.MatchPlayer{
			PlayerSlot: p.PlayerSlot,
			HeroID:     p.HeroID,
			IsRadiant:  p.Radiant,
		}
		// Anonymous accounts arrive as 0; stored as NULL so a JOIN cannot merge every
		// hidden profile into one imaginary player.
		if p.AccountID > 0 {
			id := p.AccountID
			row.AccountID = &id
		}
		if p.Position >= 1 && p.Position <= 5 {
			pos := p.Position
			row.Position = &pos
			row.PosSource = cache.PosSourceOpenDota
		}
		if p.HasStats() {
			row.IsVictory = p.Win
			row.Kills, row.Deaths, row.Assists = p.Kills, p.Deaths, p.Assists
			row.GPM, row.XPM = p.GoldPerMin, p.XPPerMin
			row.LastHits, row.Denies = p.LastHits, p.Denies
			row.Networth, row.HeroDamage, row.TowerDamage = p.NetWorth, p.HeroDamage, p.TowerDamage
			row.StatsSource = cache.StatsSourceOpenDota
		}
		out = append(out, row)
	}
	return out
}

// metaInt reads an int64 meta value, defaulting to 0 when absent/invalid.
func metaInt(store *cache.Store, key string) int64 {
	v, ok, err := store.GetMeta(key)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// bandScan walks match_id bands downward, storing new CM matches. It carries the
// running totals and the lowest band boundary reached so the caller can update
// watermarks. The per-run cap is shared across passes.
type bandScan struct {
	ctx     context.Context
	od      *opendota.Client
	store   *cache.Store
	log     logFunc
	minTier int
	maxTier int
	since   int64

	bandWidth int64
	minBand   int64
	maxBands  int
	cap       int
	gameMode  int

	stored    int
	skipped   int
	scanned   int
	malformed int
	lowest    int64 // lowest band boundary scanned this run (0 = none)
	band      int
}

func (a *bandScan) done() bool { return a.stored >= a.cap }

// fetchBand queries the Explorer for one band ending at hi, adaptively narrowing
// the width on failure (dense modes overflow the per-call timeout). It starts at
// a.bandWidth and halves down to a.minBand, returning the rows and the lower bound
// actually covered so the caller can step from there. A real ctx cancellation or a
// failure even at minBand returns the error.
func (a *bandScan) fetchBand(hi, lo int64) ([]opendota.PublicMatch, int64, error) {
	width := a.bandWidth
	for {
		loBand := hi - width
		if loBand < lo {
			loBand = lo
		}
		rows, err := a.od.PublicMatches(a.ctx, loBand, hi, a.minTier, a.maxTier, a.gameMode)
		if err == nil {
			return rows, loBand, nil
		}
		if a.ctx.Err() != nil || width <= a.minBand {
			return nil, 0, err
		}
		width /= 2
		a.log(fmt.Sprintf("explorer band dense at %d, narrowing to %d ids", hi, width))
	}
}

// run scans bands from hi down to lo (exclusive of lo), stopping at the cap, the
// band budget, ctx cancellation, or the recency-window edge.
func (a *bandScan) run(hi, lo int64) {
	const maxConsecFails = 12 // give up only after this many bands fail in a row
	fails := 0
	for hi > lo && a.stored < a.cap && a.band < a.maxBands {
		if a.ctx.Err() != nil {
			a.log("cancelled")
			return
		}
		rows, loBand, err := a.fetchBand(hi, lo)
		if err != nil {
			if a.ctx.Err() != nil {
				a.log("cancelled")
				return
			}
			// A single stuck band (Explorer stall) must not abort the whole backfill:
			// skip past it and keep paging. Bail only if many fail consecutively.
			fails++
			a.log(fmt.Sprintf("warn: explorer band failed at %d (skipping): %v", hi, err))
			if fails >= maxConsecFails {
				a.log("too many consecutive band failures — stopping")
				return
			}
			a.band++
			hi -= a.minBand // step past the stuck region
			continue
		}
		fails = 0
		bandOldest := int64(1) << 62
		for _, r := range rows {
			if r.StartTime < bandOldest {
				bandOldest = r.StartTime
			}
			if r.StartTime < a.since {
				continue
			}
			if len(r.RadiantTeam) != 5 || len(r.DireTeam) != 5 {
				a.malformed++
				continue
			}
			has, err := a.store.HasMatch(r.MatchID)
			if err != nil {
				return
			}
			if has {
				a.skipped++
				continue
			}
			if err := a.store.UpsertMatch(publicToCacheMatch(r)); err != nil {
				return
			}
			a.stored++
			if a.stored >= a.cap {
				break
			}
		}
		a.scanned += len(rows)
		a.band++
		if a.lowest == 0 || loBand < a.lowest {
			a.lowest = loBand
		}
		a.log(fmt.Sprintf("band %d: %d scanned, %d stored, %d cached", a.band, a.scanned, a.stored, a.skipped))
		if len(rows) > 0 && bandOldest < a.since {
			return // window edge
		}
		hi = loBand
	}
}

// publicToCacheMatch converts an Explorer public_matches row into a cached match.
// Only final picks are known (no bans, no draft order), so each hero is recorded
// as a pick with a synthetic sequential order: radiant five first, then dire.
func publicToCacheMatch(r opendota.PublicMatch) cache.Match {
	tier := r.AvgRankTier
	out := cache.Match{
		ID:         r.MatchID,
		StartTime:  r.StartTime,
		RadiantWin: r.RadiantWin,
		Source:     cache.SourceBulk,
		RankTier:   &tier,
	}
	if activePatchIdx != nil {
		out.Patch = activePatchIdx.NameByTime(r.StartTime)
	}
	ord := 0
	for _, h := range r.RadiantTeam {
		out.PickBans = append(out.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 0})
		ord++
	}
	for _, h := range r.DireTeam {
		out.PickBans = append(out.PickBans, cache.PickBan{Ord: ord, HeroID: h, IsPick: true, Team: 1})
		ord++
	}
	return out
}

// opTrain trains layer-1 models plus matrices and writes the bundle.
func opTrain(ctx context.Context, o trainOpts, log logFunc) (string, error) {
	if log == nil {
		log = discard
	}
	store, err := cache.Open(o.dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	log("loading cache…")
	patchIdx := ensurePatchLabels(ctx, opendota.New(), store, log)
	heroes, err := store.Heroes()
	if err != nil {
		return "", err
	}
	if len(heroes) == 0 {
		return "", fmt.Errorf("no heroes cached — run sync first")
	}
	heroIDs := make([]int, len(heroes))
	for i, h := range heroes {
		heroIDs[i] = h.ID
	}
	since := time.Now().AddDate(0, 0, -o.windowDays).Unix()
	matches, err := store.Matches(since)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no matches in the training window — run sync first")
	}
	log(fmt.Sprintf("%d matches in window, %d heroes", len(matches), len(heroIDs)))

	// Auto-stamp the bundle patch tag from the corpus when the caller did not pass
	// --patch. Otherwise the status bar forever shows "patch —" even after sync
	// labelled every match.
	if o.patchTag == "" {
		o.patchTag = cache.DominantPatch(matches)
		if o.patchTag == "" && patchIdx != nil {
			o.patchTag = patchIdx.Latest()
		}
		if o.patchTag != "" {
			log(fmt.Sprintf("patch tag: auto %s", o.patchTag))
		}
	}

	// Position meta (#9) + hero positions (#8) BEFORE train.Bundle: the strength
	// stacker fits StackFeatRole on the same coverage feature serve will apply
	// (§6.3). Baking them afterwards left the stacker weight trained on zeros.
	var positionMeta map[int][]bundle.PosHero
	var heroPositions map[int][]int
	if o.token != "" {
		if pm, hp, err := stratzPositionMeta(ctx, o, log); err != nil {
			log(fmt.Sprintf("stratz position meta: skipped (%v) — falling back to roles", err))
			heroPositions = fetchHeroPositions(ctx, log)
			if len(heroPositions) > 0 {
				log(fmt.Sprintf("hero positions: %d heroes mapped (roles → 1-5)", len(heroPositions)))
			}
		} else {
			positionMeta = pm
			heroPositions = hp
			log(fmt.Sprintf("stratz position meta: %d positions, %d heroes with positions", len(pm), len(hp)))
		}
	} else {
		heroPositions = fetchHeroPositions(ctx, log)
		if len(heroPositions) > 0 {
			log(fmt.Sprintf("hero positions: %d heroes mapped (roles → 1-5; pass --token for real per-position meta)", len(heroPositions)))
		}
	}

	rolePrior := roles.BuildPrior(matches, roles.Laplace)
	log(fmt.Sprintf("role prior: %d heroes with position counts from match_players", len(rolePrior.Prob)))

	b := train.Bundle(matches, heroIDs, train.Options{
		WindowDays:    o.windowDays,
		PatchTag:      o.patchTag,
		MMRTier:       o.mmrTier,
		HalfLifeDays:  o.halfLifeDays,
		FilterPatch:   o.filterPatch,
		MinAvgMMR:     o.minAvgMMR,
		MinRankTier:   o.trainMinRankTier,
		SourceWeights: train.ProWeight(o.proWeight),
		FMRank:        o.fmRank,
		NoPrefix:      o.noPrefix,
		HeroPositions: heroPositions,
		RolePrior:     rolePrior,
		Progress:      log,
	})
	if len(positionMeta) > 0 {
		b.PositionMeta = positionMeta
	}

	// Aggregates from the corpus this bundle was actually trained on: per-position
	// pick/win rates, ban rates, and lane pairings. They answer the same questions
	// as the STRATZ meta above but about our own population, so both are baked and
	// each panel says which it is showing.
	//
	// Computed from the same filtered match slice the models were fitted on, so the
	// numbers describe the bundle rather than whatever else happens to be cached.
	agg := heroagg.Compute(matches, heroagg.Options{})
	b.CorpusPositions = agg.Positions
	b.CorpusBans = agg.Bans
	b.CorpusLanes = agg.Lanes
	log(fmt.Sprintf("corpus aggregates: %d hero-position rows, %d ban rows, %d lane pairings",
		len(agg.Positions), len(agg.Bans), len(agg.Lanes)))

	log("saving bundle…")
	if err := b.Save(o.outPath); err != nil {
		return "", err
	}
	markTrained(store)
	return fmt.Sprintf("trained on %d matches → %s (version %s)", len(matches), o.outPath, b.BundleVersion[:12]), nil
}

// Watermarks for the background worker's retrain trigger: when the bundle was
// last built, and how large the cache was at that moment.
//
// The count that drives the trigger is public CM only. It has its OWN key rather
// than reusing metaLastTrainCount, whose stored value is a total over every
// provenance: reading that total as a public-CM count would make "new drafts
// since the last train" come out tens of thousands negative, and the retrain
// would then never fire again.
const (
	metaLastTrainUnix    = "auto_last_train_unix"
	metaLastTrainCount   = "auto_last_train_count"
	metaLastTrainCMCount = "auto_last_train_cm_count"
)

// markTrained records that a bundle was just built. It runs for MANUAL training
// too, on purpose: the trigger asks "has enough arrived since the model was last
// built", and a hand-run train answers that question just as well as an automatic
// one. Recording only automatic runs would have the worker retrain minutes after
// the user did it themselves.
//
// Best-effort — failing to write the watermark must not fail a training run that
// already produced a good bundle. The cost of losing it is one redundant retrain.
func markTrained(store *cache.Store) {
	n, err := store.CountMatches()
	if err != nil {
		return
	}
	_ = store.SetMeta(metaLastTrainUnix, fmt.Sprint(time.Now().Unix()))
	_ = store.SetMeta(metaLastTrainCount, fmt.Sprint(n))
	if cm, err := store.CountMatchesBySource(cache.SourcePublicCM); err == nil {
		_ = store.SetMeta(metaLastTrainCMCount, fmt.Sprint(cm))
	}
}

// fetchHeroPositions builds hero id → eligible positions (1-5) from OpenDota hero
// roles. Returns nil on any failure (lining then stays disabled).
func fetchHeroPositions(ctx context.Context, log logFunc) map[int][]int {
	heroes, err := opendota.New().Heroes(ctx)
	if err != nil {
		log(fmt.Sprintf("hero positions: skipped (%v)", err))
		return nil
	}
	out := make(map[int][]int, len(heroes))
	for _, h := range heroes {
		out[h.ID] = rolesToPositions(h.Roles)
	}
	return out
}

// rolesToPositions maps OpenDota role tags to plausible lane positions (1-5). It
// is deliberately coarse (roles are not positions); STRATZ position data could
// refine this later when a token is available. Unknown/empty → all positions.
func rolesToPositions(roles []string) []int {
	set := map[int]bool{}
	for _, r := range roles {
		switch r {
		case "Carry":
			set[1], set[2] = true, true
		case "Support":
			set[4], set[5] = true, true
		case "Initiator", "Durable":
			set[3] = true
		case "Nuker", "Escape":
			set[2] = true
		case "Disabler":
			set[3], set[5] = true, true
		case "Pusher", "Jungler":
			set[3], set[4] = true, true
		}
	}
	if len(set) == 0 {
		return []int{1, 2, 3, 4, 5}
	}
	out := make([]int, 0, len(set))
	for p := 1; p <= 5; p++ {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

// Position-meta tuning (#9).
const (
	posMetaStorePerPos = 15   // heroes kept per position in the bundle (panel shows fewer)
	posMetaMinMatches  = 30   // ignore a position for a hero below this volume (noise floor)
	posMetaEligShare   = 0.20 // a hero is "eligible" for a position if it has >=20% of its busiest position's play
)

// stratzPositionMeta fetches real per-position hero stats from STRATZ and builds
// both the bundle's per-position meta (#9, ranked by popularity with win rate) and
// the hero→eligible-positions map (#8, derived from where each hero actually plays).
// The bracket is taken from the tier range so the meta matches our level.
func stratzPositionMeta(ctx context.Context, o trainOpts, log logFunc) (map[int][]bundle.PosHero, map[int][]int, error) {
	brackets := stratz.BracketsForTierRange(o.minTier, o.maxTier)
	weeks := o.windowDays/7 + 1
	log(fmt.Sprintf("stratz: fetching position meta (brackets %s, %d weeks)…", joinBrackets(brackets), weeks))

	cl := stratz.New(o.token)
	stats, err := cl.HeroPositionStats(ctx, brackets, stratz.DefaultMetaGameModes(), weeks)
	if err != nil {
		return nil, nil, err
	}

	// Per-position meta: rank by popularity (matches) with a stable hero_id tie-break
	// so every machine that loads the bundle agrees (§6). Win rate rides along for
	// display. Heroes below the noise floor are dropped.
	meta := make(map[int][]bundle.PosHero, 5)
	// Track each hero's matches per position for the eligibility map.
	heroPosMatches := map[int]map[int]int{}
	for pos := 1; pos <= 5; pos++ {
		var list []bundle.PosHero
		for _, s := range stats[pos] {
			if s.Matches < posMetaMinMatches {
				continue
			}
			wr := 0.0
			if s.Matches > 0 {
				wr = model.RoundScore(float64(s.Wins) / float64(s.Matches))
			}
			list = append(list, bundle.PosHero{HeroID: s.HeroID, Matches: s.Matches, WinRate: wr})
			if heroPosMatches[s.HeroID] == nil {
				heroPosMatches[s.HeroID] = map[int]int{}
			}
			heroPosMatches[s.HeroID][pos] = s.Matches
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Matches != list[j].Matches {
				return list[i].Matches > list[j].Matches
			}
			return list[i].HeroID < list[j].HeroID
		})
		if len(list) > posMetaStorePerPos {
			list = list[:posMetaStorePerPos]
		}
		meta[pos] = list
	}

	// Eligibility: a hero can plausibly run a position if its play there is a
	// meaningful share of its busiest position. Deterministic (sorted positions).
	heroPos := make(map[int][]int, len(heroPosMatches))
	for id, byPos := range heroPosMatches {
		maxM := 0
		for _, m := range byPos {
			if m > maxM {
				maxM = m
			}
		}
		var elig []int
		for p := 1; p <= 5; p++ {
			if m, ok := byPos[p]; ok && float64(m) >= posMetaEligShare*float64(maxM) {
				elig = append(elig, p)
			}
		}
		if len(elig) > 0 {
			heroPos[id] = elig
		}
	}
	return meta, heroPos, nil
}

// joinBrackets renders a STRATZ bracket list for a log line.
func joinBrackets(b []string) string { return strings.Join(b, ",") }

// resolvePlayers turns raw ids/URLs into ResolvedPlayer, logging each outcome.
// Display names are looked up as a side effect and cached in the local db, so the
// Statistics screen can show "Dendi" instead of the profile URL that was pasted.
// The name is deliberately NOT stored in the snapshot: it is cosmetic (§6), and
// making a shared file's hash depend on whether OpenDota answered would split the
// snapshot teammates must blend on identically.
func resolvePlayers(ctx context.Context, players []string, dbPath string, log logFunc) []scout.ResolvedPlayer {
	return resolvePlayersAs(ctx, players, dbPath, "", log)
}

// resolvePlayersAs resolves and, when a role is given, remembers the accounts in
// the local address book.
//
// Remembering happens here because this is the one place that has both the id and
// whatever nickname the lookup produced. The book is what makes "scout my usual
// five" a click rather than five profile URLs pasted again — and it lives in the
// cache, never the snapshot, because the snapshot is hashed and shared.
func resolvePlayersAs(ctx context.Context, players []string, dbPath string,
	role cache.KnownRole, log logFunc) []scout.ResolvedPlayer {
	resolved := make([]scout.ResolvedPlayer, 0, len(players))
	var ids []int64
	for _, ref := range players {
		id, err := scout.ResolveAccountID(ctx, nil, ref)
		if err != nil {
			log(fmt.Sprintf("%s → unresolved: %v", ref, err))
			resolved = append(resolved, scout.ResolvedPlayer{Nickname: ref, Resolved: false})
			continue
		}
		resolved = append(resolved, scout.ResolvedPlayer{AccountID: id, Nickname: ref, Resolved: true})
		ids = append(ids, id)
	}

	names := personaNames(ctx, dbPath, ids)
	for _, p := range resolved {
		if !p.Resolved {
			continue
		}
		if n := names[p.AccountID]; n != "" {
			log(fmt.Sprintf("%s → account_id %d (%s)", p.Nickname, p.AccountID, n))
		} else {
			log(fmt.Sprintf("%s → account_id %d (no public profile name)", p.Nickname, p.AccountID))
		}
	}
	if role != "" {
		rememberScouted(dbPath, resolved, names, role)
	}
	return resolved
}

// rememberScouted files the resolved accounts under a role. Best-effort: failing
// to write the address book must not fail the scout the user is waiting on.
func rememberScouted(dbPath string, resolved []scout.ResolvedPlayer,
	names map[int64]string, role cache.KnownRole) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return
	}
	defer store.Close()
	for _, p := range resolved {
		if !p.Resolved {
			continue
		}
		_ = store.RememberPlayer(cache.KnownPlayer{
			AccountID: p.AccountID,
			Persona:   names[p.AccountID],
			Role:      role,
		})
	}
}

// personaNameTimeout bounds the whole name lookup. Names are a nicety: a slow or
// unreachable OpenDota must never hold up a scout or the Statistics screen, so
// whatever has not arrived by then is simply shown as an account id.
const personaNameTimeout = 8 * time.Second

// personaNameTTL is how long a cached display name is trusted. People do rename
// themselves, and an account that was private when we asked may have opened up
// since, so the cache is refreshed occasionally rather than never.
const personaNameTTL = 14 * 24 * time.Hour

// personaNames returns account_id → display name for the given ids, reading the
// local cache first and querying OpenDota only for the ones missing from it. Newly
// fetched names are written back to the cache. Failures are silent by design: the
// caller falls back to the account id.
func personaNames(ctx context.Context, dbPath string, ids []int64) map[int64]string {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out
	}

	// The cache handle is opened, read, and CLOSED before any network call. Holding
	// it across the fetch below would keep a connection (and its locks) on the same
	// file a background sync is writing to, which is exactly how a name lookup ends
	// up stalling an unrelated sync.
	store, err := cache.Open(dbPath)
	if err != nil {
		return out
	}
	cached, err := store.Players(ids)
	if err != nil {
		cached = nil
	}
	store.Close()

	stale := time.Now().Add(-personaNameTTL).Unix()
	var missing []int64
	for _, id := range ids {
		p, ok := cached[id]
		if ok {
			// A stale entry is still shown; it is only queued for a refresh, so an
			// offline run keeps displaying the last known name instead of an id.
			out[id] = p.Persona
		}
		if !ok || p.FetchedAt < stale {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, personaNameTimeout)
	defer cancel()

	cl := opendota.New()
	var fresh []cache.Player
	for _, id := range missing {
		if ctx.Err() != nil {
			break // out of time: the rest keep whatever the cache had
		}
		name, err := cl.PersonaName(ctx, id)
		if err != nil {
			// One bad account (deleted, 404, transient error) must not cost the
			// others their names, so keep going until the context itself expires.
			continue
		}
		out[id] = name
		// An empty name is cached too — it means "asked, no public profile", which
		// stops every screen refresh from re-querying a private account.
		fresh = append(fresh, cache.Player{AccountID: id, Persona: name})
	}
	if len(fresh) > 0 {
		// Reopened just for the write, for the same reason it was closed above.
		if w, err := cache.Open(dbPath); err == nil {
			_ = w.UpsertPlayers(fresh)
			w.Close()
		}
	}
	return out
}

// playerLabel is what the Statistics screen shows for a player: their display name
// when known, otherwise the account id. The pasted reference (often a profile URL)
// is never shown — that is what the user was complaining about.
// displayNick prefers the OpenDota persona, then a human nickname the user typed,
// and never a bare account id / profile URL in recommendation chrome.
func displayNick(persona, raw string) string {
	if persona != "" {
		return persona
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || isRawPlayerRef(raw) {
		return "игрок"
	}
	return raw
}

func isRawPlayerRef(s string) bool {
	ls := strings.ToLower(s)
	if strings.Contains(ls, "://") || strings.Contains(ls, "opendota") ||
		strings.Contains(ls, "dotabuff") || strings.Contains(ls, "steamcommunity") ||
		strings.Contains(ls, "steamcommunitx") {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func playerLabel(name string, accountID int64, resolved bool, ref string) string {
	if name != "" {
		return name
	}
	if resolved || accountID != 0 {
		return fmt.Sprintf("account %d", accountID)
	}
	return ref // never resolved to an id at all: the raw input is all we have
}

// loadOrNewSnapshot returns the existing snapshot at path, or a fresh empty one
// when there is nothing there yet. Merging lets enemy scout, ally scout, and
// hero-pool edits coexist in one file.
//
// A file that exists but cannot be read is an ERROR, never an empty snapshot: the
// callers all merge into whatever comes back and then save, so answering "empty"
// for an unreadable file silently replaces the user's scouted opponents, hero pool
// and advice rules with nothing. Refusing to proceed keeps the file intact and
// lets them see what is wrong with it.
func loadOrNewSnapshot(path string) (*scout.OpponentSnapshot, error) {
	if path == "" {
		return &scout.OpponentSnapshot{SourceTag: "opendota"}, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return &scout.OpponentSnapshot{SourceTag: "opendota"}, nil
		}
		return nil, fmt.Errorf("snapshot %s: %w", path, err)
	}
	s, err := scout.LoadSnapshot(path)
	if err != nil {
		return nil, fmt.Errorf("snapshot %s is unreadable, refusing to overwrite it: %w", path, err)
	}
	return s, nil
}

// snapshotMu serializes the read-modify-write cycles below. Every snapshot edit
// loads the file, changes part of it and saves the whole thing back, so two of
// them running at once (a scout finishing while the user saves a hero pool, or
// two browser tabs posting together) would each write a complete file from its own
// stale copy — and the loser's changes vanish with a perfectly valid hash, which
// makes the loss silent. The operations are rare and short, so one lock for all
// snapshot paths is simpler than per-path locking and costs nothing in practice.
var snapshotMu sync.Mutex

// editSnapshot runs one atomic load-modify-save cycle against the snapshot at
// path. Anything slow (network fetches) must happen BEFORE calling this, so the
// lock is only held for the file work.
func editSnapshot(path string, apply func(*scout.OpponentSnapshot) error) error {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	snap, err := loadOrNewSnapshot(path)
	if err != nil {
		return err
	}
	if err := apply(snap); err != nil {
		return err
	}
	return snap.Save(path)
}

// opScout resolves enemy players, fetches layers 2-3, and merges them into the
// snapshot (preserving allies/hero-pool already there).
func opScout(ctx context.Context, players []string, o scoutOpts, log logFunc) (string, error) {
	if log == nil {
		log = discard
	}
	resolved := resolvePlayersAs(ctx, players, o.dbPath, cache.RoleEnemy, log)
	fresh, err := scout.Fetch(ctx, scout.NewOpenDotaSource(), resolved, scout.FetchOptions{
		WindowDays: o.windowDays, EventWindow: o.eventWindow,
	}, log)
	if err != nil {
		return "", err
	}
	if fresh.Layer2 != nil {
		cachePlayerAffinity(o.dbPath, fresh.Layer2.Players, o.windowDays)
		rememberDiscovered(ctx, o.dbPath, fresh.Layer2.Players, log)
	}
	l2, l3 := 0, 0
	err = editSnapshot(o.outPath, func(snap *scout.OpponentSnapshot) error {
		snap.FrozenAt = fresh.FrozenAt
		// Merge, don't replace: scouting enemies one at a time (esp. the live add in
		// Scan) must accumulate rather than wipe previously-added players.
		snap.Layer2 = mergeLayer2(snap.Layer2, fresh.Layer2)
		if fresh.Layer3 != nil { // keep prior team drafts if this call found none
			snap.Layer3 = fresh.Layer3
		}
		if snap.Layer2 != nil {
			l2 = len(snap.Layer2.Players)
		}
		if snap.Layer3 != nil {
			l3 = snap.Layer3.DraftsObserved
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("snapshot %s: layer2 players %d, layer3 drafts %d", o.outPath, l2, l3), nil
}

// opScoutAllies fetches our own team's hero affinity (#3) and merges it into the
// snapshot as the ally layer, preserving enemy layers and the hero pool.
func opScoutAllies(ctx context.Context, players []string, o scoutOpts, log logFunc) (string, error) {
	if log == nil {
		log = discard
	}
	resolved := resolvePlayersAs(ctx, players, o.dbPath, cache.RoleAlly, log)
	src := scout.NewOpenDotaSource()
	// One typed ally should recover the rest of the stack from their recent matches,
	// same idea as enemy scout via layer 3 — otherwise capability sees a one-row roster.
	allies := scout.ExpandWithTeammates(ctx, src, resolved, o.windowDays, 4, log)
	if allies != nil {
		cachePlayerAffinity(o.dbPath, allies.Players, o.windowDays)
		rememberDiscoveredAs(ctx, o.dbPath, allies.Players, cache.RoleAlly, log)
	}
	n := 0
	if err := editSnapshot(o.outPath, func(snap *scout.OpponentSnapshot) error {
		snap.Allies = mergeLayer2(snap.Allies, allies) // accumulate, don't replace
		if snap.Allies != nil {
			n = len(snap.Allies.Players)
		}
		return nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("snapshot %s: allies %d players", o.outPath, n), nil
}

// rememberDiscovered files the teammates layer 3 recovered into the address book,
// looking up their display names the same way a named opponent gets one.
//
// Without this they would be affinity rows with no name attached anywhere, and the
// roster screen would show four bare account_ids next to the one opponent the
// captain actually typed. Best-effort, like the rest of the address book writes:
// this runs while the user waits for a scout, and failing to name someone is not a
// reason to fail the scout.
func rememberDiscovered(ctx context.Context, dbPath string, players []scout.PlayerAffinity, log logFunc) {
	rememberDiscoveredAs(ctx, dbPath, players, cache.RoleEnemy, log)
}

func rememberDiscoveredAs(ctx context.Context, dbPath string, players []scout.PlayerAffinity, role cache.KnownRole, log logFunc) {
	var found []scout.ResolvedPlayer
	var ids []int64
	for _, p := range players {
		if !p.Discovered || p.AccountID <= 0 {
			continue
		}
		found = append(found, scout.ResolvedPlayer{AccountID: p.AccountID, Resolved: true, Discovered: true})
		ids = append(ids, p.AccountID)
	}
	if len(found) == 0 {
		return
	}
	names := personaNames(ctx, dbPath, ids)
	for i := range found {
		if n := names[found[i].AccountID]; n != "" {
			found[i].Nickname = n
		}
		log(fmt.Sprintf("discovered teammate: account_id %d (%s)", found[i].AccountID,
			orDefault(found[i].Nickname, "no public profile name")))
	}
	rememberScouted(dbPath, found, names, role)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// mergeLayer2 merges fresh players into existing, deduped by account_id (fresh
// wins on conflict). Either side may be nil.
func mergeLayer2(existing, fresh *scout.Layer2) *scout.Layer2 {
	if fresh == nil {
		return existing
	}
	if existing == nil {
		return fresh
	}
	existing.WindowDays = fresh.WindowDays
	byID := map[int64]int{}
	for i, p := range existing.Players {
		byID[p.AccountID] = i
	}
	for _, p := range fresh.Players {
		if idx, ok := byID[p.AccountID]; ok && p.AccountID != 0 {
			existing.Players[idx] = p
		} else {
			byID[p.AccountID] = len(existing.Players)
			existing.Players = append(existing.Players, p)
		}
	}
	return existing
}

// ruleTokenResolver adapts a name→id map into the rules.Parse resolver, also
// accepting numeric hero ids directly.
func ruleTokenResolver(idByName map[string]int) func(string) (int, bool) {
	return func(tok string) (int, bool) {
		if n, err := strconv.Atoi(strings.TrimSpace(tok)); err == nil {
			return n, true
		}
		id, ok := idByName[strings.ToLower(strings.TrimSpace(tok))]
		return id, ok
	}
}

// opSetItemRules saves the raw advice-DSL text into the snapshot (#10) and reports
// any unresolved hero tokens.
func opSetItemRules(text, snapshotPath, dbPath string) (string, error) {
	lines := strings.Split(text, "\n")
	if err := editSnapshot(snapshotPath, func(snap *scout.OpponentSnapshot) error {
		snap.ItemRules = lines
		return nil
	}); err != nil {
		return "", err
	}
	_, idByName := heroNameMaps(dbPath)
	parsed := rules.Parse(lines, ruleTokenResolver(idByName))
	msg := fmt.Sprintf("saved %d rules", len(parsed))
	if unknown := rules.UnknownTokens(parsed); len(unknown) > 0 {
		msg += " (unknown heroes: " + strings.Join(unknown, ", ") + ")"
	}
	return msg, nil
}

// opRemovePlayer removes a scouted player (enemy or ally) from the snapshot (#2).
func opRemovePlayer(snapshotPath string, accountID int64, ally bool) (string, error) {
	removed, droppedL3, nothing := 0, false, false
	if err := editSnapshot(snapshotPath, func(snap *scout.OpponentSnapshot) error {
		layer := snap.Layer2
		if ally {
			layer = snap.Allies
		}
		if layer == nil {
			nothing = true
			return nil
		}
		kept := layer.Players[:0]
		for _, p := range layer.Players {
			if p.AccountID == accountID {
				removed++
				continue
			}
			kept = append(kept, p)
		}
		layer.Players = kept

		// Layer 3 (the enemy team's event drafts) hangs off a single resolved entry
		// point (Layer3.EntryAccountID). Removing that enemy invalidates the whole
		// layer — leaving it in keeps its team-draft pick counts blending into the
		// enemy prediction with no live basis, which skews the prediction. Drop it.
		if !ally && snap.Layer3 != nil &&
			(snap.Layer3.EntryAccountID == accountID || len(layer.Players) == 0) {
			snap.Layer3 = nil
			droppedL3 = true
		}
		return nil
	}); err != nil {
		return "", err
	}
	if nothing {
		return "nothing to remove", nil
	}
	kind := "enemy"
	if ally {
		kind = "ally"
	}
	msg := fmt.Sprintf("removed %d %s player", removed, kind)
	if droppedL3 {
		msg += " + cleared event drafts (layer 3)"
	}
	// Only the snapshot is edited. What was collected about this account stays in
	// the cache on purpose, so scouting the same opponent again — the next round of
	// the same Battle Cup, most likely — starts from what is already there instead
	// of re-fetching it. "advisor forget" is the path that actually clears it.
	msg += " (cached stats kept for a faster re-scout)"
	return msg, nil
}

// opClearLayer3 drops the enemy team's event drafts (layer 3) from the snapshot.
// Layer 3 hangs off a single entry-point enemy; once that enemy is removed it can
// linger with no live basis and skew the enemy prediction, and it is not always
// deletable via the per-player path (its entry id may not be in the layer-2 list).
func opClearLayer3(snapshotPath string) (string, error) {
	n, none := 0, false
	if err := editSnapshot(snapshotPath, func(snap *scout.OpponentSnapshot) error {
		if snap.Layer3 == nil {
			none = true
			return nil
		}
		n = snap.Layer3.DraftsObserved
		if n == 0 {
			n = len(snap.Layer3.TeamDrafts)
		}
		snap.Layer3 = nil
		return nil
	}); err != nil {
		return "", err
	}
	if none {
		return "no event drafts to clear", nil
	}
	return fmt.Sprintf("cleared event drafts (layer 3): %d drafts", n), nil
}

// opSetHeroPool merges a preferred hero pool (#4) into the snapshot.
// opClearScout drops every player from the enemy or ally layer of the snapshot.
// Clearing enemies also drops layer 3 (event drafts hang off the enemy squad).
func opClearScout(snapshotPath string, ally bool) (string, error) {
	n, none := 0, false
	if err := editSnapshot(snapshotPath, func(snap *scout.OpponentSnapshot) error {
		layer := snap.Layer2
		if ally {
			layer = snap.Allies
		}
		if layer == nil || len(layer.Players) == 0 {
			none = true
			return nil
		}
		n = len(layer.Players)
		layer.Players = nil
		if !ally {
			snap.Layer3 = nil
		}
		return nil
	}); err != nil {
		return "", err
	}
	if none {
		return "nothing to clear", nil
	}
	kind := "enemy"
	if ally {
		kind = "ally"
	}
	msg := fmt.Sprintf("cleared %d %s players from snapshot", n, kind)
	if !ally {
		msg += " (+ layer 3)"
	}
	return msg, nil
}

func opSetHeroPool(ids []int, outPath string) (string, error) {
	if err := editSnapshot(outPath, func(snap *scout.OpponentSnapshot) error {
		snap.HeroPool = ids
		return nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("snapshot %s: hero pool %d heroes", outPath, len(ids)), nil
}

// heroNameMaps loads id→display-name and name→id (lowercased localized + internal
// name with the npc_dota_hero_ prefix stripped) from the cache.
func heroNameMaps(dbPath string) (map[int]string, map[string]int) {
	nameByID := map[int]string{}
	idByName := map[string]int{}
	store, err := cache.Open(dbPath)
	if err != nil {
		return nameByID, idByName
	}
	defer store.Close()
	hs, err := store.Heroes()
	if err != nil {
		return nameByID, idByName
	}
	for _, h := range hs {
		label := h.Localized
		if label == "" {
			label = h.Name
		}
		nameByID[h.ID] = label
		if h.Localized != "" {
			idByName[strings.ToLower(h.Localized)] = h.ID
		}
		short := strings.TrimPrefix(h.Name, "npc_dota_hero_")
		if short != "" {
			idByName[strings.ToLower(short)] = h.ID
			idByName[strings.ToLower(strings.ReplaceAll(short, "_", " "))] = h.ID
		}
	}
	return nameByID, idByName
}

// opStats projects the current snapshot into a Statistics view (#5).
// opCorpus reports what the local cache holds, for the Data screen.
//
// The window is the one the collector itself uses, so the intake histogram covers
// exactly the range being kept current — a chart over a different span would
// invite the conclusion that collection stopped when it merely moved on.
func opCorpus(dbPath string, windowDays int) (session.CorpusStats, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return session.CorpusStats{}, err
	}
	defer store.Close()
	return store.CorpusStats(windowDays)
}

func opStats(snapshotPath, dbPath string) (tui.StatsView, error) {
	nameByID, _ := heroNameMaps(dbPath)
	snap, err := loadOrNewSnapshot(snapshotPath)
	if err != nil {
		return tui.StatsView{SnapshotPath: snapshotPath}, err
	}
	view := tui.StatsView{SnapshotPath: snapshotPath}

	// Display names for everyone in the snapshot, cache-first. This also covers
	// snapshots taken before names were looked up at all, where Nickname still holds
	// the pasted profile URL.
	var ids []int64
	for _, l2 := range []*scout.Layer2{snap.Layer2, snap.Allies} {
		if l2 == nil {
			continue
		}
		for _, p := range l2.Players {
			if p.Resolved && p.AccountID != 0 {
				ids = append(ids, p.AccountID)
			}
		}
	}
	names := personaNames(context.Background(), dbPath, ids)

	toPlayers := func(l2 *scout.Layer2) []tui.StatsPlayer {
		if l2 == nil {
			return nil
		}
		out := make([]tui.StatsPlayer, 0, len(l2.Players))
		for _, p := range l2.Players {
			sp := tui.StatsPlayer{
				AccountID: p.AccountID,
				Nickname:  playerLabel(names[p.AccountID], p.AccountID, p.Resolved, p.Nickname),
				Resolved:  p.Resolved,
			}
			if p.Smurf != nil {
				sp.Smurf = &tui.StatsSmurf{
					Score: p.Smurf.Score, Level: p.Smurf.Level,
					Label: p.Smurf.Label, Reasons: append([]string(nil), p.Smurf.Reasons...),
				}
			}
			for _, h := range p.Heroes {
				sp.TopHeroes = append(sp.TopHeroes, tui.StatsHero{
					ID: h.HeroID, Name: heroName(nameByID, h.HeroID),
					Games: h.Games, WinRate: h.WinRate, Aff: h.Aff,
				})
			}
			// Comfort summary: most-played first (not hero_id order from the snapshot).
			sort.SliceStable(sp.TopHeroes, func(i, j int) bool {
				if sp.TopHeroes[i].Games != sp.TopHeroes[j].Games {
					return sp.TopHeroes[i].Games > sp.TopHeroes[j].Games
				}
				return sp.TopHeroes[i].ID < sp.TopHeroes[j].ID
			})
			out = append(out, sp)
		}
		return out
	}
	view.EnemyPlayers = toPlayers(snap.Layer2)
	view.AllyPlayers = toPlayers(snap.Allies)
	if snap.Layer2 != nil {
		view.WindowDays = snap.Layer2.WindowDays
	} else if snap.Allies != nil {
		view.WindowDays = snap.Allies.WindowDays
	}
	if snap.Layer3 != nil {
		view.Layer3Drafts = snap.Layer3.DraftsObserved
	}
	for _, id := range snap.HeroPool {
		view.HeroPool = append(view.HeroPool, tui.StatsHero{ID: id, Name: heroName(nameByID, id)})
	}
	view.ItemRules = strings.Join(snap.ItemRules, "\n")
	return view, nil
}

func heroName(nameByID map[int]string, id int) string {
	if n, ok := nameByID[id]; ok && n != "" {
		return n
	}
	return fmt.Sprintf("hero %d", id)
}

// opSetHeroPoolTokens parses hero tokens (ids or names) and saves the pool (#4).
func opSetHeroPoolTokens(tokens []string, snapshotPath, dbPath string) (string, error) {
	_, idByName := heroNameMaps(dbPath)
	var ids []int
	var unknown []string
	seen := map[int]bool{}
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		var id int
		if n, err := strconv.Atoi(t); err == nil {
			id = n
		} else if n, ok := idByName[strings.ToLower(t)]; ok {
			id = n
		} else {
			unknown = append(unknown, t)
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if _, err := opSetHeroPool(ids, snapshotPath); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("hero pool: %d heroes saved", len(ids))
	if len(unknown) > 0 {
		msg += " (unknown: " + strings.Join(unknown, ", ") + ")"
	}
	return msg, nil
}

// bundlePositionMeta converts the bundle's STRATZ-baked per-position meta into the
// recommender's MetaHero lists, ranking each position by popularity (matches) with
// a hero_id tie-break (deterministic, §6). Returns nil when the bundle has none.
func bundlePositionMeta(b *bundle.Bundle) map[int][]recommender.MetaHero {
	if len(b.PositionMeta) == 0 {
		return nil
	}
	out := make(map[int][]recommender.MetaHero, len(b.PositionMeta))
	for pos, heroes := range b.PositionMeta {
		list := make([]recommender.MetaHero, 0, len(heroes))
		for _, h := range heroes {
			list = append(list, recommender.MetaHero{
				HeroID:  h.HeroID,
				Score:   float64(h.Matches),
				WinRate: h.WinRate,
			})
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Score != list[j].Score {
				return list[i].Score > list[j].Score
			}
			return list[i].HeroID < list[j].HeroID
		})
		out[pos] = list
	}
	return out
}

// buildScanConfig loads the bundle (+optional snapshot) and assembles the draft
// screen config (without GSI wiring; the caller attaches events if needed).

// rolePriorFromBundle wraps the baked P(pos|hero) table for serve-time callers.
func rolePriorFromBundle(b *bundle.Bundle) *roles.Prior {
	if b == nil || len(b.RolePrior) == 0 {
		return nil
	}
	return &roles.Prior{Prob: b.RolePrior}
}

func buildScanConfig(o serveOpts) (tui.Config, error) {
	b, err := bundle.Load(o.bundlePath)
	if err != nil {
		return tui.Config{}, fmt.Errorf("load bundle: %w", err)
	}
	hm := b.HeroMap()

	// Objective timers (M6): the bundle is the source of truth, but a bundle baked
	// before M6 has none — fall back to the compiled-in spawn table so live timers
	// still work. Deterministic across machines as long as everyone runs the same
	// binary (the table is code, not data).
	objs := b.Objectives
	if len(objs) == 0 {
		objs = objectives.DefaultObjectives()
	}

	rec := recommender.New(
		&model.WinProb{Model: b.WinProb, HeroMap: hm, Calib: b.Calibration, PhaseCalib: b.PhaseCalibration},
		&model.Predictor{Pick: b.PickPredictor, Ban: b.BanPredictor, HeroMap: hm},
	)
	rec.WithMatrices(b.SynergyMatrix, b.CounterMatrix)
	rec.WithStacker(b.StrengthStacker)
	rec.WithCMOrder(b.CMOrder)         // schedule the bundle's drafts were played under
	rec.WithPositions(b.HeroPositions) // #8 lining checks
	if len(b.RolePrior) > 0 {
		rec.WithRolePrior(&roles.Prior{Prob: b.RolePrior}) // §6.3 soft roles
	}
	rec.WithPositionMeta(bundlePositionMeta(b)) // #9 real per-position meta (STRATZ)

	// The opponent snapshot (layers 2-3) is optional: when it hasn't been scouted
	// yet, fall back cleanly to layer 1 (meta) instead of failing.
	var loaded *scout.OpponentSnapshot
	if o.snapshotPath != "" {
		if _, statErr := os.Stat(o.snapshotPath); statErr == nil {
			snap, err := scout.LoadSnapshot(o.snapshotPath)
			if err != nil {
				return tui.Config{}, fmt.Errorf("load snapshot: %w", err)
			}
			loaded = snap
			aff2 := snap.Aff2Vector(hm)
			aff3, n := snap.Aff3Vector(hm)
			rec.WithScout(aff2, aff3, n, blend.DefaultWeights())
			aff3Bans, _ := snap.Aff3BanVector(hm)
			rec.WithScoutBans(aff3Bans)            // what they banned this event feeds their ban steps
			rec.WithAllies(snap.AllyAffVector(hm)) // #3 personalize our picks
			rec.WithHeroPool(snap.HeroPool)        // #4 pool contention alert
			// Resolve display names before capability/lineup so recommendations
			// never surface a raw account id as "player".
			var nameIDs []int64
			for _, l2 := range []*scout.Layer2{snap.Allies, snap.Layer2} {
				if l2 == nil {
					continue
				}
				for _, pl := range l2.Players {
					if pl.AccountID > 0 {
						nameIDs = append(nameIDs, pl.AccountID)
					}
				}
			}
			personas := personaNames(context.Background(), o.dbPath, nameIDs)
			if rost := capability.FromAllies(snap.Allies, b.HeroIDs, capability.DefaultAlpha); rost != nil {
				for i := range rost.Players {
					rost.Players[i].Nickname = displayNick(personas[rost.Players[i].AccountID], rost.Players[i].Nickname)
				}
				rec.WithCapability(rost, capability.DefaultLambda) // §6.5 soft feasibility
			}
			if er := capability.FromLayer2(snap.Layer2, b.HeroIDs, capability.DefaultAlpha, 1); er != nil {
				for i := range er.Players {
					er.Players[i].Nickname = displayNick(personas[er.Players[i].AccountID], er.Players[i].Nickname)
				}
				rec.WithEnemyRoster(er)
			}
			if len(snap.ItemRules) > 0 { // #10 advice rules
				_, idByName := heroNameMaps(o.dbPath)
				rec.WithRules(rules.Parse(snap.ItemRules, ruleTokenResolver(idByName)))
			}
		}
	}

	return tui.Config{
		Heroes:        loadHeroDisplay(o.dbPath, b.HeroIDs),
		OurTeam:       o.ourTeam,
		FirstPick:     o.firstPick,
		BundleVersion: b.BundleVersion,
		PatchTag:      b.PatchTag,
		Rec:           rec,
		Objectives:    objs, // M6: objective spawn timers (bundle, or compiled-in fallback)
		AlertLeads:    o.alertLeads,
		AlertWindows:  o.alertWindows,
		AlertFile:     loadAlertFile(o),
		// §6.1: what was advised and what the captain did. Local file, bounded
		// retention, never shared; nil when logging is off or could not be opened.
		Log:       o.log,
		LogHeader: logHeaderFrom(context.Background(), o.dbPath, loaded, rolePriorFromBundle(b)),
		// Mid-draft enemy scout (#3): merge one profile into the snapshot, then
		// rebuild the recommender so layers 2-3 blend in live.
		AddEnemy: func(url string) (*recommender.Recommender, declog.Header, string, error) {
			summary, err := opScout(context.Background(), []string{url},
				scoutOpts{outPath: o.snapshotPath, dbPath: o.dbPath, windowDays: 30}, nil)
			if err != nil {
				return nil, declog.Header{}, "", err
			}
			nc, err := buildScanConfig(o)
			if err != nil {
				return nil, declog.Header{}, summary, err
			}
			// The rebuilt header travels with the recommender: the advice from here on
			// rests on the new snapshot, and the log has to record which one (§6.1).
			return nc.Rec, nc.LogHeader, summary, nil
		},
	}, nil
}

func loadAlertFile(o serveOpts) *alerts.File {
	path := alerts.Find(o.alertsPath)
	f, err := alerts.EnsureFile(path, o.alertLeads, o.alertWindows)
	if err != nil {
		startup.Warnf("alerts.yaml: %v", err)
	}
	return f
}
