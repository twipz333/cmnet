// Command advisor is a local Dota 2 draft advisor: data sync, model training,
// and a deterministic pick/ban recommender served through a TUI.
//
// Build (static, no CGO): GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/advisor
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/alerts"
	"cmnet/internal/config"
	"cmnet/internal/declog"
	"cmnet/internal/discordbot"
	"cmnet/internal/draft"
	"cmnet/internal/gsi"
	"cmnet/internal/session"
	"cmnet/internal/startup"
	"cmnet/internal/tui"
)

// version is overridable via -ldflags "-X main.version=...".
var version = "dev"

// envDefault returns the environment variable named key, or def when it is unset
// or empty. Used to default secrets (the STRATZ token) from the environment so
// they need not be typed on the command line / kept in shell history.
func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newRootCmd() *cobra.Command {
	var h homeFlags
	var configPath string
	root := &cobra.Command{
		Use:   "advisor",
		Short: "Dota 2 draft advisor — deterministic pick/ban recommendations",
		Long: "Run with no subcommand to open the unified TUI home (train, scan, scout all in " +
			"one place), e.g.\n\n  advisor --gsi --token <STRATZ_TOKEN>\n\n" +
			"Subcommands (sync/train/serve/scout/gsi-install/gsi-probe) remain available for scripting.\n\n" +
			"Settings can live in config.yaml instead of flags (see 'advisor config-init'); " +
			"anything passed on the command line still wins over the file.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Applies to every subcommand: load the config and use it to fill in the
		// flags the user did not pass.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			f, err := config.Load(config.Find(configPath))
			if err != nil {
				return err
			}
			return f.Apply(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHome(cmd, h)
		},
	}
	h.bind(root)
	root.PersistentFlags().StringVar(&configPath, "config", "",
		"path to config.yaml (default: ./config.yaml, else next to the binary)")
	root.AddCommand(
		newConfigInitCmd(),
		newSyncCmd(),
		newTrainCmd(),
		newServeCmd(),
		newScoutCmd(),
		newGSIInstallCmd(),
		newGSIProbeCmd(),
		newStateReplayCmd(),
		newMacroLabelCmd(),
		newMacroLabelAuditCmd(),
		newMacroLabelReviewCmd(),
		newDotaMapInstallCmd(),
		newActionSpaceCmd(),
		newMapRegionCmd(),
		newRegionPathCmd(),
		newLaneProjectCmd(),
		newGridBakeCmd(),
		newPairStatsCmd(),
		newEvalCmd(),
		newHoldoutCmd(),
		newLogCmd(),
	)
	return root
}

// homeFlags are the root flags for the unified TUI home.
type homeFlags struct {
	bundlePath         string
	dbPath             string
	snapshotPath       string
	side               string
	firstPick          string
	useGSI             bool
	gsiAddr            string
	gsiToken           string
	windowDays         int
	minRankTier        int
	maxRankTier        int
	maxMatches         int
	cmMatches          int
	bulkGameMode       int
	token              string
	useTUI             bool
	webAddr            string
	openBrowser        bool
	publicCM           int
	logDecisions       bool
	logKeepDays        int
	logKeepDrafts      int
	auto               autoOpts
	alertLeads         alertLeadFlags
	remoteToken        string
	discordConfig      string
	startDiscord       bool
	alertsPath         string
	debugState         bool
	debugStateInterval time.Duration
	debugStateFormat   string
	guest              bool
}

