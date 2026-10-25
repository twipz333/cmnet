// Package ratelimit provides per-source token-bucket limiting and an HTTP client
// that waits on the bucket and retries on HTTP 429 / 5xx with exponential backoff.
//
// Each data source has several rate windows at once (e.g. STRATZ allows 20/s,
// 250/min, 2000/hour, 10000/day). A Limiter enforces all of them simultaneously.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Window is a single rate constraint: at most N events per Per duration.
type Window struct {
	N   int
	Per time.Duration
}

// Limiter enforces several Windows at once. Wait blocks until every window admits
// one event.
type Limiter struct {
	limiters []*rate.Limiter
}

// New builds a Limiter from one or more windows.
func New(windows ...Window) *Limiter {
	ls := make([]*rate.Limiter, 0, len(windows))
	for _, w := range windows {
		if w.N <= 0 || w.Per <= 0 {
			continue
		}
		perSec := float64(w.N) / w.Per.Seconds()
		ls = append(ls, rate.NewLimiter(rate.Limit(perSec), w.N))
	}
	return &Limiter{limiters: ls}
}

// Wait blocks until all windows admit one event, or ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	for _, lim := range l.limiters {
		if err := lim.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// The limiters below are per-process singletons, and that is the whole point:
// the rate limit belongs to the API key / IP, not to whichever client object
// happens to hold it. Handing each client its own bucket was harmless only while
// the app ran exactly one operation at a time. It no longer does — the background
// worker syncs while a scout runs — and two independent buckets would let the app
// send twice the documented rate, at which point STRATZ answers 429 to BOTH: the
// background collection AND the scout the user is waiting on minutes before a
// match. One bucket makes them queue behind each other instead.

// OpenDotaLimiter reflects the documented OpenDota free-tier limit (60 req/min).
// The monthly cap (50k) is respected by the aggressive local cache, not enforced
// here.
var OpenDotaLimiter = sync.OnceValue(func() *Limiter {
	return New(Window{N: 60, Per: time.Minute})
})

// StratzLimiter reflects the documented STRATZ authenticated limits.
var StratzLimiter = sync.OnceValue(func() *Limiter {
	return New(
		Window{N: 20, Per: time.Second},
		Window{N: 250, Per: time.Minute},
		Window{N: 2000, Per: time.Hour},
		Window{N: 10000, Per: 24 * time.Hour},
	)
})
