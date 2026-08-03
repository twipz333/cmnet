package session

import (
	"context"
	"sync"
	"time"
)

// Auto keeps the local cache and the model fresh on its own, so collecting data
// is not something the user has to remember to do.
//
// Why this exists: the corpus this advisor needs cannot be fetched in one sitting.
// STRATZ only answers for ~30% of public matches, which puts the ceiling at about
// 3000 drafts a day — days of wall-clock time for the first fill, then a trickle
// of a few hundred a day to stay current. Running that as one long foreground
// command means one bad Explorer answer ends the day's collection with an exit
// code of 0 and nothing to show for it.
//
// The design is deliberately dumb about state: everything that must survive the
// process being killed already lives in SQLite (the scan watermarks) or in a file
// written atomically (the bundle). The worker keeps nothing that would have to be
// checkpointed, so "recovery" after a crash is just starting the loop again.
type Auto struct {
	cfg AutoConfig

	mu        sync.Mutex
	phase     AutoPhase
	line      string // last progress line from the running pass
	last      string // summary of the last finished pass
	lastErr   string
	lastAt    time.Time
	nextAt    time.Time
	yields    int           // manual operations currently holding priority
	draftAt   time.Time     // last TouchDraft; see draftActive
	pause     time.Duration // current wait between passes, after backoff
	trained   func() error  // front-end hook run after an automatic retrain
	cancelCur context.CancelFunc
	// running is closed when the pass in flight returns (nil when none is). Yield
	// waits on it, because cancelling is not the same as stopping: opSync only
	// notices a cancelled context at band boundaries, so without this barrier a
	// manual sync would start while the background one is still finishing its band
	// — with both of them writing the same watermarks.
	running chan struct{}
	// trainFailedAt is when an automatic training run last failed. While that is
	// recent, passes go back to collecting: TrainDue stays true after a failure
	// (the bookkeeping only advances on success), so retrying every pass would stop
	// collection entirely for as long as training is broken.
	trainFailedAt time.Time
	// reloadPending records that a background retrain finished but its bundle has
	// not been applied yet, because a draft was on screen when it did.
	reloadPending bool

	// wake interrupts the pause between passes (a manual op finished, so the worker
	// need not sit out the rest of its timer). Buffered: signalling must not block a
	// caller that is holding no lock and expecting to return immediately.
	wake chan struct{}
}

// AutoPhase is what the worker is doing right now.
type AutoPhase string

const (
	AutoIdle     AutoPhase = "idle"   // waiting out the pause between passes
	AutoSyncing  AutoPhase = "sync"   //
	AutoTraining AutoPhase = "train"  //
	AutoPaused   AutoPhase = "paused" // stood aside for a manual operation
	AutoOff      AutoPhase = "off"    // not running at all
)

// AutoOp is one long operation (a sync or a train pass).
type AutoOp func(ctx context.Context, progress func(string)) (string, error)

// AutoConfig wires the worker to the app. Sync is required; everything else is
// optional and disables the corresponding behaviour when nil.
type AutoConfig struct {
	// Sync runs one collection pass. It should be capped small enough to finish in
	// minutes: the pass boundary is the checkpoint, and an interrupted pass loses
	// only the drafts it had not yet written.
	Sync AutoOp
	// Train rebuilds the bundle.
	Train AutoOp
	// TrainDue reports whether retraining is warranted now, with a reason to show
	// the user. Consulted before every pass; nil means never train automatically.
	TrainDue func() (bool, string)
	// Count returns how many matches the cache holds, so the worker can tell whether
	// a pass actually collected anything. nil disables the idle backoff.
	Count func() (int64, error)

	// Idle is the pause after a pass that collected something, MaxIdle the ceiling
	// the pause backs off to when passes come back empty (the window is exhausted
	// and new drafts only trickle in).
	Idle    time.Duration
	MaxIdle time.Duration
}

