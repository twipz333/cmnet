package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/belief"
)

func newRegionPathCmd() *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   "region-path",
		Short: "Show the deterministic shortest macro walking path between two regions",
		RunE: func(cmd *cobra.Command, args []string) error {
			graph, err := worldmap.LoadRegionGraphCurrent()
			if err != nil {
				return err
			}
			path, dist, ok := graph.ShortestWalkPath(worldmap.RegionID(from), worldmap.RegionID(to))
			if !ok {
				return fmt.Errorf("no graph path from %s to %s", from, to)
			}
			cfg := belief.DefaultConfig()
			cmd.Printf("distance=%.0f\n", dist)
			cmd.Printf("walk_time=%.1fs\n", dist/cfg.ReferenceWalkSpeed)
			cmd.Printf("path:\n")
			for _, region := range path {
				cmd.Printf("  %s\n", region)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source RegionID")
	cmd.Flags().StringVar(&to, "to", "", "target RegionID")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
