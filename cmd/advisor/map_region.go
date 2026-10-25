package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/worldmap"
)

func newMapRegionCmd() *cobra.Command {
	var x, y int
	cmd := &cobra.Command{
		Use:   "map-region",
		Short: "Classify a Dota world coordinate into a macro map region",
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := worldmap.LoadCurrent()
			if err != nil {
				return err
			}
			id := g.RegionAt(worldmap.FromInt(x, y))
			cmd.Printf("%s\n", id)
			for _, lm := range g.LandmarkIDs() {
				d, ok := g.DistanceToLandmark(worldmap.FromInt(x, y), lm)
				if !ok {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s dist=%.0f\n", lm, d)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&x, "x", 0, "world X (GSI xpos)")
	cmd.Flags().IntVar(&y, "y", 0, "world Y (GSI ypos)")
	return cmd
}
