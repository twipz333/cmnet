// Package jobs runs the background work queue: a small worker pool over the
// persistent job rows in the cache, with mutual exclusion by resource rather than
// one global slot.
//
// Why by resource: the old design allowed exactly one long operation at a time,
// because sync and train genuinely conflict — they write the same watermarks and
// the same bundle. Scouting conflicts with neither, yet was locked out anyway, and
// so was every future kind of upkeep. Declaring what each kind touches lets the
// unrelated ones overlap and keeps the genuinely incompatible ones apart, which is
// the "no blocking, and merge only in a window where it is allowed" requirement.
package jobs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"cmnet/internal/cache"
)

// Resource is something a job needs exclusively while it runs.
type Resource string

const (
	// ResSyncWatermarks is the incremental scan bookkeeping in meta. Two jobs
	// advancing it at once interleave their bands and lose drafts between them.
	ResSyncWatermarks Resource = "sync-watermarks"
	// ResBundle is the trained model file. Two writers race over the same path.
	ResBundle Resource = "bundle"
	// ResSnapshot is the frozen scout snapshot.
	ResSnapshot Resource = "snapshot"
	// ResMatchMerge is the write side of matches/match_players. Collection stages
	// freely; the merge takes this briefly, which is the "merge window".
	ResMatchMerge Resource = "match-merge"
	// ResAssets is the on-disk image cache.
	ResAssets Resource = "assets"
)

// Job kinds. They live here rather than with the handlers because both front ends
// name them: the browser posts "start a sync", the queue stores the string, and a
// rename would orphan whatever was already queued under the old one.
const (
	KindSync        = "sync"
	KindTrain       = "train"
	KindScout       = "scout"
	KindScoutAllies = "scout-allies"
	KindAssets      = "assets"
	KindBackfill    = "backfill"
)

// Job priorities, re-exported so callers need not import the cache package just to
// say "the user asked for this".
const (
	PriorityBackground = cache.PriorityBackground
	PriorityManual     = cache.PriorityManual
)

// Log is how a running job reports progress.
type Log func(level, phase, text string)

// Info is the common case: an informational line with no phase.
func (l Log) Info(text string) { l(cache.LogInfo, "", text) }

// Handler executes one job. It must return promptly once ctx is cancelled.
type Handler func(ctx context.Context, j cache.Job, log Log) (string, error)

// Spec describes a kind of job.
type Spec struct {
	Kind string
	// Resources this kind holds for its whole run. Kinds with disjoint resource
	// sets run concurrently.
	Resources []Resource
	Handler   Handler
	// MaxAttempts caps retries; 0 means one attempt with no retry. A retry is for
	// transient failures (a source timing out), not for a job that is wrong.
	MaxAttempts int
	// RetryDelay is the wait before the next attempt. Doubled per attempt.
	RetryDelay time.Duration
}

// Queue runs jobs from the store.
type Queue struct {
	st      *cache.Store
	workers int

	mu      sync.Mutex
	specs   map[string]Spec
	running map[int64]*runState
	busy    map[Resource]int64 // resource -> job holding it

	wake     chan struct{}
	onChange func() // called when queue state changed; drives the UI push

	// logInterval/logBatch bound how often a chatty job hits the database.
	logInterval time.Duration
	logBatch    int
}

type runState struct {
	job    cache.Job
	cancel context.CancelFunc
	done   chan struct{}
	// The tail of the log, kept here so the live UI frame does not read it back out
	// of SQLite several times a second. Guarded by Queue.mu.
	lastLine  string
	lastPhase string
	lastSeq   int64
	lastAt    int64 // unix millis
}

// Options configure a queue.
type Options struct {
	// Workers is how many jobs may run at once. Resource exclusion still applies,
	// so this is an upper bound rather than a promise of parallelism.
	Workers int
	// OnChange is invoked after any state transition, so a front end can push a
	// frame instead of polling.
	OnChange func()
}

