package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestAuto builds a worker with pauses short enough for a test to observe
// several passes.
func newTestAuto(cfg AutoConfig) *Auto {
	if cfg.Idle == 0 {
		cfg.Idle = time.Millisecond
	}
	if cfg.MaxIdle == 0 {
		cfg.MaxIdle = 5 * time.Millisecond
	}
	return NewAuto(cfg)
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The point of the worker: collection keeps happening without anyone asking.
func TestAutoRunsSyncRepeatedly(t *testing.T) {
	var passes atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			passes.Add(1)
			return "synced", nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "several sync passes", func() bool { return passes.Load() >= 3 })
	if got := a.Status().Last; got != "synced" {
		t.Errorf("last summary = %q, want %q", got, "synced")
	}
}

// A manual operation must be able to take the source over: two syncs walking the
// same watermarks at once is not slow, it is incoherent.
func TestYieldStopsAndResumesTheWorker(t *testing.T) {
	var (
		running   atomic.Bool
		cancelled = make(chan struct{}, 1)
		passes    atomic.Int64
	)
	a := newTestAuto(AutoConfig{
		Sync: func(ctx context.Context, _ func(string)) (string, error) {
			passes.Add(1)
			running.Store(true)
			defer running.Store(false)
			select {
			case <-ctx.Done():
				select {
				case cancelled <- struct{}{}:
				default:
				}
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
				return "synced", nil
			}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "the first pass to start", running.Load)
	release := a.Yield()

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Yield did not cancel the running pass")
	}
	if got := a.Status().Phase; got != AutoPaused && got != AutoIdle {
		t.Errorf("phase while yielded = %q, want paused or idle", got)
	}

	// While a manual operation holds priority, no new pass may start.
	before := passes.Load()
	time.Sleep(50 * time.Millisecond)
	if after := passes.Load(); after != before {
		t.Errorf("worker started %d pass(es) while yielded", after-before)
	}

	release()
	waitFor(t, "the worker to resume", func() bool { return passes.Load() > before })
}

// A pass cancelled to make way for the user is not an error, and must not be
// reported as one.
func TestYieldedPassIsNotReportedAsFailure(t *testing.T) {
	started := make(chan struct{}, 1)
	a := newTestAuto(AutoConfig{
		Sync: func(ctx context.Context, _ func(string)) (string, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	<-started
	release := a.Yield()
	defer release()
	waitFor(t, "the pass to finish", func() bool { return a.Status().Phase != AutoSyncing })

	if err := a.Status().Error; err != "" {
		t.Errorf("a yielded pass reported an error: %q", err)
	}
}

// Training replaces the bundle, and applying it rebuilds the session — which
// clears the board. Mid-draft that is unacceptable, so the retrain waits.
func TestTrainingIsHeldBackDuringADraft(t *testing.T) {
	var trains, syncs atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			syncs.Add(1)
			return "synced", nil
		},
		Train: func(context.Context, func(string)) (string, error) {
			trains.Add(1)
			return "trained", nil
		},
		TrainDue: func() (bool, string) { return true, "due" },
	})
	a.TouchDraft()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "collection to continue during the draft", func() bool { return syncs.Load() >= 2 })
	if n := trains.Load(); n != 0 {
		t.Errorf("trained %d time(s) mid-draft; the board would have been cleared", n)
	}
}

// ...and once the draft is over (the heartbeat expires) it goes ahead.
func TestTrainingRunsWhenNoDraftIsOpen(t *testing.T) {
	var trains atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync:     func(context.Context, func(string)) (string, error) { return "synced", nil },
		Train:    func(context.Context, func(string)) (string, error) { trains.Add(1); return "trained", nil },
		TrainDue: func() (bool, string) { return true, "due" },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "an automatic retrain", func() bool { return trains.Load() >= 1 })
}

// The draft heartbeat must expire: a front end that dies mid-draft would
// otherwise block training forever.
func TestDraftHeartbeatExpires(t *testing.T) {
	a := newTestAuto(AutoConfig{Sync: func(context.Context, func(string)) (string, error) { return "", nil }})
	a.TouchDraft()
	if !a.draftActive() {
		t.Fatal("draft should be active immediately after TouchDraft")
	}
	a.mu.Lock()
	a.draftAt = time.Now().Add(-draftTTL - time.Second)
	a.mu.Unlock()
	if a.draftActive() {
		t.Error("a stale heartbeat still counts as an open draft; training would never run again")
	}
}

// A retrained bundle nobody reloads is just a file on disk.
func TestOnTrainedFires(t *testing.T) {
	var reloads atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync:     func(context.Context, func(string)) (string, error) { return "synced", nil },
		Train:    func(context.Context, func(string)) (string, error) { return "trained", nil },
		TrainDue: func() (bool, string) { return true, "due" },
	})
	a.SetOnTrained(func() error { reloads.Add(1); return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "the reload hook to fire", func() bool { return reloads.Load() >= 1 })
}

