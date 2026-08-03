package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/cache"
	"cmnet/internal/eval"
)

func newEvalCmd() *cobra.Command {
	var (
		dbPath        string
		testFrac      float64
		halfLife      float64
		windowDays    int
		trainDomain   string
		bootstrap     int
		folds         int
		sweepHalfLife []float64
		fmRank        int
		noPrefix      bool
		l2            float64
		epochs        int
		proWeight     float64
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Offline quality metrics via a time-based train/test split (writes nothing)",
		Long: "Trains layer-1 models on the older fraction of cached matches and scores them on " +
			"the held-out newer fraction: win-prob log-loss / Brier / ROC AUC / ECE (segmented into " +
			"CM vs AP), partial-draft strength vs final outcome by draft phase, and next-pick " +
			"predictor top-k. Trains into memory only — bundle.json is never touched.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cache.Open(dbPath)
			if err != nil {
				return err
			}
			defer store.Close()

			heroes, err := store.Heroes()
			if err != nil {
				return err
			}
			if len(heroes) == 0 {
				return fmt.Errorf("no heroes cached — run sync first")
			}
			heroIDs := make([]int, len(heroes))
			for i, h := range heroes {
				heroIDs[i] = h.ID
			}

			var since int64
			if windowDays > 0 {
				since = time.Now().AddDate(0, 0, -windowDays).Unix()
			}
			matches, err := store.Matches(since)
			if err != nil {
				return err
			}
			cmd.Printf("loaded %d matches (window-days=%d, 0=all)\n", len(matches), windowDays)

			rep, err := eval.Run(matches, heroIDs, eval.Options{
				TestFrac:      testFrac,
				HalfLifeDays:  halfLife,
				TrainDomain:   trainDomain,
				Bootstrap:     bootstrap,
				Folds:         folds,
				SweepHalfLife: sweepHalfLife,
				FMRank:        fmRank,
				NoPrefix:      noPrefix,
				L2:            l2,
				Epochs:        epochs,
				ProWeight:     proWeight,
			})
			if err != nil {
				return err
			}
			cmd.Println(rep.String())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&dbPath, "db", "cmnet.db", "path to the local SQLite cache")
	f.Float64Var(&testFrac, "test-frac", 0.2, "fraction of the newest matches held out for testing")
	f.Float64Var(&halfLife, "half-life", 0, "recency half-life in days (0 = train default, <0 = uniform)")
	f.IntVar(&windowDays, "window-days", 0, "limit to matches within N days of now (0 = all cached)")
	f.StringVar(&trainDomain, "train-domain", "all", "restrict TRAINING matches: all|cm|ap (test is never filtered; ablates domain shift)")
	f.IntVar(&bootstrap, "bootstrap", eval.DefaultBootstrapN, "match-level bootstrap resamples behind each 95% CI (0 = point estimates only)")
	f.IntVar(&folds, "folds", 1, "rolling time splits to evaluate; >1 checks that a result survives several meta windows")
	f.Float64SliceVar(&sweepHalfLife, "sweep-half-life", nil, "also evaluate these recency half-lives in days (e.g. 7,14,30,60,-1 for uniform)")
	f.IntVar(&fmRank, "fm-rank", 0, "also train and score a factorization machine of this latent rank (0 = skip, -1 = default rank)")
	f.BoolVar(&noPrefix, "no-prefix", false, "train win-prob on final compositions only (ablates prefix training)")
	f.Float64Var(&l2, "l2", 0, "override the L2 penalty for both heads (0 = model default)")
	f.IntVar(&epochs, "epochs", 0, "override the max training epochs (0 = model default)")
	f.Float64Var(&proWeight, "pro-weight", 0, "training weight of pro CM drafts vs public games (0 = default, 1 = no down-weighting)")
	return cmd
}
