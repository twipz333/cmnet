package main

import (
	"github.com/spf13/cobra"

	"cmnet/internal/gsi"
)

func newGSIInstallCmd() *cobra.Command {
	var (
		dotaCfgDir string
		gsiAddr    string
		token      string
	)
	cmd := &cobra.Command{
		Use:   "gsi-install",
		Short: "Install the Dota GSI config pointing at the local advisor endpoint",
		Long: "Generates gamestate_integration_advisor.cfg and writes it into the Dota " +
			"gamestate_integration config directory so Dota POSTs live draft state to the local " +
			"advisor. Idempotent.",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := gsi.Install(dotaCfgDir, gsiAddr, token)
			if err != nil {
				return err
			}
			cmd.Printf("GSI config written: %s\n", path)
			cmd.Printf("Now run: advisor serve --gsi --gsi-addr %s\n", gsiAddr)
			return nil
		},
	}
	cmd.Flags().StringVar(&dotaCfgDir, "cfg-dir", "", "Dota gamestate_integration cfg dir (autodetected if empty)")
	cmd.Flags().StringVar(&gsiAddr, "gsi-addr", "127.0.0.1:53000", "local GSI HTTP endpoint the config points to")
	cmd.Flags().StringVar(&token, "token", "", "optional auth token echoed back by Dota in events")
	return cmd
}
