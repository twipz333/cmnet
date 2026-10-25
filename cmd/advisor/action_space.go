package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/action"
)

func newActionSpaceCmd() *cobra.Command {
	var showID int
	cmd := &cobra.Command{
		Use:   "action-space",
		Short: "Inspect MacroAction v0.1 schema-valid action space",
		RunE: func(cmd *cobra.Command, args []string) error {
			sp := action.DefaultSpaceV0()
			if showID >= 0 {
				a, ok := sp.ByID(showID)
				if !ok {
					return fmt.Errorf("unknown action id %d", showID)
				}
				cmd.Printf("Schema: %s\n", a.SchemaVersion)
				cmd.Printf("ID: %d\n", a.ID)
				cmd.Printf("Kind: %s\n", a.Kind)
				cmd.Printf("Target: %s\n", a.Target)
				cmd.Printf("Meaning: %s\n", action.Meaning(a.Kind, a.Target))
				return nil
			}

			actions := sp.All()
			cmd.Printf("MacroAction schema: %s\n", sp.SchemaVersion())
			cmd.Printf("Kinds: %d\n", len(action.KindVocab()))
			cmd.Printf("Targets: %d\n", len(action.TargetVocab()))
			cmd.Printf("Valid actions: %d\n\n", len(actions))

			cmd.Printf("ID\tKIND\tTARGET\n")
			for _, a := range actions {
				cmd.Printf("%d\t%s\t%s\n", a.ID, a.Kind, a.Target)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&showID, "id", -1, "show one action by id")
	return cmd
}
