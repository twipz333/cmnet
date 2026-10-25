package scout

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestVanityName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://steamcommunity.com/id/m3g4byt3/", "m3g4byt3", true},
		{"https://steamcommunity.com/id/m3g4byt3", "m3g4byt3", true},
		{"https://steamcommunitx.com/id/FooBar/", "FooBar", true},
		{"https://steamcommunity.com/profiles/76561198083722517", "", false},
		{"https://www.opendota.com/players/123", "", false},
		{"m3g4byt3", "", false},
	}
	for _, c := range cases {
		got, ok := VanityName(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("VanityName(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

type stubPages struct {
	body []byte
	err  error
	url  string
}

func (s *stubPages) Get(_ context.Context, rawURL string) ([]byte, error) {
	s.url = rawURL
	return s.body, s.err
}

func TestResolveAccountIDVanityFromSteamPage(t *testing.T) {
	html := []byte(`g_rgProfileData = {"url":"https://steamcommunity.com/id/m3g4byt3/","steamid":"76561198130200936","personaname":"m3g4byt3"};`)
	pages := &stubPages{body: html}
	id, err := ResolveAccountID(context.Background(), pages, "https://steamcommunity.com/id/m3g4byt3/")
	if err != nil {
		t.Fatal(err)
	}
	if id != 169935208 {
		t.Fatalf("id = %d, want 169935208", id)
	}
	if !strings.Contains(pages.url, "steamcommunity.com/id/m3g4byt3") {
		t.Fatalf("fetched %q, want steamcommunity vanity URL", pages.url)
	}
}

func TestResolveAccountIDVanityFromTypoDomainStillHitsSteam(t *testing.T) {
	html := []byte(`"steamid":"76561198083722517"`)
	pages := &stubPages{body: html}
	id, err := ResolveAccountID(context.Background(), pages, "https://steamcommunitx.com/id/someone/")
	if err != nil || id != 123456789 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if !strings.Contains(pages.url, "steamcommunity.com/id/someone") {
		t.Fatalf("must resolve via steamcommunity, got %q", pages.url)
	}
}

func TestResolveAccountIDVanityMissingSteamID(t *testing.T) {
	_, err := ResolveAccountID(context.Background(), &stubPages{body: []byte("<html>nope</html>")},
		"https://steamcommunity.com/id/missing/")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAccountIDStillParsesDirect(t *testing.T) {
	id, err := ResolveAccountID(context.Background(), nil, "https://www.opendota.com/players/123456789")
	if err != nil || id != 123456789 {
		t.Fatalf("id=%d err=%v", id, err)
	}
}

func TestResolveAccountIDSteamFetchError(t *testing.T) {
	_, err := ResolveAccountID(context.Background(), &stubPages{err: fmt.Errorf("network down")},
		"https://steamcommunity.com/id/x/")
	if err == nil {
		t.Fatal("expected error")
	}
}
