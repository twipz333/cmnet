package main

import (
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"

	"cmnet/internal/gsi/probe"
)

func newGSIProbeCmd() *cobra.Command {
	var (
		listen      string
		output      string
		printConfig bool
		writeConfig string
	)
	cmd := &cobra.Command{
		Use:   "gsi-probe",
		Short: "Capture raw Dota 2 GSI payloads for schema discovery",
		Long: "Starts a local HTTP endpoint, accepts every GSI POST from Dota, and writes " +
			"the original body unchanged. Use this to see which sections and fields the " +
			"playing client actually sends — not to drive the advisor.\n\n" +
			"Print a ready-to-drop .cfg with --print-config, or write it with --write-config. " +
			"The file goes in game/dota/cfg/gamestate_integration/. A copy of the config used " +
			"for the session is stored next to the capture.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if printConfig {
				cmd.Print(probe.ConfigContent(probe.URI(listen)))
				return nil
			}
			if writeConfig != "" {
				path, err := probe.WriteConfig(writeConfig, listen)
				if err != nil {
					return err
				}
				cmd.Printf("GSI config written: %s\n", path)
				cmd.Printf("Copy it to Dota's game/dota/cfg/gamestate_integration/ and restart Dota.\n")
				return nil
			}
			return runGSIProbe(cmd, listen, output)
		},
	}
	f := cmd.Flags()
	f.StringVar(&listen, "listen", probe.DefaultListen, "local HTTP address Dota POSTs to (loopback by default)")
	f.StringVar(&output, "output", "data/gsi-probe", "parent directory for capture sessions")
	f.BoolVar(&printConfig, "print-config", false, "print the GSI .cfg to stdout and exit")
	f.StringVar(&writeConfig, "write-config", "", "write the GSI .cfg to this file or directory and exit")
	return cmd
}

func runGSIProbe(cmd *cobra.Command, listen, output string) error {
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("--listen: %w", err)
	}
	if msg := probe.WarnIfNonLoopback(listen); msg != "" {
		fmt.Fprintln(os.Stderr, "warning:", msg)
	}

	p, err := probe.New(probe.Options{
		Listen:  listen,
		Output:  output,
		Version: version,
		Log: func(msg string) {
			fmt.Fprintln(os.Stderr, msg)
		},
	})
	if err != nil {
		return err
	}

	cmd.Printf("GSI Probe started\n\n")
	cmd.Printf("Listening: %s\n", probe.URI(listen))
	cmd.Printf("Session:   %s\n\n", p.SessionDir())
	cmd.Printf("Waiting for Dota 2 GSI payloads...\n\n")
	cmd.Printf("Press Ctrl+C to stop.\n")

	startErr := p.Start(cmd.Context())
	if cerr := p.Close(); cerr != nil && startErr == nil {
		startErr = cerr
	}
	cmd.Printf("\n%s", p.MakeSummary().String())
	return startErr
}
