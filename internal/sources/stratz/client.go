// Package stratz is a minimal GraphQL client for the STRATZ API. STRATZ supplies
// the filtered Captains Mode match IDs (by average MMR bracket and recency); the
// per-match draft detail is fetched separately from OpenDota.
//
// STRATZ usage requires a logged-in token and attribution ("Powered by STRATZ").
package stratz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"cmnet/internal/sources/ratelimit"
)

const endpoint = "https://api.stratz.com/graphql"

// Client executes GraphQL queries against STRATZ with rate limiting and the
// required auth/user-agent headers.
type Client struct {
	endpoint string
	token    string
	http     *ratelimit.Client
}

// New returns a STRATZ client authenticated with token.
func New(token string) *Client {
	return &Client{
		endpoint: endpoint,
		token:    token,
		http:     stratzHTTPClient(),
	}
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

// query executes a GraphQL query and unmarshals the "data" field into out.
func (c *Client) query(ctx context.Context, q string, vars map[string]any, out any) error {
	payload, err := json.Marshal(graphQLRequest{Query: q, Variables: vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	// STRATZ requires this exact User-Agent for token-authenticated API access.
	req.Header.Set("User-Agent", "STRATZ_API")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stratz graphql: http %d: %s", resp.StatusCode, truncate(body))
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("stratz decode envelope: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("stratz graphql error: %s", envelope.Errors[0].Message)
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("stratz decode data: %w", err)
		}
	}
	return nil
}

func truncate(b []byte) string {
	const max = 256
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

// stratzHTTPClient builds the rate-limited client, named for the activity
// registry so the UI can report which API is slow instead of "something".
func stratzHTTPClient() *ratelimit.Client {
	c := ratelimit.NewClient(ratelimit.StratzLimiter())
	c.Source = "STRATZ"
	return c
}
