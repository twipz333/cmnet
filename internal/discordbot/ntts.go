package discordbot

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NTTSClient talks to https://ntts.fdev.team (Bearer auth).
type NTTSClient struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

func NewNTTSClient(baseURL, token string) *NTTSClient {
	return &NTTSClient{
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
	}
}

// Synthesize returns audio bytes for speaker+text (ext: ogg|wav).
func (c *NTTSClient) Synthesize(speaker, text, ext, effect string) ([]byte, error) {
	if c == nil || c.Token == "" {
		return nil, fmt.Errorf("ntts: not configured")
	}
	if ext == "" {
		ext = "ogg"
	}
	q := url.Values{}
	q.Set("speaker", speaker)
	q.Set("text", text)
	q.Set("ext", ext)
	if effect != "" {
		q.Set("effect", effect)
	}
	endpoint := c.BaseURL + "/api/v1/tts?" + q.Encode()

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 400 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		res, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if res.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("ntts: %s: %s", res.Status, truncate(string(body), 200))
			continue
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ntts: %s: %s", res.Status, truncate(string(body), 200))
		}
		if len(body) < 32 {
			return nil, fmt.Errorf("ntts: empty audio")
		}
		return body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ntts: exhausted retries")
	}
	return nil, lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}