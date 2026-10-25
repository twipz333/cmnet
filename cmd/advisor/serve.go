package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/cache"
	"cmnet/internal/declog"
	"cmnet/internal/draft"
	"cmnet/internal/gsi"
	"cmnet/internal/objectives"
	"cmnet/internal/tui"
	"cmnet/internal/web"
)

func newServeCmd() *cobra.Command {
	var (
		bundlePath         string
		dbPath             string
		side               string
		firstPick          string
		useGSI             bool
		gsiAddr            string
		snapshotPath       string
		logDecisions       bool
		logKeepDays        int
		logKeepDrafts      int
		alerts             alertLeadFlags
		debugState         bool
		debugStateInterval time.Duration
		debugStateFormat   string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the advisor: recommender + TUI, draft screen only (this is what friends launch)",
		Long: "Loads a bundle (verifying its version), then serves deterministic pick/ban " +
			"recommendations and enemy predictions through a TUI. Draft state comes from " +
			"manual input by default, or from Dota GSI with --gsi (run `advisor gsi-install` " +
			"first).\n\n" +
			"This is the draft screen alone: no sync/train/scout. For the full shell " +
			"(sync/train/scout + draft) run `advisor` for the web UI, or `advisor --tui` for the terminal.",
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
				bundlePath:         bundlePath,
				dbPath:             dbPath,
				ourTeam:            ourTeam,
				firstPick:          fp,
				useGSI:             useGSI,
				gsiAddr:            gsiAddr,
				snapshotPath:       snapshotPath,
				logDecisions:       logDecisions,
				logRetention:       declog.Retention{MaxAgeDays: logKeepDays, MaxDrafts: logKeepDrafts},
				alertLeads:         alerts.leads(),
				alertWindows:       alerts.windows(),
				debugState:         debugState,
				debugStateInterval: debugStateInterval,
				debugStateFormat:   debugStateFormat,
			})
		},
	}
	cmd.Flags().StringVar(&bundlePath, "bundle", "bundle.json", "path to the model bundle")
	cmd.Flags().StringVar(&dbPath, "db", "cmnet.db", "cache for hero display names (optional)")
	cmd.Flags().StringVar(&side, "side", "radiant", "our side: radiant|dire")
	cmd.Flags().StringVar(&firstPick, "first-pick", "radiant", "team that picks first: radiant|dire")
	cmd.Flags().BoolVar(&useGSI, "gsi", false, "read draft state from Dota GSI instead of manual input")
	cmd.Flags().StringVar(&gsiAddr, "gsi-addr", "127.0.0.1:54001", "local GSI HTTP listen address")
	cmd.Flags().BoolVar(&debugState, "debug-state", false, "print a live track.State snapshot about once a second (implies --gsi)")
	cmd.Flags().DurationVar(&debugStateInterval, "debug-state-interval", time.Second, "how often --debug-state prints")
	cmd.Flags().StringVar(&debugStateFormat, "debug-state-format", "text", "debug-state format: text|json")
	cmd.Flags().StringVar(&snapshotPath, "snapshot", "", "frozen opponent snapshot to blend enemy prediction (§9 layers 2-3)")
	cmd.Flags().BoolVar(&logDecisions, "decision-log", true,
		"record what was recommended and what was chosen into decisions.jsonl next to --db (local only, never shared, bounded retention)")
	cmd.Flags().IntVar(&logKeepDays, "decision-log-days", declog.DefaultRetention().MaxAgeDays,
		"drop logged drafts older than this many days (0 = no age limit)")
	cmd.Flags().IntVar(&logKeepDrafts, "decision-log-drafts", declog.DefaultRetention().MaxDrafts,
		"keep at most this many logged drafts (0 = no count limit)")
	alerts.bind(cmd.Flags())
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
	// logDecisions turns the prospective decision log on (§6.1); logRetention bounds
	// how much of it is kept.
	logDecisions bool
	logRetention declog.Retention
	// log is that log, opened once per run and shared by every rebuild of this
	// config. Nil disables logging.
	log                *declog.Logger
	alertLeads         objectives.Leads
	alertWindows       objectives.Windows
	alertsPath         string
	debugState         bool
	debugStateInterval time.Duration
	debugStateFormat   string
}

func runServe(cmd *cobra.Command, o serveOpts) error {
	log, err := openDecisionLog(o.dbPath, o.logDecisions, o.logRetention)
	if err != nil {
		cmd.PrintErrf("%v — continuing without it\n", err)
	}
	defer log.Close()
	o.log = log

	cfg, err := buildScanConfig(o)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if o.debugState {
		o.useGSI = true
	}
	if o.useGSI {
		if path, err := gsi.Install("", o.gsiAddr, ""); err != nil {
			cmd.PrintErrf("could not auto-install the GSI config: %v\n", err)
		} else {
			cmd.PrintErrf("GSI config installed: %s (RESTART Dota if it was already running)\n", path)
		}
		srv := gsi.NewServer(o.gsiAddr)
		pipe := startLiveTracker(ctx, srv, o.dbPath, liveDebug{
			Enabled:  o.debugState,
			Interval: o.debugStateInterval,
			Format:   o.debugStateFormat,
			Out:      cmd.ErrOrStderr(),
		})
		if pipe != nil {
			defer pipe.Close()
		}
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
func runWebShell(ctx context.Context, cmd *cobra.Command, shell tui.ShellConfig, addr string, openBrowser bool, remoteToken string) error {
	srv := web.New(shell, addr)
	srv.SetRemoteToken(remoteToken)
	return serveWeb(ctx, cmd, srv, openBrowser)
}

// serveWeb runs srv and prints its URL once bound. A browser is opened only when
// openBrowser is true (opt-in via --open); restarts otherwise stack tabs.
func serveWeb(ctx context.Context, cmd *cobra.Command, srv *web.Server, openBrowser bool) error {
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
	cmd.PrintErrf("web UI: %s  (ctrl-c to stop)\n", url)
	if openBrowser {
		if err := openBrowserURL(url); err != nil {
			cmd.PrintErrf("could not open a browser automatically: %v\n", err)
		}
	}
	return <-errc
}

// openBrowserURL opens url in the user's default browser. Failure is not fatal —
// the URL is printed either way.
func openBrowserURL(url string) error {
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
