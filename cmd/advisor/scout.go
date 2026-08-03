package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"cmnet/internal/scout"
)

type scoutOpts struct {
	bundlePath  string
	outPath     string
	dbPath      string // cache for scouted players' display names (optional)
	windowDays  int
	eventWindow string
}

func newScoutCmd() *cobra.Command {
	var (
		o           scoutOpts
		players     []string
		resolveOnly bool
	)
	cmd := &cobra.Command{
		Use:   "scout",
		Short: "Resolve opponents and freeze an opponent snapshot (layers 2-3)",
		Long: "Resolves opponent account_ids from raw ids, SteamID3/64, or profile URLs " +
			"(OpenDota / STRATZ / Dotabuff / Steam), then fetches player affinity (layer 2) and " +
			"the team's drafts in the current event (layer 3) and freezes them into a shareable " +
			"snapshot. One resolved id of five is enough to recover the whole team's drafts (§9).",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs := append([]string{}, players...)
			refs = append(refs, args...)
			if len(refs) == 0 {
				return fmt.Errorf("provide at least one --player <id|url> (or positional args)")
			}
			if resolveOnly {
				for _, ref := range refs {
					if id, err := scout.ParseAccountID(ref); err != nil {
						cmd.PrintErrf("  %-44s → unresolved: %v\n", ref, err)
					} else {
						cmd.Printf("  %-44s → account_id %d\n", ref, id)
					}
				}
				return nil
			}
			summary, err := opScout(cmd.Context(), refs, o, func(s string) { cmd.Println(s) })
			if err != nil {
				return err
			}
			cmd.Println(summary)
			cmd.Println("share this file with teammates and run: advisor serve --gsi --snapshot " + o.outPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&o.bundlePath, "bundle", "bundle.json", "bundle reference (hero index compatibility)")
	cmd.Flags().StringVar(&o.outPath, "out", "snapshot.json", "output snapshot path")
	cmd.Flags().StringVar(&o.dbPath, "db", "cmnet.db", "local cache, used here to remember players' display names")
	cmd.Flags().StringSliceVar(&players, "player", nil, "opponent id or profile URL (repeatable, up to 5)")
	cmd.Flags().IntVar(&o.windowDays, "window-days", 30, "layer-2 player affinity window in days")
	cmd.Flags().StringVar(&o.eventWindow, "event", "", "layer-3 event label (e.g. battlecup-2026-06-02)")
	cmd.Flags().BoolVar(&resolveOnly, "resolve-only", false, "only resolve account_ids, do not fetch/freeze")
	return cmd
}