// Passes that collect nothing must back off, so an exhausted window costs a
// couple of requests an hour instead of hammering the source.
func TestIdleBacksOffWhenNothingIsCollected(t *testing.T) {
	var count int64 // never changes: every pass collects nothing
	a := NewAuto(AutoConfig{
		Sync:    func(context.Context, func(string)) (string, error) { return "nothing new", nil },
		Count:   func() (int64, error) { return count, nil },
		Idle:    10 * time.Millisecond,
		MaxIdle: 400 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "the pause to grow past the floor", func() bool {
		st := a.Status()
		return st.Phase == AutoIdle && st.NextInSeconds >= 0 && a.currentPause() > 10*time.Millisecond
	})
}

// A productive pass resets the backoff: collection is not finished, it was slow.
func TestIdleResetsWhenAPassCollects(t *testing.T) {
	var count atomic.Int64
	a := NewAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			count.Add(10)
			return "stored 10", nil
		},
		Count:   func() (int64, error) { return count.Load(), nil },
		Idle:    5 * time.Millisecond,
		MaxIdle: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "several productive passes", func() bool { return count.Load() >= 30 })
	if p := a.currentPause(); p > 5*time.Millisecond {
		t.Errorf("pause = %v after productive passes, want it reset to the floor", p)
	}
}

// A failing source must not spin: the pause grows instead.
func TestFailingPassIsReported(t *testing.T) {
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			return "", errors.New("explorer is down")
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "the failure to surface", func() bool { return a.Status().Error != "" })
	if got := a.Status().Error; got != "explorer is down" {
		t.Errorf("error = %q, want the source's message", got)
	}
}

