package main

import (
	"fmt"
	"os"
	"time"

	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/session"
)

// autoOpts configures the background worker that keeps the cache and the bundle
// fresh while the app is open.
type autoOpts struct {
	enabled bool
	// publicCM is how many public Captains Mode drafts one background pass collects
	// before checkpointing. Small on purpose: the pass boundary is where the
	// watermarks are written, so a short pass means an interrupted one costs little.
	publicCM int
	// maxMatches / cmMatches cap the other two pools per pass, same reasoning.
	maxMatches int
	cmMatches  int

	trainEvery  time.Duration
	trainMinNew int
	idle        time.Duration
	maxIdle     time.Duration
}

// buildAuto assembles the background worker, or returns nil when it is disabled.
//
// The worker deliberately reuses opSync/opTrain rather than a background-specific
// code path: whatever the user gets from pressing Sync is what runs unattended,
// including the watermarks and the resume behaviour. Only the caps differ.
func buildAuto(o autoOpts, h homeFlags, run *runner) *session.Auto {
	if !o.enabled {
		return nil
	}
	// public-cm costs one per-match detail request per draft. Through STRATZ that is
	// a small, reliable payload; through OpenDota's /matches/{id} it times out for
	// roughly nine requests in ten, so without a token the pass would burn the
	// OpenDota budget to collect almost nothing.
	publicCM := o.publicCM
	if publicCM > 0 && h.token == "" {
		publicCM = 0
		// Say it rather than quietly collect less: public CM drafts are the pool the
		// advisor is actually used in, and a user who set auto-public-cm and gets none
		// deserves to know it is the missing token, not a broken worker.
		fmt.Fprintln(os.Stderr,
			"background collection: public CM drafts are skipped without a STRATZ token "+
				"(pass --token or set it in config.yaml)")
	}

	// The worker proposes work rather than doing it: both passes are queue
	// submissions at background priority, so a manual request preempts them and the
	// resource rules — not a bespoke handshake — keep incompatible work apart.
	return session.NewAuto(session.AutoConfig{
		Sync:     run.backgroundSync(o, publicCM),
		Train:    run.backgroundTrain(),
		TrainDue: func() (bool, string) { return trainDue(h.dbPath, h.bundlePath, o) },
		Count:    func() (int64, error) { return countMatches(h.dbPath) },
		Idle:     o.idle,
		MaxIdle:  o.maxIdle,
	})
}

// countMatches opens the cache just long enough to count. Opening per call rather
// than holding a handle keeps the worker from pinning a connection to a database
// the foreground operations write to.
func countMatches(dbPath string) (int64, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return 0, err
	}
	defer store.Close()
	return store.CountMatches()
}

// trainDue decides whether the model is stale enough to rebuild.
//
// Two conditions, both required: enough time has passed AND enough new matches
// have arrived. Time alone would retrain nightly on a cache that gained fifty
// games; volume alone would retrain repeatedly during the first days of
// collection, when thousands arrive per day and each bundle is obsolete within
// the hour. A missing bundle overrides both — there is nothing to serve.
func trainDue(dbPath, bundlePath string, o autoOpts) (bool, string) {
	// Loaded, not merely present: a bundle that fails its hash or schema check is a
	// bundle the advisor cannot serve, and "the file exists" would leave the user
	// looking at a draft screen that never works while the worker sees nothing to do.
	if _, err := bundle.Load(bundlePath); err != nil {
		if os.IsNotExist(err) {
			return true, "no bundle yet"
		}
		return true, "the bundle on disk cannot be loaded"
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return false, ""
	}
	defer store.Close()

	count, err := store.CountMatches()
	if err != nil {
		return false, ""
	}
	last := metaInt(store, metaLastTrainUnix)
	if _, ok, _ := store.GetMeta(metaLastTrainUnix); !ok || last == 0 {
		// The bundle predates this bookkeeping (or was built elsewhere), so how much
		// has arrived since it was really trained is unknowable.
		//
		// Time is stamped as "now" so the first automatic retrain is a full interval
		// away rather than an unrequested several-minute train the moment the user
		// upgrades. The COUNT is deliberately left at zero: everything in the cache
		// then counts as new, so once the interval passes the retrain definitely
		// happens instead of the stale bundle surviving on a slow week.
		if err := store.SetMeta(metaLastTrainUnix, fmt.Sprint(time.Now().Unix())); err != nil {
			// The baseline could not be recorded, so postponing on the strength of it
			// would postpone forever, silently. Train instead: at worst that is one
			// unrequested run, against a model that otherwise never updates again.
			return true, "training bookkeeping is unwritable"
		}
		_ = store.SetMeta(metaLastTrainCount, "0")
		return false, ""
	}
	lastCount := metaInt(store, metaLastTrainCount)

	newMatches := count - lastCount
	age := time.Since(time.Unix(last, 0))
	if age < o.trainEvery || newMatches < int64(o.trainMinNew) {
		return false, ""
	}
	return true, fmt.Sprintf("%d new matches in %s", newMatches, age.Round(time.Hour))
}
