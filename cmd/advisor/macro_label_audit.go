package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel/audit"
)

func newMacroLabelAuditCmd() *cobra.Command {
	var (
		inputPath  string
		reportPath string
	)

	cmd := &cobra.Command{
		Use:   "macro-label-audit",
		Short: "Compare auto macro labels with human audit annotations (NDJSON)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputPath == "" {
				return fmt.Errorf("--input is required")
			}
			f, err := os.Open(inputPath)
			if err != nil {
				return err
			}
			defer f.Close()

			items, err := audit.LoadAuditNDJSON(f)
			if err != nil {
				return err
			}
			space := action.DefaultSpaceV0()
			if err := audit.ValidateHumanActionIDs(items, space); err != nil {
				return err
			}
			if err := audit.ValidateHumanKinds(items); err != nil {
				return err
			}

			metrics := audit.CompareAutoHuman(items, space)
			cmd.Printf("audit items=%d with_human=%d precision=%.3f kind_accuracy=%.3f\n",
				metrics.TotalItems, metrics.WithHuman, metrics.Precision, metrics.KindAccuracy)

			if len(metrics.Confusion) > 0 {
				cmd.Printf("\nConfusion (auto->human):\n")
				for _, k := range audit.SortConfusionKeys(metrics.Confusion) {
					cmd.Printf("  %-24s %d\n", k, metrics.Confusion[k])
				}
			}
			if len(metrics.UnlabeledHumanKinds) > 0 {
				cmd.Printf("\nHuman UNLABELED kind hints:\n")
				for k, v := range metrics.UnlabeledHumanKinds {
					cmd.Printf("  %-18s %d\n", k, v)
				}
			}

			if reportPath != "" {
				out, err := os.Create(reportPath)
				if err != nil {
					return err
				}
				defer out.Close()
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(metrics); err != nil {
					return err
				}
				cmd.Printf("\nreport written to %s\n", reportPath)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&inputPath, "input", "", "audit NDJSON with optional human annotations")
	cmd.Flags().StringVar(&reportPath, "report", "", "write metrics JSON report")
	return cmd
}
