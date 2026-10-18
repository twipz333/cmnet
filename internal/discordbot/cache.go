package discordbot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"cmnet/internal/atomicfile"
)

// AudioCache stores NTTS results on disk, keyed by synthesis parameters.
type AudioCache struct {
	mu  sync.Mutex
	dir string
}

// CacheEntry is one cached voiceover for the Alerts UI.
type CacheEntry struct {
	Key     string `json:"key"`
	Text    string `json:"text"`
	Speaker string `json:"speaker"`
	Ext     string `json:"ext"`
	Effect  string `json:"effect"`
	Bytes   int64  `json:"bytes"`
	ModTime int64  `json:"modTime"` // unix millis
	HasMeta bool   `json:"hasMeta"`
}

type cacheMeta struct {
	Text    string `json:"text"`
	Speaker string `json:"speaker"`
	Ext     string `json:"ext"`
	Effect  string `json:"effect"`
}

func NewAudioCache(dir string) (*AudioCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &AudioCache{dir: dir}, nil
}

// Dir returns the cache directory path.
func (c *AudioCache) Dir() string { return c.dir }

func cacheKey(speaker, text, ext, effect string) string {
	h := sha256.Sum256([]byte(speaker + "\x00" + ext + "\x00" + effect + "\x00" + text))
	return hex.EncodeToString(h[:])
}

// Path returns the on-disk path for a key (may not exist yet).
func (c *AudioCache) Path(speaker, text, ext, effect string) string {
	if ext == "" {
		ext = "ogg"
	}
	return filepath.Join(c.dir, cacheKey(speaker, text, ext, effect)+"."+ext)
}

func (c *AudioCache) metaPath(audioPath string) string {
	return audioPath + ".meta.json"
}

// Get returns the path if a cached file exists.
func (c *AudioCache) Get(speaker, text, ext, effect string) (string, bool) {
	p := c.Path(speaker, text, ext, effect)
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		return "", false
	}
	return p, true
}

// Put writes data atomically into the cache slot and a text sidecar for listing.
func (c *AudioCache) Put(speaker, text, ext, effect string, data []byte) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ext == "" {
		ext = "ogg"
	}
	p := c.Path(speaker, text, ext, effect)
	if err := atomicfile.Write(p, data, 0o644); err != nil {
		return "", err
	}
	meta, _ := json.Marshal(cacheMeta{Text: text, Speaker: speaker, Ext: ext, Effect: effect})
	_ = atomicfile.Write(c.metaPath(p), meta, 0o644)
	return p, nil
}

// Clear removes all cached audio files (used when voice/templates change).
func (c *AudioCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var first error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, e.Name())); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Delete removes one cache entry by hex key (with or without extension).
func (c *AudioCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty cache key")
	}
	base := key
	if i := strings.IndexByte(key, '.'); i > 0 {
		base = key[:i]
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cache entry not found")
		}
		return err
	}
	removed := false
	for _, e := range entries {
		name := e.Name()
		if name == base || strings.HasPrefix(name, base+".") {
			_ = os.Remove(filepath.Join(c.dir, name))
			removed = true
		}
	}
	if !removed {
		return fmt.Errorf("cache entry not found")
	}
	return nil
}

// List returns cache entries newest-first. Orphans without meta still appear (empty text).
func (c *AudioCache) List() ([]CacheEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]CacheEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".meta.json") {
			continue
		}
		full := filepath.Join(c.dir, name)
		st, err := e.Info()
		if err != nil || st.Size() == 0 {
			continue
		}
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		key := strings.TrimSuffix(name, filepath.Ext(name))
		ce := CacheEntry{
			Key:     key,
			Ext:     ext,
			Bytes:   st.Size(),
			ModTime: st.ModTime().UnixMilli(),
		}
		if data, err := os.ReadFile(c.metaPath(full)); err == nil {
			var m cacheMeta
			if json.Unmarshal(data, &m) == nil {
				ce.Text = m.Text
				ce.Speaker = m.Speaker
				ce.Effect = m.Effect
				if m.Ext != "" {
					ce.Ext = m.Ext
				}
				ce.HasMeta = true
			}
		}
		out = append(out, ce)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModTime != out[j].ModTime {
			return out[i].ModTime > out[j].ModTime
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// Resolve returns a local path, fetching via synth on cache miss.
func (c *AudioCache) Resolve(n *NTTSConfig, client *NTTSClient, text string) (string, error) {
	if text == "" {
		return "", fmt.Errorf("empty tts text")
	}
	if p, ok := c.Get(n.Speaker, text, n.Ext, n.Effect); ok {
		return p, nil
	}
	data, err := client.Synthesize(n.Speaker, text, n.Ext, n.Effect)
	if err != nil {
		return "", err
	}
	return c.Put(n.Speaker, text, n.Ext, n.Effect, data)
}

// ListCacheDir lists entries under dir.
func ListCacheDir(dir string) ([]CacheEntry, error) {
	if dir == "" {
		return nil, nil
	}
	return (&AudioCache{dir: dir}).List()
}

// DeleteCacheKey deletes one entry under dir.
func DeleteCacheKey(dir, key string) error {
	return (&AudioCache{dir: dir}).Delete(key)
}

// ClearCacheDir wipes dir.
func ClearCacheDir(dir string) error {
	return (&AudioCache{dir: dir}).Clear()
}
