// Package assets keeps hero portraits, item icons and player avatars in a local
// directory and serves them from our own HTTP endpoint.
//
// On disk rather than embedded in the binary: the whole point of this project is
// one static Windows executable, and a hero set is several megabytes that grows
// with every patch. Baking them in would mean a new build every time Valve adds a
// hero, and a binary that carries images most users never look at.
//
// Fetched once and served locally rather than hot-linked: the page is opened from
// a local server and must keep working with no network at all — which is also the
// state a laptop is in on a Battle Cup night more often than anyone would like. A
// missing image is not an error anywhere; the UI falls back to the text chip it
// used before.
package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind is a family of asset, which is also its subdirectory.
type Kind string

const (
	KindHero   Kind = "heroes"
	KindItem   Kind = "items"
	KindAvatar Kind = "avatars"
)

// Store is the on-disk asset cache.
type Store struct {
	dir    string
	client *http.Client

	mu      sync.Mutex
	missing map[string]time.Time // key -> when a fetch last failed
}

// retryAfterMiss is how long a failed fetch is remembered.
//
// Without it a hero whose portrait 404s (a name we derived wrongly, a brand-new
// hero the CDN has not published yet) is re-requested on every single page load,
// which turns one bad key into a steady stream of pointless requests.
const retryAfterMiss = 6 * time.Hour

// fetchTimeout bounds one download. Images are decoration: a slow CDN must never
// hold up anything the user is waiting on.
const fetchTimeout = 15 * time.Second

// New opens (creating if needed) an asset store under dir.
func New(dir string) (*Store, error) {
	for _, k := range []Kind{KindHero, KindItem, KindAvatar} {
		if err := os.MkdirAll(filepath.Join(dir, string(k)), 0o755); err != nil {
			return nil, fmt.Errorf("create asset dir: %w", err)
		}
	}
	return &Store{
		dir:     dir,
		client:  &http.Client{Timeout: fetchTimeout},
		missing: map[string]time.Time{},
	}, nil
}

// Dir returns the root directory, which is what the HTTP handler serves.
func (s *Store) Dir() string { return s.dir }

// Path returns where an asset lives, whether or not it exists yet.
func (s *Store) Path(kind Kind, name string) string {
	return filepath.Join(s.dir, string(kind), safeName(name))
}

// Has reports whether an asset is already on disk.
func (s *Store) Has(kind Kind, name string) bool {
	st, err := os.Stat(s.Path(kind, name))
	return err == nil && st.Size() > 0
}

// safeName strips anything that could escape the asset directory. The names come
// from API responses, so they are not ours to trust: a hero named "../../config"
// must land in a file called that, not overwrite one.
func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "..", "_")
	name = strings.TrimSpace(name)
	if name == "" {
		return "_"
	}
	return name
}

// HeroFile is the filename a hero's portrait is stored under.
func HeroFile(heroID int) string { return strconv.Itoa(heroID) + ".png" }

// ItemFile is the filename an item icon is stored under.
func ItemFile(itemName string) string { return safeName(itemName) + ".png" }

// AvatarFile is the filename an account's avatar is stored under.
func AvatarFile(accountID int64) string { return strconv.FormatInt(accountID, 10) + ".jpg" }

// heroCDN builds the portrait URL from the internal hero name.
//
// Valve's own image host, keyed by the short name: "npc_dota_hero_antimage"
// becomes "antimage". The mapping is mechanical, which is why the hero id is not
// enough on its own — the CDN has no id-addressed path.
func heroCDN(heroName string) (string, error) {
	// The internal name is required, not the localized one: "Anti-Mage" is not a
	// key, and deriving one from it produces a 404 per hero and a store full of
	// nothing. The cache stores both, so the caller has the right one to hand.
	if !strings.HasPrefix(heroName, "npc_dota_hero_") {
		return "", errors.New("hero portrait needs the internal name (npc_dota_hero_*), got " + heroName)
	}
	short := strings.TrimPrefix(heroName, "npc_dota_hero_")
	if short == "" {
		return "", errors.New("empty hero name")
	}
	return "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/" +
		safeName(short) + ".png", nil
}

// itemCDN builds the icon URL from the internal item name.
func itemCDN(itemName string) string {
	short := strings.TrimPrefix(itemName, "item_")
	return "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/items/" +
		safeName(short) + ".png"
}

// FetchHero downloads one hero portrait if it is not already cached.
func (s *Store) FetchHero(ctx context.Context, heroID int, heroName string) error {
	file := HeroFile(heroID)
	if s.Has(KindHero, file) || s.recentlyMissed(KindHero, file) {
		return nil
	}
	url, err := heroCDN(heroName)
	if err != nil {
		return err
	}
	return s.fetch(ctx, KindHero, file, url)
}

// FetchItem downloads one item icon if it is not already cached.
func (s *Store) FetchItem(ctx context.Context, itemName string) error {
	file := ItemFile(itemName)
	if s.Has(KindItem, file) || s.recentlyMissed(KindItem, file) {
		return nil
	}
	return s.fetch(ctx, KindItem, file, itemCDN(itemName))
}

// FetchAvatar downloads a player's avatar from the URL a profile lookup returned.
//
// Unlike heroes and items there is no derivable URL: the avatar hash comes from
// the profile, which is why this takes one rather than building it.
func (s *Store) FetchAvatar(ctx context.Context, accountID int64, url string) error {
	if url == "" {
		return nil
	}
	file := AvatarFile(accountID)
	if s.Has(KindAvatar, file) || s.recentlyMissed(KindAvatar, file) {
		return nil
	}
	return s.fetch(ctx, KindAvatar, file, url)
}

// fetch downloads one asset and writes it atomically.
//
// Atomic because the file is served by our own HTTP handler while this runs: a
// half-written PNG would be served as a broken image and then cached as complete
// by the browser.
func (s *Store) fetch(ctx context.Context, kind Kind, file, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.noteMiss(kind, file)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.noteMiss(kind, file)
		return fmt.Errorf("%s: %s", url, resp.Status)
	}

	dst := s.Path(kind, file)
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded

	// Bounded: an asset host answering with something enormous must not fill the
	// disk of a machine that only asked for an icon.
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxAssetBytes)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

// maxAssetBytes caps one downloaded asset. Portraits are tens of kilobytes; this
// is generous by two orders of magnitude and still bounded.
const maxAssetBytes = 4 << 20

func (s *Store) recentlyMissed(kind Kind, file string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.missing[string(kind)+"/"+file]
	return ok && time.Since(at) < retryAfterMiss
}

func (s *Store) noteMiss(kind Kind, file string) {
	s.mu.Lock()
	s.missing[string(kind)+"/"+file] = time.Now()
	s.mu.Unlock()
}

// Handler serves the cached assets read-only.
//
// http.Dir refuses to follow paths that escape the root, which together with
// safeName on the way in is what keeps a hostile name in an API response from
// reaching anything outside this directory.
func (s *Store) Handler(prefix string) http.Handler {
	fs := http.FileServer(http.Dir(s.dir))
	return http.StripPrefix(prefix, cacheForever(fs))
}

// cacheForever tells the browser these bytes never change. They are content for a
// specific hero or item, and a re-download would only re-fetch the same image.
func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		h.ServeHTTP(w, r)
	})
}

// Count reports how many assets of a kind are cached, for the Data screen.
func (s *Store) Count(kind Kind) int {
	entries, err := os.ReadDir(filepath.Join(s.dir, string(kind)))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".tmp-") {
			n++
		}
	}
	return n
}
