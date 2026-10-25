package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/cache"
	"cmnet/internal/eval"
)

func newHoldoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "holdout",
		Short: "Seal / inspect the §6.8 final hold-out manifest",
		Long: "The final hold-out is a sealed set of match ids that must never be used for " +
			"hyperparameter tuning. Seal once, then pass --exclude-holdout to every iterative " +
			"eval and --final-holdout only for the one-shot final report.",
	}
	cmd.AddCommand(newHoldoutSealCmd(), newHoldoutStatusCmd())
	return cmd
}

func newHoldoutSealCmd() *cobra.Command {
	var (
		dbPath     string
		outPath    string
		frac       float64
		stratify   string
		windowDays int
		force      bool
	)
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal the newest frac of target-domain matches into a holdout manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cache.Open(dbPath)
			if err != nil {
				return err
			}
			defer store.Close()

			var since int64
			if windowDays > 0 {
				since = time.Now().AddDate(0, 0, -windowDays).Unix()
			}
			matches, err := store.Matches(since)
			if err != nil {
				return err
			}
			h, err := eval.SealHoldout(matches, frac, stratify)
			if err != nil {
				return err
			}
			if err := eval.SaveHoldout(outPath, h, force); err != nil {
				return err
			}
			cmd.Printf("sealed holdout → %s\n", outPath)
			cmd.Printf("  n=%d  frac=%.3f  stratify=%s  policy=%s\n", h.N, h.Frac, h.Stratify, h.Policy)
			cmd.Printf("  start_time=%d  end_time=%d\n", h.MinStartTime, h.MaxStartTime)
			cmd.Printf("  sha256=%s\n", h.ContentSHA256)
			cmd.Printf("  sealed_at=%s\n", h.SealedAt)
			cmd.Println("use: advisor eval --exclude-holdout " + outPath + " …   (tuning)")
			cmd.Println("     advisor eval --final-holdout " + outPath + " …     (one-shot final)")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&dbPath, "db", "cmnet.db", "path to the local SQLite cache")
	f.StringVar(&outPath, "out", "holdout.json", "path for the sealed manifest")
	f.Float64Var(&frac, "frac", 0.1, "newest fraction of target-domain matches to seal (must be < 0.5)")
	f.StringVar(&stratify, "stratify", "cm", "target domain: cm|public-cm")
	f.IntVar(&windowDays, "window-days", 0, "limit corpus to N days (0 = all cached)")
	f.BoolVar(&force, "force", false, "overwrite an existing sealed holdout (burns the old final set)")
	return cmd
}

func newHoldoutStatusCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show and verify a sealed holdout manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := eval.LoadHoldout(outPath)
			if err != nil {
				return err
			}
			cmd.Printf("holdout %s OK\n", outPath)
			cmd.Printf("  n=%d  frac=%.3f  stratify=%s  policy=%s\n", h.N, h.Frac, h.Stratify, h.Policy)
			cmd.Printf("  start_time=%d  end_time=%d\n", h.MinStartTime, h.MaxStartTime)
			cmd.Printf("  sha256=%s\n", h.ContentSHA256)
			cmd.Printf("  sealed_at=%s\n", h.SealedAt)
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "holdout.json", "path to the sealed manifest")
	return cmd
}

// loadHoldoutFlag is shared by eval flags; empty path → nil.
func loadHoldoutFlag(path string) (*eval.Holdout, error) {
	if path == "" {
		return nil, nil
	}
	h, err := eval.LoadHoldout(path)
	if err != nil {
		return nil, fmt.Errorf("holdout %s: %w", path, err)
	}
	return h, nil
}