const (
	defaultWorkers     = 3
	defaultLogInterval = 250 * time.Millisecond
	defaultLogBatch    = 64
	// pollInterval bounds how long a queue sits idle before re-checking. Enqueue
	// wakes it immediately; this only covers retries becoming due.
	pollInterval = 2 * time.Second
)

// New builds a queue over the store.
func New(st *cache.Store, opts Options) *Queue {
	if opts.Workers <= 0 {
		opts.Workers = defaultWorkers
	}
	return &Queue{
		st:          st,
		workers:     opts.Workers,
		specs:       map[string]Spec{},
		running:     map[int64]*runState{},
		busy:        map[Resource]int64{},
		wake:        make(chan struct{}, 1),
		onChange:    opts.OnChange,
		logInterval: defaultLogInterval,
		logBatch:    defaultLogBatch,
	}
}

// Register adds a handler. Registering an unknown kind that is already queued is
// fine — such jobs simply wait until a build that knows the kind runs them.
func (q *Queue) Register(s Spec) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.specs[s.Kind] = s
}

// Enqueue adds a job. dedupe collapses it onto an identical pending one.
//
// A manual request preempts a running background job of the same kind. That
// replaces the old Yield handshake: the two contend for the same resource anyway,
// so without preemption a user pressing Sync would wait out however long the
// background pass had left. The displaced job is not lost — the worker proposes it
// again on its next pass, and its watermarks were committed as it went.
func (q *Queue) Enqueue(kind string, priority int, payload string, dedupe bool) (int64, error) {
	id, err := q.st.EnqueueJob(kind, priority, payload, dedupe)
	if err != nil {
		return 0, err
	}
	q.preempt(kind, priority, id)
	q.signal()
	q.changed()
	return id, nil
}

// preempt cancels running jobs of the same kind that rank below priority.
func (q *Queue) preempt(kind string, priority int, except int64) {
	q.mu.Lock()
	var victims []context.CancelFunc
	for id, rs := range q.running {
		if id != except && rs.job.Kind == kind && rs.job.Priority < priority {
			victims = append(victims, rs.cancel)
		}
	}
	q.mu.Unlock()
	for _, cancel := range victims {
		cancel()
	}
}

// Run drives the queue until ctx is done, then waits for running jobs to stop.
//
// Jobs left running by a previous process are recovered first: their rows say
// running, nothing is executing them, and the resources they claim would otherwise
// stay held forever.
func (q *Queue) Run(ctx context.Context) {
	if n, err := q.st.RecoverJobs(); err == nil && n > 0 {
		q.changed()
	}
	for {
		q.dispatch(ctx)
		select {
		case <-ctx.Done():
			q.waitAll()
			return
		case <-q.wake:
		case <-time.After(pollInterval):
		}
	}
}

// dispatch claims and starts as many jobs as the pool and the resources allow.
func (q *Queue) dispatch(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		q.mu.Lock()
		free := q.workers - len(q.running)
		skip := q.blockedKindsLocked()
		q.mu.Unlock()
		if free <= 0 {
			return
		}
		job, err := q.st.ClaimJob(skip)
		if err != nil || job == nil {
			return
		}
		q.start(ctx, *job)
	}
}

// blockedKindsLocked lists the kinds that cannot start right now: those needing a
// resource already held, and those with no registered handler.
func (q *Queue) blockedKindsLocked() []string {
	var skip []string
	for kind, spec := range q.specs {
		for _, r := range spec.Resources {
			if _, held := q.busy[r]; held {
				skip = append(skip, kind)
				break
			}
		}
	}
	// Unknown kinds are skipped by name too, so a job written by a newer build does
	// not get claimed and immediately failed by an older one.
	for _, k := range q.unknownKindsLocked() {
		skip = append(skip, k)
	}
	// Stable order keeps the generated SQL identical between calls, which matters
	// only for readability of logs and query plans — but costs nothing.
	sort.Strings(skip)
	return skip
}

