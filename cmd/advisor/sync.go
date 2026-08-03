package main

import (
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var o syncOpts
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Incrementally fetch CM matches into the local cache",
		Long: "Discovers Captains Mode match IDs at the configured rank tier from OpenDota's " +
			"public_matches (via the Explorer endpoint), then fetches pick/ban detail per match " +
			"from /matches/{id} into the local SQLite cache. Already-cached matches are skipped. " +
			"No STRATZ token is needed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, o)
		},
	}
	cmd.Flags().StringVar(&o.dbPath, "db", "cmnet.db", "path to the local SQLite cache")
	cmd.Flags().IntVar(&o.windowDays, "window-days", 31, "recency window in days (T-N)")
	cmd.Flags().IntVar(&o.minTier, "min-rank-tier", 40, "min avg_rank_tier (40=Archon,50=Legend,60=Ancient,70=Divine,80=Immortal)")
	cmd.Flags().IntVar(&o.maxTier, "max-rank-tier", 85, "max avg_rank_tier")
	cmd.Flags().IntVar(&o.maxMatches, "max-matches", 2500, "cap on new matches fetched per run (0 = unlimited / whole window)")
	cmd.Flags().IntVar(&o.publicCM, "public-cm", 0, "cap on PUBLIC Captains Mode drafts pulled from STRATZ in pages of 100 (0 = skip). This is the population the advisor is used in, unlike pro CM; needs a token")
	cmd.Flags().IntVar(&o.cmMatches, "cm-matches", 500, "cap on real CM drafts (with bans, from /proMatches) fetched per run for the next-pick predictor (0 = skip)")
	cmd.Flags().IntVar(&o.bulkGameMode, "bulk-game-mode", 22, "Dota game_mode for the bulk picks-only pool feeding synergy/counter+win-prob (22=Ranked All Pick, 2=Captains Mode, 0=all modes)")
	cmd.Flags().StringVar(&o.token, "token", envDefault("STRATZ_TOKEN", ""), "STRATZ API token: fetches CM draft detail from STRATZ (small, reliable payload) instead of OpenDota's heavy /matches/{id}. Empty = OpenDota fallback")
	return cmd
}

type syncOpts struct {
	dbPath       string
	windowDays   int
	minTier      int
	maxTier      int
	maxMatches   int
	cmMatches    int
	publicCM     int
	bulkGameMode int
	token        string
}

func runSync(cmd *cobra.Command, o syncOpts) error {
	summary, err := opSync(cmd.Context(), o, func(s string) { cmd.Println(s) })
	if err != nil {
		return err
	}
	cmd.Println(summary)
	return nil
}