// Cancelling the context stops the loop; nothing keeps running behind the app.
func TestRunStopsOnContextCancel(t *testing.T) {
	done := make(chan struct{})
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) { return "synced", nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { a.Run(ctx); close(done) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// Yield is reentrant: two manual operations overlapping must not resume the
// worker when only the first one finishes.
func TestNestedYieldsResumeOnlyWhenAllRelease(t *testing.T) {
	var passes atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			passes.Add(1)
			return "synced", nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	waitFor(t, "the worker to start", func() bool { return passes.Load() > 0 })

	first, second := a.Yield(), a.Yield()
	time.Sleep(20 * time.Millisecond)
	before := passes.Load()

	first()
	time.Sleep(50 * time.Millisecond)
	if after := passes.Load(); after != before {
		t.Errorf("worker resumed with one yield still held (%d new passes)", after-before)
	}

	second()
	waitFor(t, "the worker to resume once every yield is released", func() bool { return passes.Load() > before })
}

// Cancelling is not stopping: opSync only notices a cancelled context between
// bands, so Yield must not return while the pass is still writing.
func TestYieldWaitsForThePassToActuallyStop(t *testing.T) {
	var (
		inFlight atomic.Bool
		started  = make(chan struct{}, 1)
	)
	a := newTestAuto(AutoConfig{
		Sync: func(ctx context.Context, _ func(string)) (string, error) {
			inFlight.Store(true)
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			// Stand in for opSync finishing the band it was in the middle of.
			time.Sleep(100 * time.Millisecond)
			inFlight.Store(false)
			return "partial", nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	<-started
	release := a.Yield()
	defer release()
	if inFlight.Load() {
		t.Error("Yield returned while the background pass was still running; " +
			"a manual sync would now be writing the same watermarks")
	}
}

// A train that keeps failing must not starve collection: TrainDue stays true
// until a run succeeds, so without a cooldown every pass would be a doomed train.
func TestFailingTrainDoesNotStarveSync(t *testing.T) {
	var trains, syncs atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			syncs.Add(1)
			return "synced", nil
		},
		Train: func(context.Context, func(string)) (string, error) {
			trains.Add(1)
			return "", errors.New("no matches in the training window")
		},
		TrainDue: func() (bool, string) { return true, "due" },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitFor(t, "collection to continue after training failed", func() bool { return syncs.Load() >= 3 })
	if n := trains.Load(); n > 1 {
		t.Errorf("train was retried %d times despite the cooldown; collection would starve", n)
	}
}

// Training takes minutes. A draft that starts DURING it must not have its board
// wiped by the reload that follows.
func TestReloadIsDeferredWhenADraftStartsDuringTraining(t *testing.T) {
	var reloads atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync:  func(context.Context, func(string)) (string, error) { return "synced", nil },
		Train: func(context.Context, func(string)) (string, error) { return "trained", nil },
		TrainDue: func() func() (bool, string) {
			var once bool
			return func() (bool, string) {
				if once {
					return false, ""
				}
				once = true
				return true, "due"
			}
		}(),
	})
	a.SetOnTrained(func() error { reloads.Add(1); return nil })

	// The draft opens while training is in flight, so the pre-training check passed.
	a.mu.Lock()
	a.reloadPending = true
	a.mu.Unlock()
	a.TouchDraft()
	a.applyTrained()
	if reloads.Load() != 0 {
		t.Fatal("the bundle was applied mid-draft; the board would have been cleared")
	}

	// Once the draft is over the deferred apply goes through.
	a.mu.Lock()
	a.draftAt = time.Now().Add(-draftTTL - time.Second)
	a.mu.Unlock()
	a.applyTrained()
	if reloads.Load() != 1 {
		t.Errorf("deferred apply did not run after the draft ended (reloads = %d)", reloads.Load())
	}
}

// A reload that fails must be visible and retried, not silently dropped while the
// model is reported as up to date.
func TestFailedReloadIsReportedAndRetried(t *testing.T) {
	var attempts atomic.Int64
	a := newTestAuto(AutoConfig{Sync: func(context.Context, func(string)) (string, error) { return "", nil }})
	a.SetOnTrained(func() error {
		if attempts.Add(1) == 1 {
			return errors.New("bundle is not loadable")
		}
		return nil
	})

	a.mu.Lock()
	a.reloadPending = true
	a.mu.Unlock()

	a.applyTrained()
	if got := a.Status().Error; got == "" {
		t.Error("a failed reload was not reported to the user")
	}
	a.applyTrained() // still pending, so it is tried again
	if attempts.Load() != 2 {
		t.Errorf("apply attempts = %d, want the failed one retried", attempts.Load())
	}
	a.mu.Lock()
	pending := a.reloadPending
	a.mu.Unlock()
	if pending {
		t.Error("reload still pending after it succeeded")
	}
}

// A pass cancelled by Yield must not be booked as a finished one — for training
// that would apply a bundle the interrupted run never wrote.
func TestCancelledTrainIsNotBookedAsDone(t *testing.T) {
	var applied atomic.Int64
	started := make(chan struct{}, 1)
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) { return "synced", nil },
		Train: func(ctx context.Context, _ func(string)) (string, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return "", ctx.Err()
		},
		TrainDue: func() (bool, string) { return true, "due" },
	})
	a.SetOnTrained(func() error { applied.Add(1); return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	<-started
	release := a.Yield()
	defer release()

	if applied.Load() != 0 {
		t.Error("an interrupted training run applied a bundle it never produced")
	}
	a.mu.Lock()
	pending := a.reloadPending
	a.mu.Unlock()
	if pending {
		t.Error("an interrupted training run left a reload pending")
	}
}

// A pass must not start once a manual operation has claimed priority, even if the
// yield lands between the turn check and the operation starting.
func TestNoPassStartsOnceYieldIsHeld(t *testing.T) {
	var passes atomic.Int64
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) {
			passes.Add(1)
			return "synced", nil
		},
	})
	release := a.Yield() // held before the worker ever starts
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	if n := passes.Load(); n != 0 {
		t.Errorf("%d pass(es) ran while a manual operation held priority", n)
	}
}

// The status must keep saying "paused" while a manual operation holds priority,
// instead of reporting an idle worker that is not actually free.
func TestPhaseStaysPausedWhileYielded(t *testing.T) {
	a := newTestAuto(AutoConfig{
		Sync: func(context.Context, func(string)) (string, error) { return "synced", nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	release := a.Yield()
	defer release()
	time.Sleep(50 * time.Millisecond)
	if got := a.Status().Phase; got != AutoPaused {
		t.Errorf("phase = %q while yielded, want %q", got, AutoPaused)
	}
}

// Releasing twice must not corrupt the yield count (a deferred release plus an
// explicit one is an easy mistake to make at a call site).
func TestReleaseIsIdempotent(t *testing.T) {
	a := newTestAuto(AutoConfig{Sync: func(context.Context, func(string)) (string, error) { return "", nil }})
	release := a.Yield()
	release()
	release()

	a.mu.Lock()
	got := a.yields
	a.mu.Unlock()
	if got != 0 {
		t.Errorf("yields = %d after a double release, want 0", got)
	}
}

// The status is read from the UI goroutine while the worker writes it.
func TestStatusIsRaceFree(t *testing.T) {
	a := newTestAuto(AutoConfig{
		Sync: func(_ context.Context, progress func(string)) (string, error) {
			for i := 0; i < 50; i++ {
				progress("line")
			}
			return "synced", nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = a.Status()
				a.TouchDraft()
			}
		}()
	}
	wg.Wait()
}
