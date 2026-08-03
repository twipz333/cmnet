package main

import (
	"context"
	"encoding/json"
	"fmt"

	"cmnet/internal/cache"
	"cmnet/internal/jobs"
	"cmnet/internal/session"
)

// Job kinds, named by the queue package because both front ends post them.
const (
	jobSync        = jobs.KindSync
	jobTrain       = jobs.KindTrain
	jobScout       = jobs.KindScout
	jobScoutAllies = jobs.KindScoutAllies
)

// runner owns the job queue and turns the app's operations into queued work.
//
// Every long operation goes through here, whichever front end asked for it. That
// is what makes the behaviour uniform: a sync started from the browser, from the
// terminal shell or by the background worker is the same row in the same queue,
// with the same log and the same resource exclusion — instead of three code paths
// that each decide separately what may run alongside what.
type runner struct {
	q  *jobs.Queue
	st *cache.Store
}

// newRunner opens the queue's own handle on the cache and registers the handlers.
//
// The handle is long-lived, unlike the per-operation cache.Open the operations
// themselves still do: the queue reads and writes job rows constantly, and opening
// a database for every log flush would be absurd.
func newRunner(h homeFlags) (*runner, error) {
	st, err := cache.Open(h.dbPath)
	if err != nil {
		return nil, fmt.Errorf("open cache for the job queue: %w", err)
	}
	q := jobs.New(st, jobs.Options{Workers: 3})

	// Resources are declared by what each operation WRITES, not what it reads.
	// Sync and train both read the cache; only sync advances the scan watermarks,
	// only train replaces the bundle, so the two may overlap. Scouting touches
	// neither and runs alongside both.
	q.Register(jobs.Spec{
		Kind:      jobSync,
		Resources: []jobs.Resource{jobs.ResSyncWatermarks, jobs.ResMatchMerge},
		Handler: func(ctx context.Context, j cache.Job, log jobs.Log) (string, error) {
			var p syncPayload
			if err := decodePayload(j.Payload, &p); err != nil {
				return "", err
			}
			return opSync(ctx, syncOpts{
				dbPath:       h.dbPath,
				windowDays:   h.windowDays,
				minTier:      h.minRankTier,
				maxTier:      h.maxRankTier,
				maxMatches:   pick(p.MaxMatches, h.maxMatches),
				cmMatches:    pick(p.CMMatches, h.cmMatches),
				publicCM:     pick(p.PublicCM, h.publicCM),
				bulkGameMode: h.bulkGameMode,
				token:        h.token,
			}, progressOf(log))
		},
		MaxAttempts: 1,
	})
	q.Register(jobs.Spec{
		Kind:      jobTrain,
		Resources: []jobs.Resource{jobs.ResBundle},
		Handler: func(ctx context.Context, j cache.Job, log jobs.Log) (string, error) {
			return opTrain(ctx, trainOpts{
				dbPath: h.dbPath, outPath: h.bundlePath, windowDays: h.windowDays,
				token: h.token, minTier: h.minRankTier, maxTier: h.maxRankTier,
			}, progressOf(log))
		},
		MaxAttempts: 1,
	})
	q.Register(jobs.Spec{
		Kind:      jobScout,
		Resources: []jobs.Resource{jobs.ResSnapshot},
		Handler: func(ctx context.Context, j cache.Job, log jobs.Log) (string, error) {
			var p playersPayload
			if err := decodePayload(j.Payload, &p); err != nil {
				return "", err
			}
			return opScout(ctx, p.Players,
				scoutOpts{outPath: h.snapshotPath, dbPath: h.dbPath, windowDays: 30}, progressOf(log))
		},
		MaxAttempts: 1,
	})
	q.Register(jobs.Spec{
		Kind:      jobScoutAllies,
		Resources: []jobs.Resource{jobs.ResSnapshot},
		Handler: func(ctx context.Context, j cache.Job, log jobs.Log) (string, error) {
			var p playersPayload
			if err := decodePayload(j.Payload, &p); err != nil {
				return "", err
			}
			return opScoutAllies(ctx, p.Players,
				scoutOpts{outPath: h.snapshotPath, dbPath: h.dbPath, windowDays: 30}, progressOf(log))
		},
		MaxAttempts: 1,
	})

	return &runner{q: q, st: st}, nil
}

