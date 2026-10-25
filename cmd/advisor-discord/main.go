package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"cmnet/internal/discordbot"
)

func main() {
	var cfgPath string
	root := &cobra.Command{
		Use:   "advisor-discord",
		Short: "Discord remote-control + NTTS voice alerts for local advisor",
		Long: "Separate process from advisor.exe so Clash can PROXY Discord/NTTS while " +
			"keeping 127.0.0.1 DIRECT to the advisor web API.\n\n" +
			"Requires advisor --remote-token matching advisor-token in discord.yaml.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return discordbot.Run(ctx, cfgPath)
		},
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "discord.yaml", "path to discord bot config")
	root.AddCommand(&cobra.Command{
		Use:   "sync-commands",
		Short: "Reset and re-register guild slash commands",
		Long:  "Clears global commands and BulkOverwrite the guild list from discord.yaml.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return discordbot.SyncCommandsOnce(cfgPath)
		},
	})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}