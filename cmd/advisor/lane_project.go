package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/lanestate"
)

func newLaneProjectCmd() *cobra.Command {
	var x, y int
	var side string
	cmd := &cobra.Command{
		Use:   "lane-project",
		Short: "Project a Dota world coordinate onto versioned lane polylines",
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := worldmap.LoadLanesCurrent()
			if err != nil {
				return err
			}
			p := worldmap.FromInt(x, y)
			var fromRadiant *bool
			switch strings.ToLower(strings.TrimSpace(side)) {
			case "radiant":
				v := true
				fromRadiant = &v
			case "dire":
				v := false
				fromRadiant = &v
			case "":
			default:
				return fmt.Errorf("--side must be radiant, dire, or empty")
			}
			cmd.Print(lanestate.FormatProjection(g, p, fromRadiant))
			return nil
		},
	}
	cmd.Flags().IntVar(&x, "x", 0, "world X (GSI xpos)")
	cmd.Flags().IntVar(&y, "y", 0, "world Y (GSI ypos)")
	cmd.Flags().StringVar(&side, "side", "", "optional radiant|dire for team_progress")
	return cmd
}
