package scout

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// VanityName extracts a Steam custom URL slug from steamcommunity.com/id/<name>
// (and steamcommunitx.com/id/<name> — we only take the slug; resolution always
// hits steamcommunity.com, never Dotabuff).
func VanityName(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if !strings.Contains(host, "steamcommunity.com") && !strings.Contains(host, "steamcommunitx.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || !strings.EqualFold(parts[0], "id") {
		return "", false
	}
	name := strings.TrimSpace(parts[1])
	if name == "" || vanityAllDigits(name) {
		return "", false
	}
	return name, true
}

func vanityAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// PageGetter fetches a URL body. Tests inject a stub; production uses Steam HTTP.
type PageGetter interface {
	Get(ctx context.Context, rawURL string) ([]byte, error)
}

var steamIDInProfile = regexp.MustCompile(`"steamid"\s*:\s*"(\d{17})"`)

// ResolveAccountID parses a direct id/URL, or resolves steamcommunity.com/id/<vanity>
// by reading the unique SteamID64 embedded in the Steam profile page.
// OpenDota persona search is intentionally NOT used — nicknames collide.
func ResolveAccountID(ctx context.Context, pages PageGetter, input string) (int64, error) {
	if id, err := ParseAccountID(input); err == nil {
		return id, nil
	}
	vanity, ok := VanityName(input)
	if !ok {
		return 0, fmt.Errorf("cannot parse %q as id, profile URL, or steamcommunity.com/id/…", input)
	}
	if pages == nil {
		pages = defaultSteamGetter{}
	}
	return resolveVanityViaSteam(ctx, pages, vanity)
}

func resolveVanityViaSteam(ctx context.Context, pages PageGetter, vanity string) (int64, error) {
	// Always canonical Steam host — unique /id/<vanity> mapping lives there.
	rawURL := "https://steamcommunity.com/id/" + url.PathEscape(vanity) + "/"
	body, err := pages.Get(ctx, rawURL)
	if err != nil {
		return 0, fmt.Errorf("steam vanity %q: %w", vanity, err)
	}
	m := steamIDInProfile.FindSubmatch(body)
	if m == nil {
		return 0, fmt.Errorf("steam vanity %q: page has no steamid (private, missing, or layout changed)", vanity)
	}
	return normalizeNumeric(mustParseInt(string(m[1])))
}

func mustParseInt(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

type defaultSteamGetter struct{}

func (defaultSteamGetter) Get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// Steam serves a lighter page without a browser UA sometimes; be polite.
	req.Header.Set("User-Agent", "cmnet-advisor/1.0 (+local draft tool)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("HTTP 404")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}