// AutoStatus is the worker's state for the UI. Plain data, safe to serialise.
type AutoStatus struct {
	Phase AutoPhase `json:"phase"`
	Line  string    `json:"line"`
	Last  string    `json:"last"`
	Error string    `json:"error,omitempty"`
	// NextInSeconds is how long until the next pass; 0 unless idle.
	NextInSeconds int `json:"nextInSeconds"`
}

const (
	defaultAutoIdle    = 1 * time.Minute
	defaultAutoMaxIdle = 30 * time.Minute
)

// NewAuto builds a worker. It does nothing until Run is called.
func NewAuto(cfg AutoConfig) *Auto {
	if cfg.Idle <= 0 {
		cfg.Idle = defaultAutoIdle
	}
	if cfg.MaxIdle < cfg.Idle {
		cfg.MaxIdle = defaultAutoMaxIdle
	}
	if cfg.MaxIdle < cfg.Idle {
		cfg.MaxIdle = cfg.Idle
	}
	return &Auto{cfg: cfg, phase: AutoOff, wake: make(chan struct{}, 1)}
}

// Run drives the loop until ctx is done. Blocks; call it in a goroutine.
func (a *Auto) Run(ctx context.Context) {
	if a.cfg.Sync == nil {
		return
	}
	pause := a.cfg.Idle
	for ctx.Err() == nil {
		if !a.awaitTurn(ctx) {
			return
		}
		// A bundle whose apply was deferred because a draft was open gets picked up
		// here, once the draft is over — otherwise it would sit unapplied until the
		// next retrain came due, a day later.
		a.applyTrained()
		progressed, err := a.pass(ctx)
		if ctx.Err() != nil {
			return
		}
		// Back off when passes stop producing, so an exhausted window costs a couple
		// of requests an hour instead of hammering a source that has nothing new. Any
		// progress at all resets it: the collection is not done, it was just slow.
		switch {
		case err != nil:
			pause = min(max(pause*2, a.cfg.Idle), a.cfg.MaxIdle)
		case progressed:
			pause = a.cfg.Idle
		default:
			pause = min(pause*2, a.cfg.MaxIdle)
		}
		a.setIdle(pause)
		if !a.sleep(ctx, pause) {
			return
		}
	}
}

// pass runs one unit of work: a training run when one is due, otherwise a sync.
// It reports whether anything was accomplished, which drives the backoff.
func (a *Auto) pass(ctx context.Context) (progressed bool, err error) {
	op, name, reason := a.next()
	if op == nil {
		return false, nil
	}
	// Whether the pass achieved anything is measured, not parsed out of the summary
	// string: a sync that stored nothing and a sync that stored 300 both end with a
	// cheerful "sync done".
	before := a.count()
	summary, cancelled, err := a.runOp(ctx, name, op)
	if err != nil {
		if name == AutoTraining {
			// Hand the next passes back to collection; see trainRetryCooldown.
			a.mu.Lock()
			a.trainFailedAt = time.Now()
			a.mu.Unlock()
		}
		return false, err
	}
	if cancelled {
		// Nothing to book: a training run stopped halfway produced no bundle, and
		// treating it as done would apply one that was never written.
		return false, nil
	}
	if reason != "" {
		summary = reason + " → " + summary
	}
	a.finish(summary, nil)
	// Any training run that returned without an error built a bundle, whether or not
	// it had anything to say about it: gating this on a nonempty summary would leave
	// a good bundle unapplied because the operation happened to be terse.
	if name == AutoTraining {
		a.mu.Lock()
		a.trainFailedAt = time.Time{}
		a.reloadPending = true
		a.mu.Unlock()
		a.applyTrained()
	}
	after := a.count()
	return name == AutoTraining || before < 0 || after > before, nil
}

