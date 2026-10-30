package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cmnet/internal/cache"
)

func newStore(t *testing.T) *cache.Store {
	t.Helper()
	st, err := cache.Open(filepath.Join(t.TempDir(), "cmnet.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// runQueue starts q.Run in the background and registers cleanup that cancels and
// waits for Run to return before the store Close (registered earlier) runs.
// Without the wait, Cancel returns immediately while workers still hold the
// SQLite file, and Windows TempDir RemoveAll flakes.
func runQueue(t *testing.T, q *Queue) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("queue Run did not exit after cancel")
		}
	})
}

// blocker is a handler that reports when it started and waits to be released.
type blocker struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlocker() *blocker {
	return &blocker{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (b *blocker) handle(ctx context.Context, j cache.Job, log Log) (string, error) {
	b.started <- struct{}{}
	log(cache.LogInfo, "work", "started "+j.Kind)
	select {
	case <-b.release:
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *blocker) unblock() { b.once.Do(func() { close(b.release) }) }

func waitFor(t *testing.T, ch <-chan struct{}, d time.Duration, what string) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(d):
		t.Errorf("timed out waiting for %s", what)
		return false
	}
}

func notWithin(ch <-chan struct{}, d time.Duration) bool {
	select {
	case <-ch:
		return false
	case <-time.After(d):
		return true
	}
}

// Two kinds that touch nothing in common must overlap. This is the whole reason
// the queue exists: the old single slot serialised scouting behind a sync that had
// nothing to do with it.
func TestDisjointResourcesRunConcurrently(t *testing.T) {
	q := New(newStore(t), Options{Workers: 2})
	syncB, scoutB := newBlocker(), newBlocker()
	q.Register(Spec{Kind: "sync", Resources: []Resource{ResSyncWatermarks}, Handler: syncB.handle})
	q.Register(Spec{Kind: "scout", Resources: []Resource{ResSnapshot}, Handler: scoutB.handle})
	runQueue(t, q)

	if _, err := q.Enqueue("sync", PriorityBackground, "", false); err != nil {
		t.Fatalf("enqueue sync: %v", err)
	}
	if _, err := q.Enqueue("scout", PriorityBackground, "", false); err != nil {
		t.Fatalf("enqueue scout: %v", err)
	}

	waitFor(t, syncB.started, 3*time.Second, "sync to start")
	waitFor(t, scoutB.started, 3*time.Second, "scout to start alongside sync")

	syncB.unblock()
	scoutB.unblock()
}

// Two kinds that write the same thing must not overlap, however many workers are
// free. Interleaved watermark writers lose drafts between their bands.
func TestSharedResourceSerialises(t *testing.T) {
	q := New(newStore(t), Options{Workers: 4})
	first, second := newBlocker(), newBlocker()
	q.Register(Spec{Kind: "sync", Resources: []Resource{ResSyncWatermarks}, Handler: first.handle})
	q.Register(Spec{Kind: "backfill", Resources: []Resource{ResSyncWatermarks}, Handler: second.handle})
	runQueue(t, q)

	q.Enqueue("sync", PriorityBackground, "", false)
	q.Enqueue("backfill", PriorityBackground, "", false)

	waitFor(t, first.started, 3*time.Second, "the first job to start")
	if !notWithin(second.started, 500*time.Millisecond) {
		t.Fatal("both holders of sync-watermarks ran at once")
	}
	first.unblock()
	waitFor(t, second.started, 3*time.Second, "the second job to start once the resource freed")
	second.unblock()
}

// A user pressing Sync must not queue behind background upkeep that was enqueued
// first.
func TestPriorityBeatsInsertionOrder(t *testing.T) {
	st := newStore(t)
	if _, err := st.EnqueueJob("bg", PriorityBackground, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnqueueJob("manual", PriorityManual, "", false); err != nil {
		t.Fatal(err)
	}
	got, err := st.ClaimJob(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != "manual" {
		t.Fatalf("claimed %+v, want the manual job first", got)
	}
}

// The background worker re-proposes the same upkeep every pass. Without dedupe a
// queue busy for an hour comes back to sixty identical syncs.
func TestDedupeCollapsesPending(t *testing.T) {
	st := newStore(t)
	id1, err := st.EnqueueJob("sync", PriorityBackground, "", true)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.EnqueueJob("sync", PriorityManual, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("dedupe created a second job (%d, %d)", id1, id2)
	}
	active, err := st.ActiveJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("queue holds %d jobs, want 1", len(active))
	}
	if active[0].Priority != PriorityManual {
		t.Errorf("priority = %d, want the manual request to raise it to %d",
			active[0].Priority, PriorityManual)
	}
}

// A row that says "running" with no process behind it holds its resources forever.
// Recovery at startup is what stops a crash from deadlocking the next run.
func TestRecoverReleasesInterruptedJobs(t *testing.T) {
	st := newStore(t)
	if _, err := st.EnqueueJob("sync", PriorityBackground, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(nil); err != nil {
		t.Fatal(err)
	}
	n, err := st.RecoverJobs()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recovered %d jobs, want 1", n)
	}
	got, err := st.ClaimJob(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the interrupted job was not claimable again")
	}
	if got.Attempts != 2 {
		t.Errorf("attempts = %d, want the recovered claim to be counted", got.Attempts)
	}
}

// A transient failure retries; a persistent one gives up rather than spinning.
func TestRetryThenFail(t *testing.T) {
	st := newStore(t)
	q := New(st, Options{Workers: 1})
	var calls atomic.Int32
	done := make(chan struct{})
	q.Register(Spec{
		Kind: "flaky",
		Handler: func(ctx context.Context, j cache.Job, log Log) (string, error) {
			if calls.Add(1) >= 2 {
				select {
				case <-done:
				default:
					close(done)
				}
			}
			return "", errors.New("source timed out")
		},
		MaxAttempts: 2,
		RetryDelay:  10 * time.Millisecond,
	})
	runQueue(t, q)
	id, err := q.Enqueue("flaky", PriorityBackground, "", false)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, done, 5*time.Second, "the second attempt")

	final, err := q.Wait(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != cache.JobFailed {
		t.Errorf("state = %q, want %q after exhausting attempts", final.State, cache.JobFailed)
	}
	if final.Error == "" {
		t.Error("a failed job kept no error text, so the UI can say nothing about why")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("handler ran %d times, want exactly MaxAttempts=2", got)
	}
}

// Cancelling a running job stops it and records the outcome as cancelled — not as
// a failure, which would light up the UI with an error the user caused on purpose.
func TestCancelRunningJob(t *testing.T) {
	q := New(newStore(t), Options{Workers: 1})
	b := newBlocker()
	q.Register(Spec{Kind: "sync", Resources: []Resource{ResSyncWatermarks}, Handler: b.handle})
	runQueue(t, q)

	id, err := q.Enqueue("sync", PriorityManual, "", false)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, b.started, 3*time.Second, "the job to start")
	if err := q.Cancel(id); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	final, err := q.Wait(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != cache.JobCancelled {
		t.Errorf("state = %q, want %q", final.State, cache.JobCancelled)
	}
	if !q.Busy(ResSyncWatermarks) {
		// nothing to assert here beyond the resource being released below
	}
	deadline := time.Now().Add(2 * time.Second)
	for q.Busy(ResSyncWatermarks) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if q.Busy(ResSyncWatermarks) {
		t.Error("the cancelled job never released its resource")
	}
}

// Log lines survive the process: that is the difference between the old in-memory
// []string and a queue whose history a user can open an hour later.
func TestJobLogPersistsAndPaginates(t *testing.T) {
	st := newStore(t)
	q := New(st, Options{Workers: 1})
	q.Register(Spec{Kind: "chatty", Handler: func(ctx context.Context, j cache.Job, log Log) (string, error) {
		for i := 0; i < 200; i++ {
			log(cache.LogInfo, "phase", "line")
		}
		log(cache.LogWarn, "phase", "careful")
		return "ok", nil
	}})
	runQueue(t, q)

	id, err := q.Enqueue("chatty", PriorityManual, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Wait(context.Background(), id); err != nil {
		t.Fatal(err)
	}

	first, err := st.JobLog(id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 100 {
		t.Fatalf("first page has %d lines, want 100", len(first))
	}
	if first[0].Seq != 1 {
		t.Errorf("first seq = %d, want 1", first[0].Seq)
	}
	rest, err := st.JobLog(id, first[len(first)-1].Seq, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 101 {
		t.Fatalf("second page has %d lines, want 101", len(rest))
	}
	last := rest[len(rest)-1]
	if last.Level != cache.LogWarn || last.Text != "careful" {
		t.Errorf("last line = %+v, want the warning", last)
	}
}

// A job whose kind this build does not know must not be claimed and failed: a
// newer build may be the one that can run it.
func TestUnknownKindIsLeftAlone(t *testing.T) {
	st := newStore(t)
	q := New(st, Options{Workers: 1})
	id, err := q.Enqueue("from-the-future", PriorityManual, "", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	q.Run(ctx)

	jobs, err := st.Jobs(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.ID == id && j.State == cache.JobFailed {
			t.Error("an unknown kind was failed instead of left pending")
		}
	}
}

// A hold stops a running background job, keeps background work from starting
// while held, and puts the stopped job back in the queue to resume afterwards.
func TestHoldBackgroundPausesAndResumes(t *testing.T) {
	q := New(newStore(t), Options{Workers: 3})
	var runs atomic.Int32
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	q.Register(Spec{Kind: "sync", Resources: []Resource{ResSyncWatermarks}, Handler: func(ctx context.Context, j cache.Job, log Log) (string, error) {
		runs.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}})
	runQueue(t, q)

	id, err := q.Enqueue("sync", PriorityBackground, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, started, 2*time.Second, "background sync") {
		return
	}
	done := q.HoldBackground()
	if !notWithin(started, 500*time.Millisecond) {
		t.Fatal("background job restarted while held")
	}
	j, err := q.Job(id)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != cache.JobPending {
		t.Fatalf("held job state = %s, want pending (back in the queue)", j.State)
	}
	done()
	if !waitFor(t, started, 3*time.Second, "resumed sync") {
		return
	}
	close(release)
	if runs.Load() != 2 {
		t.Fatalf("runs = %d, want 2 (started, paused, resumed)", runs.Load())
	}
}
