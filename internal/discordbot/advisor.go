package discordbot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AdvisorClient calls advisor /api/remote/* on localhost.
type AdvisorClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewAdvisorClient(baseURL, token string) *AdvisorClient {
	return &AdvisorClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *AdvisorClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Advisor-Token", c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", res.Status, truncate(string(data), 300))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// State is a partial advisor stateDTO we care about.
type State struct {
	Meta   StateMeta       `json:"meta"`
	Status string          `json:"status"`
	Ring   bool            `json:"ring"`
	Alerts []StateAlert    `json:"alerts"`
	Lineup json.RawMessage `json:"lineup"`
}

type StateMeta struct {
	OurTeam string `json:"ourTeam"`
}

type StateAlert struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	NextAt int    `json:"nextAt"`
	NextIn int    `json:"nextIn"`
	Due    bool   `json:"due"`
	TTS    string `json:"tts,omitempty"`
}

func (c *AdvisorClient) GetState(ctx context.Context) (*State, error) {
	var st State
	if err := c.doJSON(ctx, http.MethodGet, "/api/remote/state", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *AdvisorClient) Scout(ctx context.Context, players string) (*State, error) {
	var st State
	if err := c.doJSON(ctx, http.MethodPost, "/api/remote/scout", map[string]string{"players": players}, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *AdvisorClient) ScoutClear(ctx context.Context, ally bool) (*State, error) {
	var st State
	if err := c.doJSON(ctx, http.MethodPost, "/api/remote/scout-clear", map[string]bool{"ally": ally}, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *AdvisorClient) SetSide(ctx context.Context, team string) (*State, error) {
	var st State
	if err := c.doJSON(ctx, http.MethodPost, "/api/remote/side", map[string]string{"team": team}, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *AdvisorClient) Lineup(ctx context.Context, side string, position int, accountID, heroID int64, clear bool) (*State, error) {
	body := map[string]any{
		"side":     side,
		"position": position,
		"clear":    clear,
	}
	if accountID > 0 {
		body["accountId"] = accountID
	}
	if heroID > 0 {
		body["heroId"] = heroID
	}
	var st State
	if err := c.doJSON(ctx, http.MethodPost, "/api/remote/lineup", body, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Events calls onFrame for every SSE state from /api/remote/events until ctx done.
func (c *AdvisorClient) Events(ctx context.Context, onFrame func(*State)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/remote/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Advisor-Token", c.Token)
	req.Header.Set("Accept", "text/event-stream")
	// Long-lived: no overall timeout on this client call.
	httpClient := &http.Client{Timeout: 0}
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("events: %s: %s", res.Status, truncate(string(b), 200))
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "{}" {
			continue
		}
		var st State
		if err := json.Unmarshal([]byte(payload), &st); err != nil {
			continue
		}
		onFrame(&st)
	}
	return sc.Err()
}