func (h *homeFlags) bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&h.bundlePath, "bundle", "bundle.json", "model bundle path")
	f.StringVar(&h.dbPath, "db", "cmnet.db", "local SQLite cache path")
	f.StringVar(&h.snapshotPath, "snapshot", "snapshot.json", "opponent snapshot path")
	f.StringVar(&h.side, "side", "radiant", "our side: radiant|dire")
	f.StringVar(&h.firstPick, "first-pick", "radiant", "team that picks first: radiant|dire")
	f.BoolVar(&h.useGSI, "gsi", false, "use Dota GSI for live draft state in the scan screen")
	f.StringVar(&h.gsiAddr, "gsi-addr", "127.0.0.1:54001", "local GSI HTTP listen address")
	f.StringVar(&h.gsiToken, "gsi-token", "", "shared secret Dota echoes back; when set, events without it are rejected (RESTART Dota after changing)")
	h.alertLeads.bind(f)
	f.StringVar(&h.remoteToken, "remote-token", "",
		"shared secret for /api/remote/* (Discord bot); empty disables remote control")
	f.StringVar(&h.discordConfig, "discord-config", "",
		"path to discord.yaml for the web editor (default: ./discord.yaml or next to the binary)")
	f.BoolVar(&h.startDiscord, "discord", true,
		"start advisor-discord as a separate process with the web UI (needs discord.yaml + advisor-discord.exe nearby)")
	f.StringVar(&h.alertsPath, "alerts", "",
		"path to alerts.yaml (default: ./alerts.yaml or next to the binary)")
	f.BoolVar(&h.debugState, "debug-state", false, "print a live track.State snapshot about once a second (implies --gsi)")
	f.DurationVar(&h.debugStateInterval, "debug-state-interval", time.Second, "how often --debug-state prints")
	f.StringVar(&h.debugStateFormat, "debug-state-format", "text", "debug-state format: text|json")
	f.BoolVar(&h.guest, "guest", edition == "guest", "guest app: no sync, train or scouting; the model and scouting come from a host by code")
	f.IntVar(&h.windowDays, "window-days", 31, "recency window in days (T-N)")
	f.IntVar(&h.minRankTier, "min-rank-tier", 40, "min avg_rank_tier (40=Archon,50=Legend,60=Ancient,70=Divine,80=Immortal)")
	f.IntVar(&h.maxRankTier, "max-rank-tier", 85, "max avg_rank_tier")
	f.IntVar(&h.maxMatches, "max-matches", 2500, "cap on new matches fetched per sync run (0 = unlimited)")
	f.IntVar(&h.cmMatches, "cm-matches", 500, "cap on real CM drafts (with bans) fetched per sync for the next-pick predictor (0 = skip)")
	f.IntVar(&h.bulkGameMode, "bulk-game-mode", 22, "Dota game_mode for the bulk pool feeding synergy/counter+win-prob (22=Ranked All Pick, 2=Captains Mode, 0=all)")
	f.StringVar(&h.token, "token", envDefault("STRATZ_TOKEN", ""), "STRATZ API token; enables real per-position meta (#9) on Train. Falls back to OpenDota roles when empty")
	f.BoolVar(&h.useTUI, "tui", false, "use the terminal UI instead of the browser (default is web)")
	f.StringVar(&h.webAddr, "web-addr", "127.0.0.1:54000", "local web UI listen address")
	f.BoolVar(&h.openBrowser, "open", false, "open the web UI in the default browser (off by default — URL is printed)")
	f.IntVar(&h.publicCM, "public-cm", 0, "cap on PUBLIC Captains Mode drafts per manual Sync (0 = skip); needs --token")

	// §6.1: the only measurement that can say whether the advice helps the person
	// holding the draft clock, as opposed to whether the model ranks old drafts
	// well. On by default because it has to accumulate to be worth anything, and
	// it stays on this machine: local file, bounded retention, erasable per player.
	f.BoolVar(&h.logDecisions, "decision-log", true,
		"record what was recommended and what was chosen into decisions.jsonl next to --db (local only, never shared)")
	f.IntVar(&h.logKeepDays, "decision-log-days", declog.DefaultRetention().MaxAgeDays,
		"drop logged drafts older than this many days (0 = no age limit)")
	f.IntVar(&h.logKeepDrafts, "decision-log-drafts", declog.DefaultRetention().MaxDrafts,
		"keep at most this many logged drafts (0 = no count limit)")

	// Background collection. The corpus takes days to fill and a few hundred drafts
	// a day to keep current, which is not something to run by hand: this keeps the
	// cache and the bundle current for as long as the app is open, and picks up
	// where it left off after a restart (the watermarks live in the database).
	//
	// The per-pass caps are deliberately lopsided towards public CM. That is the
	// only stream matching what the advisor is used for — Captains Mode at pub
	// rank — and it is the scarce one (~3.5k cached against ~52k bulk). Bulk is
	// all-pick with no bans and no draft order: it feeds final compositions and the
	// synergy/counter matrices, and those matrices stop being empty at roughly 45k
	// matches, which the cache already passed. Collecting more of it mostly grows a
	// number in the log. Pro CM is real Captains Mode but a different population —
	// coordinated teams where execution, not the draft, decides games.
	f.BoolVar(&h.auto.enabled, "auto", true, "keep syncing (and retraining) in the background while the app is open")
	f.IntVar(&h.auto.publicCM, "auto-public-cm", 400, "public CM drafts per background pass (0 = skip); short passes checkpoint often")
	f.IntVar(&h.auto.maxMatches, "auto-max-matches", 100, "bulk matches per background pass (all-pick; keeps the matrices current, no longer the priority)")
	f.IntVar(&h.auto.cmMatches, "auto-cm-matches", 50, "pro CM drafts per background pass")
	f.DurationVar(&h.auto.trainEvery, "auto-train-every", 24*time.Hour, "minimum time between automatic retrains")
	f.IntVar(&h.auto.trainMinNew, "auto-train-min-new", 500, "minimum new public CM drafts before an automatic retrain")
	f.DurationVar(&h.auto.idle, "auto-idle", time.Minute, "pause between background passes that collected something")
	f.DurationVar(&h.auto.maxIdle, "auto-max-idle", 30*time.Minute, "longest pause between background passes (reached when passes come back empty)")
}

