// Package opendota is a thin REST client for the OpenDota API: hero reference
// data and detailed match pick/ban. STRATZ supplies match IDs; OpenDota supplies
// the per-match draft detail.
package opendota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"cmnet/internal/sources/ratelimit"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

const baseURL = "https://api.opendota.com/api"

// Client talks to OpenDota through a rate-limited HTTP client.
type Client struct {
	base string
	http *ratelimit.Client
}

// New returns a Client using the documented OpenDota rate limit.
func New() *Client {
	rc := ratelimit.NewClient(ratelimit.OpenDotaLimiter())
	// Named so the UI can say which API it is waiting on rather than "something".
	rc.Source = "OpenDota"
	// Hard cap for the heaviest endpoint, /matches/{id}: full match detail (with
	// picks_bans, players, etc.) is a large payload that can legitimately take tens
	// of seconds. The Explorer endpoint, which instead suffers from connection
	// stalls, imposes its own much shorter per-call deadline via context (see
	// explorerRows) so a stall still fails fast and is retried.
	rc.HTTP.Timeout = 40 * time.Second
	// Disable HTTP keep-alive. When an Explorer call is abandoned on its short ctx
	// deadline (the endpoint stalls), the underlying connection is left in a bad
	// state; if the pool reuses it for the next request the read fails with
	// "unexpected EOF". A fresh connection per request removes that whole failure
	// class at the cost of a TLS handshake — negligible at our request rate.
	rc.HTTP.Transport = &http.Transport{DisableKeepAlives: true}
	return &Client{base: baseURL, http: rc}
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	// Retry the whole request on read/decode failures too: OpenDota occasionally
	// terminates a response mid-body (the rate-limit client only retries transport
	// and 5xx/429 errors at the Do level, not a truncated body that fails to
	// decode). A few extra attempts make every OpenDota read resilient to that.
	const attempts = 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		err := c.getJSONOnce(ctx, path, out)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
	}
	return lastErr
}

func (c *Client) getJSONOnce(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opendota GET %s: http %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("opendota decode %s: %w", path, err)
	}
	return nil
}
