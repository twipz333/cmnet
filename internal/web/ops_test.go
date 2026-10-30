package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"cmnet/internal/cache"
	"cmnet/internal/jobs"
	"cmnet/internal/session"
)

// newQueueServer builds a server whose operations run through a real queue over a
// throwaway cache, plus a handler that blocks until released.
func newQueueServer(t *testing.T) (*Server, *jobs.Queue, chan struct{}) {
	t.Helper()
	st, err := cache.Open(filepath.Join(t.TempDir(), "cmnet.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	q := jobs.New(st, jobs.Options{Workers: 2})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	handler := func(ctx context.Context, j cache.Job, log jobs.Log) (string, error) {
		log(cache.LogInfo, "collect", "working")
		select {
		case <-release:
			return "collected 3 drafts", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	q.Register(jobs.Spec{Kind: jobs.KindSync, Resources: []jobs.Resource{jobs.ResSyncWatermarks}, Handler: handler})

	// Unblock handlers, then cancel and wait for Run before store Close:
	// otherwise workers still hold the SQLite file when TempDir RemoveAll runs.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx)
	}()
	t.Cleanup(func() {
		unblock()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("queue Run did not exit after cancel")
		}
	})

	s := New(session.ShellConfig{Jobs: q}, "127.0.0.1:0")
	return s, q, release
}

func waitRunning(t *testing.T, q *jobs.Queue, d time.Duration) cache.Job {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, j := range q.Active() {
			if j.State == cache.JobRunning {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no job started in time")
	return cache.Job{}
}

// The frame must say how long a job has been running and how long it has been
// quiet: a browser that connects mid-run has no idea when it began, and without
// the quiet timer a slow phase and a dead connection look identical.
func TestJobsViewReportsLiveness(t *testing.T) {
	s, q, _ := newQueueServer(t)

	if _, err := s.startSync(nil); err != nil {
		t.Fatalf("start sync: %v", err)
	}
	waitRunning(t, q, 3*time.Second)
	time.Sleep(30 * time.Millisecond)

	view := s.jobsView()
	if view.Running != jobs.KindSync {
		t.Fatalf("Running = %q, want %q", view.Running, jobs.KindSync)
	}
	if len(view.Active) != 1 {
		t.Fatalf("Active has %d jobs, want 1", len(view.Active))
	}
	got := view.Active[0]
	if got.RunningMs == 0 {
		t.Error("RunningMs is zero, so the page cannot say how long this has been going")
	}
	if got.LastLine != "working" {
		t.Errorf("LastLine = %q, want the handler's line", got.LastLine)
	}
	if got.Seq == 0 {
		t.Error("Seq is zero, so the page cannot tell a quiet job from a dead stream")
	}
}

// The old shell rejected a second operation outright. The queue accepts it: that
// is the point of the wave — collection keeps running while the user does
// something else, and incompatible work is separated by resource, not refused.
func TestSecondRequestIsQueuedNotRejected(t *testing.T) {
	s, q, _ := newQueueServer(t)

	if _, err := s.startSync(nil); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	waitRunning(t, q, 3*time.Second)

	if _, err := s.startSync(nil); err != nil {
		t.Fatalf("second sync was rejected instead of queued: %v", err)
	}
}

// A job's log is paged from the database, so its history survives the page being
// reloaded — the old in-memory []string did not.
func TestJobLogEndpointPages(t *testing.T) {
	s, q, _ := newQueueServer(t)

	if _, err := s.startSync(nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitRunning(t, q, 3*time.Second)

	// Wait for the first line to be flushed to storage.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lines, err := q.Log(job.ID, 0, 10); err == nil && len(lines) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	req := httptest.NewRequest("GET", "/api/jobs/log?id="+strconv.FormatInt(job.ID, 10)+"&after=0", nil)
	w := httptest.NewRecorder()
	s.handleJobLog(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var got struct {
		Job   *jobDTO      `json:"job"`
		Lines []logLineDTO `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Lines) == 0 {
		t.Fatal("no log lines returned")
	}
	if got.Lines[0].Phase != "collect" {
		t.Errorf("phase = %q, want the structured phase to survive to the browser", got.Lines[0].Phase)
	}
	if got.Lines[0].Level != cache.LogInfo {
		t.Errorf("level = %q, want %q", got.Lines[0].Level, cache.LogInfo)
	}
	if got.Job == nil || got.Job.State != string(cache.JobRunning) {
		t.Errorf("job = %+v, want the running job alongside its lines", got.Job)
	}
}

// Without a queue the endpoints must say so rather than panic: some builds and
// tests wire the operations directly.
func TestOpsWithoutQueue(t *testing.T) {
	s := New(session.ShellConfig{}, "127.0.0.1:0")
	if _, err := s.startSync(nil); err == nil {
		t.Error("starting a job without a queue reported success")
	}
	if view := s.jobsView(); view.Running != "" || len(view.Active) != 0 {
		t.Errorf("jobsView on a queueless server = %+v, want empty", view)
	}
}