// runHome wires the operation callbacks and launches the unified TUI shell.
func runHome(cmd *cobra.Command, h homeFlags) error {
	ourTeam, err := parseTeam(h.side)
	if err != nil {
		return err
	}
	firstPick, err := parseTeam(h.firstPick)
	if err != nil {
		return fmt.Errorf("--first-pick: %w", err)
	}
	// One log per run, shared by every rebuild of the draft config: two handles on
	// the same file would mean two notions of "the draft in progress".
	decisionLog, err := openDecisionLog(h.dbPath, h.logDecisions,
		declog.Retention{MaxAgeDays: h.logKeepDays, MaxDrafts: h.logKeepDrafts})
	if err != nil {
		startup.Warnf("%v — continuing without it", err)
	}
	defer decisionLog.Close()

	scanOpts := serveOpts{
		bundlePath:   h.bundlePath,
		dbPath:       h.dbPath,
		ourTeam:      ourTeam,
		firstPick:    firstPick,
		useGSI:       h.useGSI,
		gsiAddr:      h.gsiAddr,
		snapshotPath: h.snapshotPath,
		log:          decisionLog,
		alertLeads:   h.alertLeads.leads(),
		alertWindows: h.alertLeads.windows(),
		alertsPath:   h.alertsPath,
	}

	// Every long operation runs through the job queue, whichever front end asked
	// for it: one row, one log, one set of rules about what may run alongside what.
	run, err := newRunner(h)
	if err != nil {
		return err
	}
	defer run.Close()
	queueCtx, stopQueue := context.WithCancel(cmd.Context())
	defer stopQueue()
	go run.Run(queueCtx)

	cfg := tui.ShellConfig{
		GSI:         h.useGSI,
		Token:       true, // sync no longer needs a token (OpenDota-only)
		Jobs:        run.Queue(),
		AssetsDir:   assetsDirFor(h.dbPath),
		Train:       run.manualTrain(),
		Scout:       run.scoutOp(jobScout),
		ScoutAllies: run.scoutOp(jobScoutAllies),
		Stats: func() (tui.StatsView, error) {
			return opStats(h.snapshotPath, h.dbPath)
		},
		Corpus: func() (session.CorpusStats, error) {
			return opCorpus(h.dbPath, h.windowDays)
		},
		SetHeroPool: func(tokens []string) (string, error) {
			return opSetHeroPoolTokens(tokens, h.snapshotPath, h.dbPath)
		},
		RemovePlayer: func(accountID int64, ally bool) (string, error) {
			return opRemovePlayer(h.snapshotPath, accountID, ally)
		},
		ClearLayer3: func() (string, error) {
			return opClearLayer3(h.snapshotPath)
		},
		ClearScout: func(ally bool) (string, error) {
			return opClearScout(h.snapshotPath, ally)
		},
		SetItemRules: func(text string) (string, error) {
			return opSetItemRules(text, h.snapshotPath, h.dbPath)
		},
		GetConfig: func() (config.View, error) {
			explicit, _ := cmd.Root().PersistentFlags().GetString("config")
			return config.BuildView(cmd.Root(), config.Find(explicit))
		},
		SaveConfig: func(req config.SaveRequest) (string, error) {
			explicit, _ := cmd.Root().PersistentFlags().GetString("config")
			path := config.Find(explicit)
			if err := config.SaveView(cmd.Root(), path, req); err != nil {
				return "", err
			}
			return fmt.Sprintf("сохранено в %s — перезапустите advisor, чтобы применить", path), nil
		},
		GetDiscordConfig: func() (discordbot.EditorView, error) {
			return discordbot.BuildEditorView(discordbot.FindConfig(h.discordConfig))
		},
		SaveDiscordConfig: func(req discordbot.EditorSaveRequest) (string, error) {
			path := discordbot.FindConfig(h.discordConfig)
			if err := discordbot.SaveEditor(path, req); err != nil {
				return "", err
			}
			return fmt.Sprintf("сохранено в %s — advisor-discord подхватит сам", path), nil
		},
		GetAlerts: func() (alerts.View, error) {
			path := alerts.Find(h.alertsPath)
			_, existed := os.Stat(path)
			f, err := alerts.EnsureFile(path, h.alertLeads.leads(), h.alertLeads.windows())
			if err != nil {
				startup.Warnf("alerts.yaml: %v", err)
			}
			if os.IsNotExist(existed) {
				seedTTSFromDiscord(f, h.discordConfig)
				_ = alerts.Save(path, f)
			}
			return alerts.BuildView(path, f), nil
		},
		SaveAlerts: func(req alerts.SaveRequest) (string, *alerts.File, error) {
			path := alerts.Find(h.alertsPath)
			f := alerts.ApplySave(req)
			if err := alerts.Save(path, f); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("сохранено в %s — применяется сразу", path), f, nil
		},
		ListTTSCache: func() ([]discordbot.CacheEntry, error) {
			return discordbot.ListCacheDir(ttsCacheDir(h.discordConfig))
		},
		DeleteTTSCache: func(key string) (string, error) {
			dir := ttsCacheDir(h.discordConfig)
			if err := discordbot.DeleteCacheKey(dir, key); err != nil {
				return "", err
			}
			return "озвучка удалена", nil
		},
		ClearTTSCache: func() (string, error) {
			dir := ttsCacheDir(h.discordConfig)
			if err := discordbot.ClearCacheDir(dir); err != nil {
				return "", err
			}
			return "кэш озвучки очищен", nil
		},
		CorpusMeta: func() (session.CorpusMetaView, error) {
			return opCorpusMeta(h.bundlePath, h.dbPath)
		},
		KnownPlayers: func(role string) ([]session.KnownPlayer, error) {
			return opKnownPlayers(h.dbPath, h.snapshotPath, role)
		},
		AddPlayer: func(ref string, ally bool) (string, error) {
			return opAddPlayer(cmd.Context(), h.dbPath, ref, ally)
		},
		RefreshProfiles: func(ids []int64) (string, error) {
			return opRefreshProfiles(cmd.Context(), h.dbPath, ids)
		},
		ForgetPlayer: func(accountID int64) (string, error) {
			return opForgetPlayer(h.dbPath, accountID, decisionLog)
		},
		PinPlayer: func(accountID int64, pinned bool) (string, error) {
			return opPinPlayer(h.dbPath, accountID, pinned)
		},
		Sync: run.manualSync(),
	}
	if h.guest {
		applyGuest(&cfg, &h)
	}
	shareShell(&cfg, h)
	// Background collection. The lock is claimed before the worker is wired into
	// anything, so a second instance behaves exactly like one started with
	// --auto=false rather than half-connected.
	var autoLockHeld *autoLock
	if auto := buildAuto(h.auto, h, run); auto != nil {
		lock, held := acquireAutoLock(h.dbPath)
		if lock == nil {
			// Someone else is already collecting into this cache. Run the UI anyway:
			// everything except background collection works fine on a shared database.
			startup.Warnf("background collection is off: %s", held)
		} else {
			defer lock.Release()
			autoLockHeld = lock
			cfg.Auto = auto
		}
	}

	if h.debugState {
		h.useGSI = true
	}

	// One GSI server for the whole session; its events feed the scan screen,
	// and the same receiver feeds the live state tracker via Pipe.
	var gsiEvents <-chan *draft.Snapshot
	if h.useGSI {
		// Auto-install the GSI config so Dota actually POSTs to us. Without this file
		// in Dota's gamestate_integration dir the game never sends events and the TUI
		// hangs forever on "waiting". Best-effort: a failure here (e.g. Dota dir not
		// found) is reported but does not block manual fallback.
		if path, err := gsi.Install("", h.gsiAddr, h.gsiToken); err != nil {
			startup.Warnf("could not auto-install the GSI config: %v", err)
			startup.Warnf("run 'advisor gsi-install --cfg-dir <dota cfg dir>' by hand, then RESTART Dota")
		} else {
			startup.Infof("GSI config installed: %s (RESTART Dota if it was already running)", path)
		}
		// The token is checked, not merely written: without it the endpoint accepted
		// a POST from anything on the machine that could reach the port.
		srv := gsi.NewServerWithToken(h.gsiAddr, h.gsiToken)
		pipe := startLiveTracker(cmd.Context(), srv, h.dbPath, liveDebug{
			Enabled:  h.debugState,
			Interval: h.debugStateInterval,
			Format:   h.debugStateFormat,
		})
		if pipe != nil {
			defer pipe.Close()
		}
		go func() { _ = srv.Start(cmd.Context()) }()
		gsiEvents = srv.Events()
	}
	cfg.BuildScan = func() (tui.Config, error) {
		c, err := buildScanConfig(scanOpts)
		if err != nil {
			return tui.Config{}, err
		}
		if gsiEvents != nil {
			c.GSIEvents = gsiEvents
		}
		return c, nil
	}

	if cfg.Auto != nil {
		// Tied to the app's lifetime: the worker owns no state that outlives the
		// process, so stopping it is just cancelling the context. Whatever a killed
		// pass had collected is already committed, and the watermarks in the database
		// tell the next run where to resume.
		autoCtx, stopAuto := context.WithCancel(cmd.Context())
		defer stopAuto()
		go cfg.Auto.Run(autoCtx)

		// Two instances can decide at the same moment that an old lock is stale and
		// both claim it; the one whose token loses stops here, so they do not spend
		// the same daily quota twice over.
		if autoLockHeld != nil {
			go func() {
				select {
				case <-autoCtx.Done():
				case <-autoLockHeld.Lost():
					startup.Warnf("background collection stopped: another advisor took over the cache")
					stopAuto()
				}
			}()
		}
	}

	if h.useTUI {
		return tui.RunShell(cfg)
	}
	// Default: browser UI over the full shell (sync/train/scout + draft).
	remote := strings.TrimSpace(h.remoteToken)
	if remote == "" {
		remote = discordbot.AdvisorRemoteToken(h.discordConfig)
		if remote != "" {
			startup.Infof("remote-control: using advisor-token from discord.yaml")
		}
	}
	if h.startDiscord {
		maybeStartDiscord(cmd.Context(), h.discordConfig)
	}
	return runWebShell(cmd.Context(), cmd, cfg, h.webAddr, h.openBrowser, remote)
}