// applyTrained hands a freshly built bundle to the front end.
//
// A bundle nobody loads is a file: the front end holds the previous one in memory
// for the whole session, so without this an automatic retrain would only take
// effect after a restart. Two things make it more than a straight call:
//
//   - it is deferred while a draft is open. The check before training is not
//     enough — training takes minutes, and a draft that starts during it would
//     otherwise have its board wiped by the reload the moment training ends.
//   - a failed reload keeps the pending flag AND surfaces the error, instead of
//     being dropped on the floor while the model is reported as up to date.
func (a *Auto) applyTrained() {
	a.mu.Lock()
	pending, cb, inDraft := a.reloadPending, a.trained, a.draftLiveLocked()
	a.mu.Unlock()
	if !pending || cb == nil || inDraft {
		return
	}
	if err := cb(); err != nil {
		a.mu.Lock()
		a.lastErr = "new bundle not applied: " + err.Error()
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.reloadPending = false
	a.mu.Unlock()
}

// count is the current match count, or -1 when the app did not supply a counter
// (in which case the worker cannot tell a productive pass from an empty one and
// treats every pass as productive).
func (a *Auto) count() int64 {
	if a.cfg.Count == nil {
		return -1
	}
	n, err := a.cfg.Count()
	if err != nil {
		return -1
	}
	return n
}

// trainRetryCooldown is how long collection gets the passes to itself after a
// failed training run.
//
// Without it a broken train starves the sync entirely: the trigger only clears
// when a run SUCCEEDS, so a train that fails for a structural reason (an empty
// window, a source outage) would be chosen again on every single pass, forever,
// and the cache would stop growing — the opposite of what the worker is for.
const trainRetryCooldown = time.Hour

// next picks the operation for this pass.
func (a *Auto) next() (op AutoOp, name AutoPhase, reason string) {
	if a.cfg.Train != nil && a.cfg.TrainDue != nil && a.trainAllowed() {
		// Holding training back mid-draft is not about cost: Train replaces the bundle
		// the draft screen is reading, and the user watching a ranked list does not
		// need it to shift under them between two picks.
		if due, why := a.cfg.TrainDue(); due && !a.draftActive() {
			return a.cfg.Train, AutoTraining, why
		}
	}
	return a.cfg.Sync, AutoSyncing, ""
}

// trainAllowed reports whether enough time has passed since training last failed.
func (a *Auto) trainAllowed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.trainFailedAt.IsZero() || time.Since(a.trainFailedAt) >= trainRetryCooldown
}

// SetOnTrained registers what to do after an automatic retrain — reloading the
// draft session, so a bundle built in the background takes effect without a
// restart. Set by the front end, which owns the session. An error means the new
// bundle was not applied: it is shown to the user and the apply is retried later.
func (a *Auto) SetOnTrained(fn func() error) {
	a.mu.Lock()
	a.trained = fn
	a.mu.Unlock()
}

// draftTTL is how long a TouchDraft heartbeat keeps the draft considered live.
// Long enough to span the gaps between a user's actions, short enough that a
// closed browser or a crashed front end stops holding training back.
const draftTTL = 2 * time.Minute

// TouchDraft says a draft is on screen right now. The front end calls it while
// the draft view is being shown; the flag expires on its own after draftTTL.
//
// A heartbeat rather than an on/off switch, because both failure modes of a
// switch are real: a browser closed mid-draft would leave it stuck on and block
// training forever, and a front end that forgot to set it would let training run
// mid-draft. Expiry makes the first impossible and the second temporary.
//
// It gates training only. Applying a new bundle means rebuilding the session,
// which clears the board — unacceptable between two picks, and worth deferring by
// an hour. Collecting matches changes nothing on screen and continues throughout.
func (a *Auto) TouchDraft() {
	a.mu.Lock()
	a.draftAt = time.Now()
	a.mu.Unlock()
}

func (a *Auto) draftActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.draftLiveLocked()
}

// draftLiveLocked is draftActive for callers already holding the lock.
func (a *Auto) draftLiveLocked() bool {
	return !a.draftAt.IsZero() && time.Since(a.draftAt) < draftTTL
}

