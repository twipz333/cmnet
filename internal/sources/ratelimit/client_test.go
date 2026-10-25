package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fastClient builds a Client with no real delay so retry logic can be exercised quickly.
func fastClient() *Client {
	c := NewClient(New(Window{N: 1000, Per: time.Second}))
	c.BaseDelay = time.Millisecond
	return c
}

func TestDoRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := fastClient().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3 (two 429s then success)", got)
	}
}

func TestDoGivesUpAfterMaxRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := fastClient()
	c.MaxRetries = 2
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if _, err := c.Do(req); err == nil {
		t.Error("expected error after exhausting retries")
	}
}

func TestLimiterWaitRespectsContext(t *testing.T) {
	// A tiny window so the second Wait would block; cancel makes it return early.
	l := New(Window{N: 1, Per: time.Hour})
	ctx := context.Background()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := l.Wait(cctx); err == nil {
		t.Error("expected context error on canceled wait")
	}
}
