package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"cmnet/internal/sources/stratz"
)

// cacheSchema bumps when the cached payload shape changes, invalidating old files.
const cacheSchema = "v2"

// fetchResult is the cached output of the expensive STRATZ work: the resolved
// anchor, the joint matches, and player profiles. Hero names are NOT cached here
// (they come from the local SQLite cache for free).
type fetchResult struct {
	FetchedAt time.Time                `json:"fetched_at"`
	Anchor    int64                    `json:"anchor"`
	Mode      string                   `json:"mode"`
	Friends   []int64                  `json:"friends"`
	Matches   []stratz.JointMatch      `json:"matches"`
	Profiles  map[int64]stratz.Profile `json:"profiles"`
}

// cacheKey is a stable hash of the inputs that determine the STRATZ pull: the
// (order-independent) focus ids and the explicit anchor argument. Output paths,
// formats and the token are deliberately excluded so they don't bust the cache.
func cacheKey(focus []int64, anchorArg string) string {
	ids := append([]int64(nil), focus...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	h := sha256.New()
	fmt.Fprintf(h, "%s|anchor=%s|", cacheSchema, anchorArg)
	for _, id := range ids {
		fmt.Fprintf(h, "%d,", id)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// fetchOrCache returns the joint-match pull, loading a fresh-enough cache file
// when allowed, otherwise hitting STRATZ and writing the cache.
func fetchOrCache(ctx context.Context, o pairstatsOpts, focus []int64, log func(string)) (*fetchResult, error) {
	key := cacheKey(focus, o.anchor)
	path := filepath.Join(o.cacheDir, "pair-"+key+".json")

	if !o.noCache {
		if fr, age, ok := loadCache(path, o.cacheTTL); ok {
			log(fmt.Sprintf("using cached pull %s (age %s, %d matches) — 0 API calls. --no-cache to refresh.",
				path, age.Round(time.Minute), len(fr.Matches)))
			return fr, nil
		}
	}

	client := stratz.New(o.token)

	// Resolve the anchor (explicit --anchor, else the first public focus id).
	anchor, friends, mode, err := resolveAnchor(ctx, client, o.anchor, focus, log)
	if err != nil {
		return nil, err
	}

	log(fmt.Sprintf("fetching joint matches (anchor=%d, friends=%v) …", anchor, friends))
	matches, err := client.JointMatches(ctx, anchor, friends, stratz.JointFilter{MaxPages: o.maxPages})
	if err != nil {
		return nil, fmt.Errorf("fetch joint matches: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no joint matches found (anchor=%d). If both profiles are private, "+
			"pass --anchor with an open teammate's id", anchor)
	}

	profiles := map[int64]stratz.Profile{}
	for _, id := range focus {
		if p, err := client.PlayerProfile(ctx, id); err == nil {
			profiles[id] = p
		}
	}

	fr := &fetchResult{
		FetchedAt: time.Now().UTC(),
		Anchor:    anchor,
		Mode:      mode,
		Friends:   friends,
		Matches:   matches,
		Profiles:  profiles,
	}
	if err := saveCache(path, fr); err != nil {
		log("warning: could not write cache: " + err.Error())
	} else {
		log("cached pull → " + path)
	}
	return fr, nil
}

// filterModes drops Turbo and/or Captains Mode matches from a pull, returning
// the kept matches and how many were removed. STRATZ reports gameMode as an
// enum string ("TURBO", "CAPTAINS_MODE"); Battle Cup drafts are Captains Mode.
func filterModes(matches []stratz.JointMatch, noTurbo, noCM bool) ([]stratz.JointMatch, int) {
	if !noTurbo && !noCM {
		return matches, 0
	}
	kept := make([]stratz.JointMatch, 0, len(matches))
	for _, m := range matches {
		if noTurbo && m.GameMode == "TURBO" {
			continue
		}
		if noCM && m.GameMode == "CAPTAINS_MODE" {
			continue
		}
		kept = append(kept, m)
	}
	return kept, len(matches) - len(kept)
}

// loadCache reads a cache file and returns it when younger than ttl.
func loadCache(path string, ttl time.Duration) (*fetchResult, time.Duration, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false
	}
	var fr fetchResult
	if err := json.Unmarshal(data, &fr); err != nil {
		return nil, 0, false
	}
	age := time.Since(fr.FetchedAt)
	if age > ttl {
		return nil, age, false
	}
	return &fr, age, true
}

// saveCache writes fr to path, creating the cache directory if needed.
func saveCache(path string, fr *fetchResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