// unknownKindsLocked returns kinds present in the queue that this build cannot run.
func (q *Queue) unknownKindsLocked() []string {
	active, err := q.st.ActiveJobs()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, j := range active {
		if _, ok := q.specs[j.Kind]; !ok && !seen[j.Kind] {
			seen[j.Kind] = true
			out = append(out, j.Kind)
		}
	}
	return out
}

// start launches a claimed job.
func (q *Queue) start(ctx context.Context, job cache.Job) {
	q.mu.Lock()
	spec, ok := q.specs[job.Kind]
	if !ok {
		q.mu.Unlock()
		// Claimed but unrunnable: hand it back rather than failing it, since a build
		// that knows the kind may run later.
		_ = q.st.RetryJob(job.ID, "no handler for kind "+job.Kind, time.Minute)
		return
	}
	jobCtx, cancel := context.WithCancel(ctx)
	rs := &runState{job: job, cancel: cancel, done: make(chan struct{})}
	q.running[job.ID] = rs
	for _, r := range spec.Resources {
		q.busy[r] = job.ID
	}
	q.mu.Unlock()
	q.changed()

	go func() {
		defer close(rs.done)
		lg := q.newLogger(job.ID)
		summary, err := spec.Handler(jobCtx, job, lg.log)
		lg.close()

		switch {
		case jobCtx.Err() != nil && ctx.Err() == nil:
			// Cancelled on purpose (the user, or a higher-priority job taking over).
			_ = q.st.FinishJob(job.ID, cache.JobCancelled, summary, "")
		case ctx.Err() != nil:
			// The whole process is going down; leave it claimable next start.
			_ = q.st.RetryJob(job.ID, "interrupted by shutdown", 0)
		case err != nil:
			if job.Attempts < spec.MaxAttempts {
				delay := spec.RetryDelay
				if delay <= 0 {
					delay = time.Minute
				}
				for i := 1; i < job.Attempts; i++ {
					delay *= 2
				}
				_ = q.st.RetryJob(job.ID, err.Error(), delay)
			} else {
				_ = q.st.FinishJob(job.ID, cache.JobFailed, "", err.Error())
			}
		default:
			_ = q.st.FinishJob(job.ID, cache.JobDone, summary, "")
		}

		cancel()
		q.mu.Lock()
		delete(q.running, job.ID)
		for _, r := range spec.Resources {
			if q.busy[r] == job.ID {
				delete(q.busy, r)
			}
		}
		q.mu.Unlock()
		q.signal()
		q.changed()
	}()
}

// Cancel stops a job. A pending one is marked cancelled; a running one is asked to
// stop and marks itself when it returns.
func (q *Queue) Cancel(id int64) error {
	q.mu.Lock()
	rs := q.running[id]
	q.mu.Unlock()
	if rs != nil {
		rs.cancel()
		return nil
	}
	if err := q.st.CancelJob(id); err != nil {
		return err
	}
	q.changed()
	return nil
}