// runOp executes one operation under a cancellable context that Yield can stop.
func (a *Auto) runOp(ctx context.Context, name AutoPhase, op AutoOp) (string, bool, error) {
	opCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.mu.Lock()
	// Claiming the slot and checking for a yield happen under ONE lock, because a
	// Yield landing between "awaitTurn said go" and "running is installed" would
	// find nothing to wait for and let the manual run start alongside this one.
	if a.yields > 0 {
		a.mu.Unlock()
		cancel()
		return "", true, nil
	}
	a.phase = name
	a.line = ""
	a.lastErr = ""
	a.cancelCur = cancel
	a.running = done
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		a.cancelCur = nil
		a.running = nil
		a.mu.Unlock()
		close(done) // releases anyone waiting in Yield
	}()

	summary, err := op(opCtx, a.progress)
	// A pass cancelled to make way for a manual operation is neither a failure nor
	// a success: reporting it as a failure would light up the UI with an error the
	// user caused on purpose, and reporting it as a success would book a half-done
	// training run as a finished one — which is what applies its bundle.
	if opCtx.Err() != nil && ctx.Err() == nil {
		a.finish("paused for a manual operation", nil)
		return "", true, nil
	}
	if err != nil {
		a.finish("", err)
		return "", false, err
	}
	return summary, false, nil
}

func (a *Auto) progress(line string) {
	a.mu.Lock()
	a.line = line
	a.mu.Unlock()
}

func (a *Auto) finish(summary string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.phase = a.idlePhaseLocked()
	a.line = ""
	a.lastAt = time.Now()
	if err != nil {
		a.lastErr = err.Error()
		return
	}
	a.lastErr = ""
	if summary != "" {
		a.last = summary
	}
}

func (a *Auto) setIdle(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.phase = a.idlePhaseLocked()
	a.pause = d
	a.nextAt = time.Now().Add(d)
}

// idlePhaseLocked is the phase to fall back to when a pass ends: still "paused"
// while a manual operation holds priority, since that is why nothing is running.
func (a *Auto) idlePhaseLocked() AutoPhase {
	if a.yields > 0 {
		return AutoPaused
	}
	return AutoIdle
}

// currentPause is the pause the backoff has settled on.
func (a *Auto) currentPause() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pause
}

// Yield hands priority to a manual operation: the running pass is cancelled and no
// new one starts until the returned release is called.
//
// It covers manual sync and train specifically, not scouting. Those two contend
// with the worker over the very things it owns — the scan watermarks and the
// bundle file — so running both at once is not slower, it is incoherent. Scouting
// touches neither and runs alongside collection.
func (a *Auto) Yield() (release func()) {
	a.mu.Lock()
	a.yields++
	cancel, running := a.cancelCur, a.running
	// Paused regardless of what it was doing, including before its first pass: the
	// worker is running, it is just standing aside, and reporting "off" would read
	// as background collection being disabled altogether.
	a.phase = AutoPaused
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Wait for the pass to actually finish, not merely to be told to. opSync checks
	// its context between bands, so the cancelled pass is still mid-band — and both
	// it and the manual run about to start write the same watermarks.
	if running != nil {
		<-running
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.yields--
			resumed := a.yields == 0
			a.mu.Unlock()
			if resumed {
				a.signal() // resume now instead of sitting out the rest of the pause
			}
		})
	}
}

// awaitTurn blocks while a manual operation holds priority. It reports false when
// ctx ended.
func (a *Auto) awaitTurn(ctx context.Context) bool {
	for {
		a.mu.Lock()
		held := a.yields > 0
		a.mu.Unlock()
		if !held {
			return ctx.Err() == nil
		}
		if !a.sleep(ctx, time.Second) {
			return false
		}
	}
}

// sleep waits for d, returning false if ctx ended. A wake signal cuts it short.
func (a *Auto) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-a.wake:
		return true
	case <-t.C:
		return true
	}
}

func (a *Auto) signal() {
	select {
	case a.wake <- struct{}{}:
	default: // a wake-up is already pending; one is enough
	}
}

// Status reports what the worker is doing, for the status bar / API.
func (a *Auto) Status() AutoStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := AutoStatus{Phase: a.phase, Line: a.line, Last: a.last, Error: a.lastErr}
	if a.phase == AutoIdle && !a.nextAt.IsZero() {
		if d := time.Until(a.nextAt); d > 0 {
			st.NextInSeconds = int(d.Seconds())
		}
	}
	return st
}
