package ratelimit

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// Client is an HTTP client that respects a Limiter and retries transient errors
// (HTTP 429 and 5xx) with exponential backoff, honoring Retry-After when present.
type Client struct {
	HTTP       *http.Client
	Limiter    *Limiter
	MaxRetries int           // retry attempts on transient errors
	BaseDelay  time.Duration // base for exponential backoff
	// Source names the API for the activity registry ("STRATZ", "OpenDota").
	// Empty disables tracking, which is what tests and one-off clients want.
	Source string
}

// NewClient builds a Client with sane defaults around the given limiter.
func NewClient(lim *Limiter) *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		Limiter:    lim,
		MaxRetries: 5,
		BaseDelay:  500 * time.Millisecond,
	}
}

// Do waits on the limiter, sends the request, and retries on 429/5xx. The caller
// owns closing the returned body. The request body, if any, must be replayable;
// callers that need retries with a body should set req.GetBody.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if err := c.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
		// Reset the body for replay on retries.
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}

		// Tracked around the call itself, so "how long has this been outstanding"
		// is a real measurement rather than a guess made from the last log line.
		id := beginRequest(c.Source)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			endRequest(c.Source, id, 0, err)
			lastErr = err
			d := c.backoff(attempt, 0)
			noteRetry(c.Source, d, false)
			if waitErr := sleep(ctx, d); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			status := resp.StatusCode
			drainClose(resp)
			endRequest(c.Source, id, status, nil)
			lastErr = fmt.Errorf("http %d from %s", status, req.URL)
			if attempt == c.MaxRetries {
				break
			}
			d := c.backoff(attempt, retryAfter)
			noteRetry(c.Source, d, status == http.StatusTooManyRequests)
			if waitErr := sleep(ctx, d); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		endRequest(c.Source, id, resp.StatusCode, nil)
		return resp, nil
	}
	return nil, fmt.Errorf("request failed after %d retries: %w", c.MaxRetries, lastErr)
}

// maxBackoff caps any single wait so a large server Retry-After (e.g. STRATZ's
// hourly-cap reset, which can be minutes) can't silently freeze the client.
const maxBackoff = 20 * time.Second

// backoff returns the delay before the next attempt: max(exponential, Retry-After),
// clamped to maxBackoff.
func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	exp := time.Duration(float64(c.BaseDelay) * math.Pow(2, float64(attempt)))
	d := exp
	if retryAfter > d {
		d = retryAfter
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func drainClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