// CancelKind stops every pending or running job of a kind. Used when a manual
// action supersedes the background copy of the same work.
func (q *Queue) CancelKind(kind string) error {
	active, err := q.st.ActiveJobs()
	if err != nil {
		return err
	}
	for _, j := range active {
		if j.Kind == kind {
			if err := q.Cancel(j.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Wait blocks until the job reaches a terminal state, returning its final row.
// Used by the command-line front end, which is synchronous by nature.
func (q *Queue) Wait(ctx context.Context, id int64) (cache.Job, error) {
	for {
		jobs, err := q.st.Jobs(0)
		if err != nil {
			return cache.Job{}, err
		}
		for _, j := range jobs {
			if j.ID != id {
				continue
			}
			switch j.State {
			case cache.JobDone, cache.JobFailed, cache.JobCancelled:
				return j, nil
			}
		}
		select {
		case <-ctx.Done():
			return cache.Job{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Follow streams a job's log lines to progress until it finishes, then returns its
// summary or error.
//
// This is the bridge for front ends that are synchronous by nature — the terminal
// shell and the command line both expect "run this and tell me how it went". The
// work still happens in the queue, so the same run is visible to the browser and
// survives this caller going away.
//
// Cancelling ctx cancels the job: for these callers the context IS the user
// pressing stop.
func (q *Queue) Follow(ctx context.Context, id int64, progress func(string)) (string, error) {
	var seq int64
	cancelled := false
	for {
		lines, err := q.st.JobLog(id, seq, 500)
		if err == nil {
			for _, l := range lines {
				seq = l.Seq
				if progress != nil {
					progress(l.Text)
				}
			}
		}

		job, done, err := q.lookup(id)
		if err != nil {
			return "", err
		}
		if done {
			// Drain whatever landed between the last read and the job finishing, so a
			// summary line emitted at the very end is not lost.
			if tail, err := q.st.JobLog(id, seq, 500); err == nil && progress != nil {
				for _, l := range tail {
					progress(l.Text)
				}
			}
			switch job.State {
			case cache.JobDone:
				return job.Result, nil
			case cache.JobCancelled:
				return "cancelled", nil
			default:
				if job.Error == "" {
					return "", fmt.Errorf("%s failed", job.Kind)
				}
				return "", fmt.Errorf("%s", job.Error)
			}
		}

		select {
		case <-ctx.Done():
			if !cancelled {
				cancelled = true
				_ = q.Cancel(id)
			}
			// Keep looping: the job has been asked to stop, and the caller wants its
			// final state, not a guess.
		case <-time.After(followInterval):
		}
	}
}

// followInterval is how often Follow polls. Log lines are batched anyway, so a
// tighter loop would only add queries.
const followInterval = 150 * time.Millisecond

// lookup returns a job and whether it is in a terminal state.
func (q *Queue) lookup(id int64) (cache.Job, bool, error) {
	job, err := q.st.Job(id)
	if err != nil {
		return cache.Job{}, false, err
	}
	if job == nil {
		return cache.Job{}, true, fmt.Errorf("job %d is gone", id)
	}
	switch job.State {
	case cache.JobDone, cache.JobFailed, cache.JobCancelled:
		return *job, true, nil
	}
	return *job, false, nil
}

// Active returns the queued and running jobs, oldest first.
func (q *Queue) Active() []cache.Job {
	jobs, err := q.st.ActiveJobs()
	if err != nil {
		return nil
	}
	return jobs
}

// Recent returns the newest jobs regardless of state.
func (q *Queue) Recent(limit int) []cache.Job {
	jobs, err := q.st.Jobs(limit)
	if err != nil {
		return nil
	}
	return jobs
}

// LastLine returns a job's most recent log line, its sequence and its timestamp.
//
// Cached in memory for running jobs: the live frame is built several times a
// second, and reading the tail of the log out of SQLite each time would make the
// UI's refresh rate a database workload.
func (q *Queue) LastLine(id int64) (text string, seq int64, atMillis int64, ok bool) {
	q.mu.Lock()
	if rs, running := q.running[id]; running && rs.lastLine != "" {
		text, seq, atMillis = rs.lastLine, rs.lastSeq, rs.lastAt
		q.mu.Unlock()
		return text, seq, atMillis, true
	}
	q.mu.Unlock()

	last, found, err := q.st.LastJobLog(id)
	if err != nil || !found {
		return "", 0, 0, false
	}
	return last.Text, last.Seq, last.At, true
}

// Note appends a line to a job's log from outside the handler — used to record
// something that happened after the job returned, such as a failed reload.
func (q *Queue) Note(id int64, level, text string) error {
	return q.st.AppendJobLog(id, level, "", text)
}

// Phase reports what a running job is currently doing, or "" when it has not
// said. This is the difference between "sync is running" and "sync is on its
// public Captains Mode band", which is the question a user watching it has.
func (q *Queue) Phase(id int64) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if rs, ok := q.running[id]; ok {
		return rs.lastPhase
	}
	return ""
}

// Log returns a page of a job's log after seq.
func (q *Queue) Log(id, afterSeq int64, limit int) ([]cache.JobLogEntry, error) {
	return q.st.JobLog(id, afterSeq, limit)
}

// Job returns one job, or nil when it has been trimmed away.
func (q *Queue) Job(id int64) (*cache.Job, error) { return q.st.Job(id) }

// Running reports the jobs currently executing, by id.
func (q *Queue) Running() []cache.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]cache.Job, 0, len(q.running))
	for _, rs := range q.running {
		out = append(out, rs.job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Busy reports whether any job holding r is running.
func (q *Queue) Busy(r Resource) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, held := q.busy[r]
	return held
}

func (q *Queue) waitAll() {
	q.mu.Lock()
	var dones []chan struct{}
	for _, rs := range q.running {
		dones = append(dones, rs.done)
	}
	q.mu.Unlock()
	for _, d := range dones {
		<-d
	}
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) changed() {
	q.mu.Lock()
	cb := q.onChange
	q.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// SetOnChange replaces the change callback. The front end registers itself after
// the queue is built.
func (q *Queue) SetOnChange(fn func()) {
	q.mu.Lock()
	q.onChange = fn
	q.mu.Unlock()
}

// --- logging ----------------------------------------------------------------

// logger batches a job's lines. A sync emits thousands; one transaction each made
// logging cost more than the work being logged.
// logTrimEvery is how many lines may accumulate between trims of one job's log.
const logTrimEvery = 500

type logger struct {
	q     *Queue
	jobID int64

	mu        sync.Mutex
	seq       int64
	trimmedAt int64
	pending   []cache.JobLogEntry
	closed    bool
	flushed   chan struct{}
	timer     *time.Timer
}

func (q *Queue) newLogger(jobID int64) *logger {
	seq, err := q.st.MaxJobLogSeq(jobID)
	if err != nil {
		seq = 0
	}
	return &logger{q: q, jobID: jobID, seq: seq, flushed: make(chan struct{})}
}

func (l *logger) log(level, phase, text string) {
	if level == "" {
		level = cache.LogInfo
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.seq++
	at := time.Now().UnixMilli()
	l.pending = append(l.pending, cache.JobLogEntry{
		JobID: l.jobID, Seq: l.seq, At: at,
		Level: level, Phase: phase, Text: text,
	})
	full := len(l.pending) >= l.q.logBatch
	if l.timer == nil {
		l.timer = time.AfterFunc(l.q.logInterval, l.flush)
	}
	seq := l.seq
	l.mu.Unlock()

	// Publish the tail immediately, before the batch is flushed: the UI shows the
	// latest line and its quiet timer from here, and waiting for the flush would
	// make a job look stalled for as long as the batch window.
	l.q.mu.Lock()
	if rs, ok := l.q.running[l.jobID]; ok {
		rs.lastLine, rs.lastSeq, rs.lastAt = text, seq, at
		// A line with no phase does not clear the last one: operations emit plenty
		// of unprefixed detail inside a phase, and blanking the label on each of
		// those would make it flicker.
		if phase != "" {
			rs.lastPhase = phase
		}
	}
	l.q.mu.Unlock()
	if full {
		l.flush()
	}
}

func (l *logger) flush() {
	l.mu.Lock()
	batch := l.pending
	l.pending = nil
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	l.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	if err := l.q.st.AppendJobLogBatch(batch); err == nil {
		l.q.changed()
	}
	// Checked on the batch boundary rather than per line: a job that runs for an
	// hour must not accumulate an unbounded log, and this costs one statement per
	// few dozen lines.
	l.mu.Lock()
	over := l.seq-l.trimmedAt > logTrimEvery
	if over {
		l.trimmedAt = l.seq
	}
	l.mu.Unlock()
	if over {
		_ = l.q.st.TrimJobLog(l.jobID, 0)
	}
}

func (l *logger) close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.flush()
}

// KindError formats the error a front end shows when a kind is not available in
// this build (the handler was never registered).
func KindError(kind string) error {
	return fmt.Errorf("%s is not available in this build", kind)
}