// Queue exposes the queue for the front ends.
func (r *runner) Queue() *jobs.Queue { return r.q }

// Close releases the queue's database handle.
func (r *runner) Close() error { return r.st.Close() }

// Run drives the queue until ctx ends, trimming history on the way out.
func (r *runner) Run(ctx context.Context) {
	r.q.Run(ctx)
	// Bounded history: a machine that collects for months would otherwise keep every
	// line of every pass forever.
	_ = r.st.TrimJobs(200)
}

// syncPayload lets a caller override the per-pass caps. Empty fields fall back to
// the flags, so the browser's "Sync" and the background worker's smaller pass are
// the same handler with different numbers.
type syncPayload struct {
	MaxMatches int `json:"maxMatches,omitempty"`
	CMMatches  int `json:"cmMatches,omitempty"`
	PublicCM   int `json:"publicCM,omitempty"`
}

type playersPayload struct {
	Players []string `json:"players"`
}

func decodePayload(s string, v any) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

func encodePayload(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// pick returns override when it is set, otherwise fallback.
func pick(override, fallback int) int {
	if override > 0 {
		return override
	}
	return fallback
}

// progressOf adapts a job logger to the plain progress callback the operations
// were written against. Phases are not invented here: an operation that wants them
// can log through the queue directly later.
func progressOf(log jobs.Log) func(string) {
	return func(line string) { log(cache.LogInfo, "", line) }
}

// enqueueAndFollow is the shape ShellConfig expects: run the operation, stream its
// progress, return a summary. The work runs in the queue; this only watches it.
func (r *runner) enqueueAndFollow(kind string, priority int, payload string, dedupe bool) session.AutoOp {
	return func(ctx context.Context, progress func(string)) (string, error) {
		id, err := r.q.Enqueue(kind, priority, payload, dedupe)
		if err != nil {
			return "", err
		}
		return r.q.Follow(ctx, id, progress)
	}
}

// manualSync / manualTrain are what a user's button press runs.
//
// Deduped: pressing Sync twice while one is queued must not queue two. Priority
// preempts a background pass of the same kind, which is what the old Yield
// handshake did — except the queue reinstates the displaced work by itself.
func (r *runner) manualSync() session.AutoOp {
	return r.enqueueAndFollow(jobSync, jobs.PriorityManual, "", true)
}

func (r *runner) manualTrain() session.AutoOp {
	return r.enqueueAndFollow(jobTrain, jobs.PriorityManual, "", true)
}

// backgroundSync / backgroundTrain are what the unattended worker proposes. Same
// handlers, smaller caps, and they yield to anything the user starts.
func (r *runner) backgroundSync(o autoOpts, publicCM int) session.AutoOp {
	return r.enqueueAndFollow(jobSync, jobs.PriorityBackground, encodePayload(syncPayload{
		MaxMatches: o.maxMatches,
		CMMatches:  o.cmMatches,
		PublicCM:   publicCM,
	}), true)
}

func (r *runner) backgroundTrain() session.AutoOp {
	return r.enqueueAndFollow(jobTrain, jobs.PriorityBackground, "", true)
}

// scoutOp queues a scout for the given players. Not deduped: two scouts are two
// different player lists, and collapsing them would silently drop one.
func (r *runner) scoutOp(kind string) func(context.Context, []string, func(string)) (string, error) {
	return func(ctx context.Context, players []string, progress func(string)) (string, error) {
		payload := encodePayload(playersPayload{Players: players})
		id, err := r.q.Enqueue(kind, jobs.PriorityManual, payload, false)
		if err != nil {
			return "", err
		}
		return r.q.Follow(ctx, id, progress)
	}
}