func seedTTSFromDiscord(f *alerts.File, discordConfig string) {
	if f == nil {
		return
	}
	cfg, _, err := discordbot.LoadLoose(discordbot.FindConfig(discordConfig))
	if err != nil || cfg == nil || cfg.NTTS.Templates == nil {
		return
	}
	if t := cfg.NTTS.Templates["default"]; t != "" {
		f.DefaultTTS = t
	}
	for k, tmpl := range cfg.NTTS.Templates {
		if k == "default" || tmpl == "" {
			continue
		}
		ks := f.Kinds[k]
		if ks.TTS == "" {
			ks.TTS = tmpl
			f.Kinds[k] = ks
		}
	}
}

func ttsCacheDir(discordConfig string) string {
	path := discordbot.FindConfig(discordConfig)
	cfg, _, err := discordbot.LoadLoose(path)
	if err != nil || cfg == nil {
		return "tts-cache"
	}
	if cfg.NTTS.CacheDir != "" {
		return cfg.NTTS.CacheDir
	}
	return "tts-cache"
}

func main() {
	// Ctrl+C asks for a stop rather than taking one. Without this the process was
	// killed outright: whatever the collection pass had not yet committed was
	// lost, the queue never got to write out its final log lines, and the job row
	// stayed marked running until the next start recovered it.
	//
	// A second Ctrl+C gives up waiting — a hung source must not be able to hold
	// the terminal hostage.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "\nstopping: finishing the current step, press Ctrl+C again to quit now…")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Fprintln(os.Stderr, "quitting without finishing")
		os.Exit(130) // 128 + SIGINT, the shell convention
	}()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
