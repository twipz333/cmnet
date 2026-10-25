package assets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s
}

// Names come from API responses, so a hostile one must not escape the asset
// directory and overwrite something else.
func TestSafeNameCannotEscape(t *testing.T) {
	s := newStore(t)
	for _, name := range []string{"../../config.yaml", "..\\..\\cmnet.db", "a/b", ""} {
		got := s.Path(KindItem, name)
		rel, err := filepath.Rel(s.Dir(), got)
		if err != nil {
			t.Fatalf("rel(%q): %v", name, err)
		}
		if strings.HasPrefix(rel, "..") {
			t.Errorf("name %q escaped the asset dir: %s", name, got)
		}
	}
}

// The portrait URL is derived from the internal name. A localized name is not a
// key, and guessing one from it would 404 once per hero.
func TestHeroCDNRequiresInternalName(t *testing.T) {
	url, err := heroCDN("npc_dota_hero_antimage")
	if err != nil {
		t.Fatalf("internal name rejected: %v", err)
	}
	if !strings.HasSuffix(url, "/antimage.png") {
		t.Errorf("url = %q, want it keyed by the short name", url)
	}
	if _, err := heroCDN("Anti-Mage"); err == nil {
		t.Error("a localized display name was accepted as a CDN key")
	}
}

// A download writes atomically and is skipped once cached: the file is served by
// our own handler while a fetch may be running, and a half-written PNG would be
// served broken and then cached as complete by the browser.
func TestFetchIsAtomicAndCachedOnce(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte("\x89PNG fake"))
	}))
	defer srv.Close()

	s := newStore(t)
	ctx := context.Background()
	if err := s.fetch(ctx, KindHero, "1.png", srv.URL); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !s.Has(KindHero, "1.png") {
		t.Fatal("asset was not stored")
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Join(s.Dir(), string(KindHero)))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}

	// FetchHero must not re-download what is already there.
	if err := s.FetchHero(ctx, 1, "npc_dota_hero_antimage"); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if hits != 1 {
		t.Errorf("server hit %d times, want 1: a cached asset was re-downloaded", hits)
	}
}

// A 404 must be remembered for a while. Without it one bad key is re-requested on
// every page load, turning a single missing portrait into a steady stream.
func TestMissesAreRemembered(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := newStore(t)
	ctx := context.Background()
	if err := s.fetch(ctx, KindItem, "x.png", srv.URL); err == nil {
		t.Fatal("a 404 was reported as success")
	}
	if s.recentlyMissed(KindItem, "x.png") != true {
		t.Fatal("the miss was not recorded")
	}
	if err := s.FetchItem(ctx, "x"); err != nil {
		t.Fatalf("second attempt errored instead of being skipped: %v", err)
	}
	if hits != 1 {
		t.Errorf("server hit %d times, want 1: the miss was not respected", hits)
	}
}

// The handler serves the cache read-only and refuses to walk out of it.
func TestHandlerServesAndConfines(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.Path(KindHero, "5.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := s.Handler("/assets/")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/heroes/5.png", nil))
	if w.Code != http.StatusOK || w.Body.String() != "png" {
		t.Fatalf("status %d body %q, want the cached file", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want the browser told these never change", cc)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/../../cmnet.db", nil))
	if w.Code == http.StatusOK {
		t.Error("a traversal attempt was served")
	}
}
