package main

import (
	"context"
	"fmt"
	"path/filepath"

	"cmnet/internal/assets"
	"cmnet/internal/cache"
)

// assetsDirFor puts the image cache next to the database.
//
// Next to the database rather than next to the binary: both are per-machine state
// that a user may relocate together, and a read-only install directory is a normal
// way to run a Go binary on Windows.
func assetsDirFor(dbPath string) string {
	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		return "assets"
	}
	return filepath.Join(dir, "assets")
}

// opFetchAssets downloads the portraits and avatars the UI can show.
//
// Best-effort throughout: an image that will not download is not an error the user
// needs to act on, and the UI falls back to the text chip it used before. What
// matters is that the page never reaches out to a CDN itself — after one pass it
// works with no network at all.
func opFetchAssets(ctx context.Context, dbPath string, log logFunc) (string, error) {
	store, err := cache.Open(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	as, err := assets.New(assetsDirFor(dbPath))
	if err != nil {
		return "", err
	}

	heroes, err := store.Heroes()
	if err != nil {
		return "", err
	}
	var fetched, skipped, failed int
	for i, h := range heroes {
		if err := ctx.Err(); err != nil {
			return fmt.Sprintf("assets: %d fetched, %d already cached, %d unavailable (stopped early)",
				fetched, skipped, failed), nil
		}
		if as.Has(assets.KindHero, assets.HeroFile(h.ID)) {
			skipped++
			continue
		}
		if err := as.FetchHero(ctx, h.ID, h.Name); err != nil {
			failed++
			continue
		}
		fetched++
		if (i+1)%25 == 0 {
			log(fmt.Sprintf("assets: %d/%d heroes", i+1, len(heroes)))
		}
	}

	log(fmt.Sprintf("assets: heroes %d cached, %d new, %d unavailable",
		as.Count(assets.KindHero), fetched, failed))
	return fmt.Sprintf("assets: %d new, %d already cached, %d unavailable", fetched, skipped, failed), nil
}
