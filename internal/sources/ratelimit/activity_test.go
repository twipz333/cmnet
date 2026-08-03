package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A request in flight must be visible while it is still open — that is the whole
// point. Before this, a job that had been silent for two minutes and a job whose
// source had hung looked identical from the UI.
func TestActivityReportsInFlight(t *testing.T) {
	ResetForTest()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(New(Window{N: 1000, Per: time.Second}))
	c.Source = "TestAPI"

	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait for the request to be registered as outstanding.
	deadline := time.Now().Add(3 * time.Second)
	var seen Activity
	for time.Now().Before(deadline) {
		for _, a := range Snapshot() {
			if a.Source == "TestAPI" && a.InFlight > 0 {
				seen = a
			}
		}
		if seen.InFlight > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if seen.InFlight != 1 {
		t.Fatalf("in-flight = %d, want 1 while the request is open", seen.InFlight)
	}
	if src, waited := Busiest(); src != "TestAPI" || waited < 0 {
		t.Errorf("Busiest = %q/%v, want the open request named", src, waited)
	}

	close(release)
	<-done

	after := activityFor(t, "TestAPI")
	if after.InFlight != 0 {
		t.Errorf("in-flight = %d after completion, want 0", after.InFlight)
	}
	if after.Requests != 1 {
		t.Errorf("requests = %d, want 1", after.Requests)
	}
	if after.LastStatus != http.StatusOK {
		t.Errorf("last status = %d, want 200", after.LastStatus)
	}
}

// A 429 is the case the panel exists for: the source answered, told us to wait,
// and the wait is not a hang.
func TestActivityCountsRateLimiting(t *testing.T) {
	ResetForTest()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(New(Window{N: 1000, Per: time.Second}))
	c.Source = "TestAPI"
	c.BaseDelay = time.Millisecond

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()

	a := activityFor(t, "TestAPI")
	if a.RateLimited != 1 {
		t.Errorf("rate-limited count = %d, want 1", a.RateLimited)
	}
	if a.Retries != 1 {
		t.Errorf("retries = %d, want 1", a.Retries)
	}
	if a.Requests != 2 {
		t.Errorf("requests = %d, want 2 (the retry is a request too)", a.Requests)
	}
}

// A source with nothing outstanding must not be named as the thing we are waiting
// on: "waiting on OpenDota" while it is idle is worse than saying nothing.
func TestBusiestIgnoresIdleSources(t *testing.T) {
	ResetForTest()
	if src, _ := Busiest(); src != "" {
		t.Errorf("Busiest = %q with nothing outstanding, want empty", src)
	}
}

func activityFor(t *testing.T, source string) Activity {
	t.Helper()
	for _, a := range Snapshot() {
		if a.Source == source {
			return a
		}
	}
	t.Fatalf("no activity recorded for %q", source)
	return Activity{}
}
