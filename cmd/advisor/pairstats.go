package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/cache"
	"cmnet/internal/pairstats"
	"cmnet/internal/scout"
	"cmnet/internal/sources/stratz"
)

type pairstatsOpts struct {
	token     string
	anchor    string
	dbPath    string
	maxPages  int
	htmlPath  string
	jsonPath  string
	xlsxPath  string
	pngPrefix string
	outPrefix string
	noCache   bool
	cacheTTL  time.Duration
	cacheDir  string
	noTurbo   bool
	noCM      bool
}

func newPairStatsCmd() *cobra.Command {
	var o pairstatsOpts
	cmd := &cobra.Command{
		Use:   "pairstats <player1> <player2> [player3 ...]",
		Short: "Head-to-head stats across two+ players' joint (same-team) matches",
		Long: "Pulls every match where the given players were teammates (STRATZ server-side\n" +
			"withFriendSteamAccountIds filter — the whole set is one paginated query) and\n" +
			"aggregates win rate, IMP, GPM, KDA, lane-win@10 and per-hero splits.\n\n" +
			"Profile visibility:\n" +
			"  open/open, open/closed → anchor is auto-picked from a public profile.\n" +
			"  closed/closed          → pass --anchor <open id/url> of a common teammate.\n\n" +
			"Players accept raw account_id, SteamID3/64, or profile URLs. Powered by STRATZ.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairStats(cmd.Context(), o, args, func(s string) { cmd.Println(s) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.token, "token", envDefault("STRATZ_TOKEN", ""), "STRATZ API token (required)")
	f.StringVar(&o.anchor, "anchor", "", "open profile (id/url) that played with all targets; needed for closed/closed")
	f.StringVar(&o.dbPath, "db", "cmnet.db", "SQLite cache for hero names (optional; IDs shown if absent)")
	f.IntVar(&o.maxPages, "max-pages", 50, "pagination safety cap (100 matches/page)")
	f.StringVar(&o.htmlPath, "html", "", "write HTML report to this path")
	f.StringVar(&o.jsonPath, "json", "", "write JSON dump to this path")
	f.StringVar(&o.xlsxPath, "xlsx", "", "write Excel workbook to this path")
	f.StringVar(&o.pngPrefix, "png", "", "write PNG charts using this path prefix")
	f.StringVar(&o.outPrefix, "out", "", "convenience: write ALL formats using this prefix (e.g. pairstats)")
	f.BoolVar(&o.noCache, "no-cache", false, "ignore any cached pull and fetch fresh from STRATZ (still updates cache)")
	f.DurationVar(&o.cacheTTL, "cache-ttl", 12*time.Hour, "max age of a cached pull before refetching (e.g. 6h, 24h)")
	f.StringVar(&o.cacheDir, "cache-dir", "pairstats-cache", "directory holding cached STRATZ pulls")
	f.BoolVar(&o.noTurbo, "no-turbo", false, "exclude Turbo games (skewed economy/lane); filters the cached pull, no refetch")
	f.BoolVar(&o.noCM, "no-cm", false, "exclude Captains Mode / Battle Cup drafts; filters the cached pull, no refetch")
	return cmd
}

func runPairStats(ctx context.Context, o pairstatsOpts, args []string, log func(string)) error {
	if o.token == "" {
		return fmt.Errorf("STRATZ token required: pass --token or set STRATZ_TOKEN")
	}

	// Resolve focus account ids (raw id / SteamID / profile URL), preserving order.
	focus := make([]int64, 0, len(args))
	seen := map[int64]bool{}
	for _, a := range args {
		id, err := scout.ParseAccountID(a)
		if err != nil {
			return fmt.Errorf("player %q: %w", a, err)
		}
		if !seen[id] {
			seen[id] = true
			focus = append(focus, id)
		}
	}

	// The expensive part (anchor probing + joint-match pull + profiles) is cached
	// on disk so repeated runs on the same players spend no tokens until the cache
	// expires (default 12h — long enough to avoid churn, short enough that a fresh
	// match shows up the same day).
	fr, err := fetchOrCache(ctx, o, focus, log)
	if err != nil {
		return err
	}

	// Mode filtering is applied to the cached pull (never at fetch time), so the
	// cache stays mode-agnostic and toggling --no-turbo/--no-cm costs no tokens.
	matches, dropped := filterModes(fr.Matches, o.noTurbo, o.noCM)
	if dropped > 0 {
		log(fmt.Sprintf("filtered out %d match(es) by mode (--no-turbo=%t --no-cm=%t)", dropped, o.noTurbo, o.noCM))
	}
	if len(matches) == 0 {
		return fmt.Errorf("no matches left after mode filtering")
	}

	heroNames := loadHeroNames(o.dbPath)
	profiles := map[int64]pairstats.Profile{}
	for id, p := range fr.Profiles {
		profiles[id] = pairstats.Profile{Name: p.Name, Avatar: p.Avatar, ProfileURI: p.ProfileURI}
	}

	rep := pairstats.Build(matches, focus, fr.Anchor, fr.Mode, profiles, heroNames)

	// Always print the terminal summary.
	log("")
	log(rep.Text())

	// Honor --out as "write everything with this prefix".
	if o.outPrefix != "" {
		if o.htmlPath == "" {
			o.htmlPath = o.outPrefix + ".html"
		}
		if o.jsonPath == "" {
			o.jsonPath = o.outPrefix + ".json"
		}
		if o.xlsxPath == "" {
			o.xlsxPath = o.outPrefix + ".xlsx"
		}
		if o.pngPrefix == "" {
			o.pngPrefix = o.outPrefix
		}
	}

	if o.jsonPath != "" {
		if err := rep.WriteJSON(o.jsonPath); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		log("wrote " + o.jsonPath)
	}
	if o.htmlPath != "" {
		if err := rep.WriteHTML(o.htmlPath); err != nil {
			return fmt.Errorf("write html: %w", err)
		}
		log("wrote " + o.htmlPath)
	}
	if o.xlsxPath != "" {
		if err := rep.WriteXLSX(o.xlsxPath); err != nil {
			return fmt.Errorf("write xlsx: %w", err)
		}
		log("wrote " + o.xlsxPath)
	}
	if o.pngPrefix != "" {
		files, err := rep.WritePNG(o.pngPrefix)
		if err != nil {
			return fmt.Errorf("write png: %w", err)
		}
		for _, p := range files {
			log("wrote " + p)
		}
	}
	return nil
}

// resolveAnchor decides which public profile to enumerate matches from.
// Returns the anchor id, the friend filter (the other focus ids, or all focus
// ids when an external anchor is used), and a human-readable mode label.
func resolveAnchor(ctx context.Context, c *stratz.Client, anchorArg string, focus []int64, log func(string)) (int64, []int64, string, error) {
	if anchorArg != "" {
		a, err := scout.ParseAccountID(anchorArg)
		if err != nil {
			return 0, nil, "", fmt.Errorf("--anchor %q: %w", anchorArg, err)
		}
		return a, focus, fmt.Sprintf("anchored via %d (closed/closed supported)", a), nil
	}

	// Probe public visibility of each focus id (cheap, 1 call each).
	public := make([]bool, len(focus))
	npublic := 0
	for i, id := range focus {
		ok, err := c.IsPublic(ctx, id)
		if err != nil {
			return 0, nil, "", fmt.Errorf("probe %d: %w", id, err)
		}
		public[i] = ok
		if ok {
			npublic++
		}
	}
	anchorIdx := -1
	for i, ok := range public {
		if ok {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 {
		return 0, nil, "", fmt.Errorf("all %d profiles are private; pass --anchor with an open "+
			"teammate's id (one open common friend unlocks the whole team's matches)", len(focus))
	}

	anchor := focus[anchorIdx]
	friends := make([]int64, 0, len(focus)-1)
	for i, id := range focus {
		if i != anchorIdx {
			friends = append(friends, id)
		}
	}
	mode := "open/open"
	if npublic < len(focus) {
		mode = fmt.Sprintf("open/closed (%d/%d public)", npublic, len(focus))
	}
	log(fmt.Sprintf("anchor auto-selected: %d (%s)", anchor, mode))
	return anchor, friends, mode, nil
}

// loadHeroNames returns hero id → localized name from the cache, or nil on any
// error (the report then falls back to "hero#<id>").
func loadHeroNames(dbPath string) map[int]string {
	store, err := cache.Open(dbPath)
	if err != nil {
		return nil
	}
	defer store.Close()
	heroes, err := store.Heroes()
	if err != nil {
		return nil
	}
	m := make(map[int]string, len(heroes))
	for _, h := range heroes {
		m[h.ID] = h.Localized
	}
	return m
}
