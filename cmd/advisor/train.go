package main

import (
	"github.com/spf13/cobra"

	"cmnet/internal/train"
)

func newTrainCmd() *cobra.Command {
	var o trainOpts
	cmd := &cobra.Command{
		Use:   "train",
		Short: "Train layer-1 models from the cache and emit a versioned bundle",
		Long: "Builds features from the local cache, trains the next-pick predictor (5.1) and " +
			"the win-probability model (5.2) deterministically on Go (recency-weighted, optional " +
			"patch/MMR filter, Platt-calibrated), and writes a self-contained versioned bundle.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrain(cmd, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dbPath, "db", "cmnet.db", "path to the local SQLite cache")
	f.StringVar(&o.outPath, "out", "bundle.json", "output bundle path")
	f.IntVar(&o.windowDays, "window-days", 31, "training recency window in days (T-N)")
	f.StringVar(&o.patchTag, "patch", "", "patch tag stamped into the bundle")
	f.StringVar(&o.mmrTier, "mmr-tier", "", "MMR tier label stamped into the bundle")
	f.Float64Var(&o.halfLifeDays, "half-life", 0, "recency half-life in days (0 = default, <0 = uniform)")
	f.StringVar(&o.filterPatch, "filter-patch", "", "train only on matches with this patch tag")
	f.IntVar(&o.minAvgMMR, "min-avg-mmr", 0, "train only on matches with avg MMR >= this")
	f.IntVar(&o.trainMinRankTier, "train-min-rank-tier", 0, "drop training matches below this avg rank tier (40=Archon … 80=Immortal); matches with unknown tier are kept")
	f.Float64Var(&o.proWeight, "pro-weight", train.DefaultProWeight, "training weight of PRO Captains Mode drafts relative to public games; they are real CM but a different population (1 = weigh them equally)")
	f.IntVar(&o.fmRank, "fm-rank", 0, "also train a factorization machine of this latent rank into the bundle (0 = skip, -1 = default rank)")
	f.BoolVar(&o.noPrefix, "no-prefix", false, "train win-prob on final compositions only, skipping partial-board rows")
	f.StringVar(&o.token, "token", envDefault("STRATZ_TOKEN", ""), "STRATZ API token; enables real per-position meta (#9). Falls back to OpenDota roles when empty")
	f.IntVar(&o.minTier, "min-rank-tier", 40, "min avg_rank_tier for the STRATZ position meta bracket (40=Archon … 80=Immortal)")
	f.IntVar(&o.maxTier, "max-rank-tier", 85, "max avg_rank_tier for the STRATZ position meta bracket")
	return cmd
}

type trainOpts struct {
	dbPath           string
	outPath          string
	windowDays       int
	patchTag         string
	mmrTier          string
	halfLifeDays     float64
	filterPatch      string
	minAvgMMR        int
	trainMinRankTier int
	proWeight        float64
	fmRank           int
	noPrefix         bool
	// STRATZ per-position meta (#9). When token is empty the bundle falls back to
	// coarse OpenDota role positions.
	token   string
	minTier int
	maxTier int
}

func runTrain(cmd *cobra.Command, o trainOpts) error {
	summary, err := opTrain(cmd.Context(), o, func(s string) { cmd.Println(s) })
	if err != nil {
		return err
	}
	cmd.Println(summary)
	return nil
}
