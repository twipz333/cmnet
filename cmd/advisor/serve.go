package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/gsi"
	"cmnet/internal/tui"
	"cmnet/internal/web"
)

func newServeCmd() *cobra.Command {
	var (
		bundlePath   string
		dbPath       string
		side         string
		firstPick    string
		useGSI       bool
		gsiAddr      string
		snapshotPath string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the advisor: recommender + TUI, draft screen only (this is what friends launch)",
		Long: "Loads a bundle (verifying its version), then serves deterministic pick/ban " +
			"recommendations and enemy predictions through a TUI. Draft state comes from " +
			"manual input by default, or from Dota GSI with --gsi (run `advisor gsi-install` " +
			"first).\n\n" +
			"This is the draft screen alone: no sync/train/scout. For the browser UI — and " +
			"for every operation the terminal home offers — run `advisor --web` with no " +
			"subcommand.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ourTeam, err := parseTeam(side)
			if err != nil {
				return err
			}
			fp, err := parseTeam(firstPick)
			if err != nil {
				return fmt.Errorf("--first-pick: %w", err)
			}
			return runServe(cmd, serveOpts{
				bundlePath:   bundlePath,
				dbPath:       dbPath,
				ourTeam:      ourTeam,
				firstPick:    fp,
				useGSI:       useGSI,
				gsiAddr:      gsiAddr,
				snapshotPath: snapshotPath,
			})
		},
	}
	cmd.Flags().StringVar(&bundlePath, "bundle", "bundle.json", "path to the model bundle")
	cmd.Flags().StringVar(&dbPath, "db", "cmnet.db", "cache for hero display names (optional)")
	cmd.Flags().StringVar(&side, "side", "radiant", "our side: radiant|dire")
	cmd.Flags().StringVar(&firstPick, "first-pick", "radiant", "team that picks first: radiant|dire")
	cmd.Flags().BoolVar(&useGSI, "gsi", false, "read draft state from Dota GSI instead of manual input")
	cmd.Flags().StringVar(&gsiAddr, "gsi-addr", "127.0.0.1:53000", "local GSI HTTP listen address")
	cmd.Flags().StringVar(&snapshotPath, "snapshot", "", "frozen opponent snapshot to blend enemy prediction (§9 layers 2-3)")
	return cmd
}

func parseTeam(s string) (draft.Team, error) {
	switch s {
	case "radiant", "r", "Radiant":
		return draft.Radiant, nil
	case "dire", "d", "Dire":
		return draft.Dire, nil
	default:
		return draft.Radiant, fmt.Errorf("invalid team %q (want radiant|dire)", s)
	}
}

type serveOpts struct {
	bundlePath   string
	dbPath       string
	ourTeam      draft.Team
	firstPick    draft.Team
	useGSI       bool
	gsiAddr      string
	snapshotPath string
}

func runServe(cmd *cobra.Command, o serveOpts) error {
	cfg, err := buildScanConfig(o)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if o.useGSI {
		srv := gsi.NewServer(o.gsiAddr)
		go func() {
			if err := srv.Start(ctx); err != nil {
				cmd.PrintErrf("gsi server: %v\n", err)
			}
		}()
		cfg.GSIEvents = srv.Events()
		cmd.PrintErrf("GSI listening on %s — start a Dota match\n", o.gsiAddr)
	}

	return tui.Run(cfg)
}

// runWebShell serves the browser front end over the full shell: the draft screen
// plus sync/train/scout and snapshot editing, i.e. everything the terminal home
// offers.
func runWebShell(ctx context.Context, cmd *cobra.Command, shell tui.ShellConfig, addr string, noOpen bool) error {
	return serveWeb(ctx, cmd, web.New(shell, addr), noOpen)
}

// serveWeb runs srv, advertises its URL once it has bound, and optionally opens a
// browser at it.
func serveWeb(ctx context.Context, cmd *cobra.Command, srv *web.Server, noOpen bool) error {
	// Listening happens inside Run; give it a moment to bind before advertising the
	// URL, and only open a browser once it is actually up.
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx) }()

	select {
	case err := <-errc:
		return err
	case <-time.After(150 * time.Millisecond):
	}

	url := "http://" + srv.Addr()
	cmd.PrintErrf("web UI on %s (ctrl-c to stop)\n", url)
	if !noOpen {
		if err := openBrowser(url); err != nil {
			cmd.PrintErrf("could not open a browser automatically: %v\n", err)
		}
	}
	return <-errc
}

// openBrowser opens url in the user's default browser. Failure is not fatal — the
// URL is printed either way.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// loadHeroDisplay maps the bundle's hero ids to display names from the cache when
// available, falling back to id-only labels. Names are display-only and never
// affect the model (§6).
func loadHeroDisplay(dbPath string, ids []int) []tui.Hero {
	names := map[int]cache.Hero{}
	if store, err := cache.Open(dbPath); err == nil {
		defer store.Close()
		if hs, err := store.Heroes(); err == nil {
			for _, h := range hs {
				names[h.ID] = h
			}
		}
	}
	out := make([]tui.Hero, len(ids))
	for i, id := range ids {
		h := tui.Hero{ID: id}
		if c, ok := names[id]; ok {
			h.Name, h.Localized = c.Name, c.Localized
		}
		out[i] = h
	}
	return out
}
