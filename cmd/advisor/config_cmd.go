package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"cmnet/internal/config"
)

// newConfigInitCmd writes a starter config.yaml.
func newConfigInitCmd() *cobra.Command {
	var (
		outPath string
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "config-init",
		Short: "Write a starter config.yaml so settings need not be typed as flags",
		Long: "Creates a commented config.yaml whose keys are flag names. Anything passed on " +
			"the command line still overrides the file.\n\n" +
			"The file is created with owner-only permissions because it is meant to hold the " +
			"STRATZ token; keep it out of version control (it is already in .gitignore).",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Never clobber an existing config: it holds a token the user pasted in
			// by hand, and regenerating it silently would throw that away.
			if _, err := os.Stat(outPath); err == nil && !force {
				return fmt.Errorf("%s already exists (pass --force to overwrite)", outPath)
			}
			if err := os.WriteFile(outPath, []byte(config.Example), 0o600); err != nil {
				return err
			}
			cmd.Printf("wrote %s — put your STRATZ token in it, then run commands without flags\n", outPath)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&outPath, "out", config.DefaultName, "path to write")
	f.BoolVar(&force, "force", false, "overwrite an existing file")
	return cmd
}
