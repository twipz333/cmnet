package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"cmnet/internal/session"
)

// ShellConfig wires a front end's home screen to the app's operations. It lives in
// session because the web UI drives the same operations; this alias keeps the
// terminal code (and cmd/advisor) reading as before.
type ShellConfig = session.ShellConfig

// RunShell launches the unified home TUI and blocks until quit.
func RunShell(cfg ShellConfig) error {
	_, err := tea.NewProgram(newShell(cfg), tea.WithAltScreen()).Run()
	return err
}

type shellScreen int

const (
	screenHome shellScreen = iota
	screenRunning
	screenScout
	screenAllies
	screenStats
	screenPoolEdit
	screenRulesEdit
	screenScan
)

type menuItem struct {
	id, title, desc string
	enabled         bool
}

func (m menuItem) Title() string       { return m.title }
func (m menuItem) Description() string  { return m.desc }
func (m menuItem) FilterValue() string  { return m.title }

// opDoneMsg reports the result of a long operation.
type opDoneMsg struct {
	summary string
	err     error
}

// progressMsg is one progress line emitted by a long operation.
type progressMsg string

// waitProgress blocks on the next progress line; returns nil when the channel
// is closed (op finished), which tea ignores and the listen loop stops.
func waitProgress(ch chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return progressMsg(line)
	}
}

type shell struct {
	cfg     ShellConfig
	th      theme
	screen  shellScreen
	menu    list.Model
	spin    spinner.Model
	input    textinput.Model
	rules    textarea.Model
	scan          model
	scanInit      bool // whether scan has been constructed
	scanExitArmed bool // first esc arms exit; second confirms (accidental-exit guard)
	running  string // label of the in-progress op
	cancel   context.CancelFunc // cancels the in-progress op
	progress chan string // progress lines from the in-progress op
	progLog  []string    // last few progress lines, newest last
	stats    StatsView   // cached Statistics-screen projection
	statsSel int         // Statistics cursor over selectable players (#2)
	alliesMode bool      // input screen is collecting allies (vs enemies)
	result  string
	width   int
	height  int
}

func newShell(cfg ShellConfig) shell {
	items := []list.Item{
		menuItem{id: "scan", title: "Scan draft", desc: "live pick/ban advisor (GSI or manual)", enabled: cfg.BuildScan != nil},
		menuItem{id: "scout", title: "Scout opponents", desc: "fetch + freeze opponent snapshot (layers 2-3)", enabled: cfg.Scout != nil},
		menuItem{id: "allies", title: "Scout allies", desc: "fetch our team's comfort heroes (personalizes picks)", enabled: cfg.ScoutAllies != nil},
		menuItem{id: "stats", title: "Statistics", desc: "view scouts, allies, hero pool; edit pool", enabled: cfg.Stats != nil},
		menuItem{id: "sync", title: "Sync data", desc: "pull CM matches into the local cache", enabled: cfg.Sync != nil},
		menuItem{id: "train", title: "Train model", desc: "train layer-1 models → bundle", enabled: cfg.Train != nil},
		menuItem{id: "quit", title: "Quit", desc: "exit", enabled: true},
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "advisor"
	l.SetShowStatusBar(false)
	l.SetShowHelp(true)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "player ids or profile URLs, space-separated"
	ti.CharLimit = 512

	ta := textarea.New()
	ta.Placeholder = "axe, lifestealer | ursa => buy Eul / Lotus\nelse => standard itemization"
	ta.ShowLineNumbers = true

	return shell{cfg: cfg, th: defaultTheme(), menu: l, spin: sp, input: ti, rules: ta}
}

// splitCommaTokens splits on commas and trims each token, dropping empties. Used
// for hero pools so multi-word names ("Crystal Maiden") survive.
func splitCommaTokens(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (s shell) Init() tea.Cmd {
	if s.cfg.Auto == nil {
		return nil
	}
	return autoTick()
}

// autoTickMsg drives the background collector's status line and the draft
// heartbeat. Bubbletea only redraws on a message, so without a tick the status
// would freeze at whatever it said when the user last pressed a key.
type autoTickMsg struct{}

// autoTickInterval is fast enough for a progress line to read as live and slow
// enough to be free; it is also well inside the worker's draft heartbeat TTL.
const autoTickInterval = 2 * time.Second

func autoTick() tea.Cmd {
	return tea.Tick(autoTickInterval, func(time.Time) tea.Msg { return autoTickMsg{} })
}

func (s shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case autoTickMsg:
		// While the draft screen is up, keep telling the worker so it defers the
		// retrain: applying a new bundle rebuilds the session and clears the board.
		if s.cfg.Auto != nil && s.screen == screenScan {
			s.cfg.Auto.TouchDraft()
		}
		return s, autoTick()

	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		s.menu.SetSize(msg.Width, msg.Height-2)
		// Forward to the scan sub-model only once it has been constructed; the
		// zero-value model has an uninitialized list that panics on SetSize.
		if s.scanInit {
			sm, _ := s.scan.Update(msg)
			s.scan = sm.(model)
		}
		return s, nil

	case spinner.TickMsg:
		if s.screen == screenRunning {
			var cmd tea.Cmd
			s.spin, cmd = s.spin.Update(msg)
			return s, cmd
		}
		return s, nil

	case opDoneMsg:
		s.screen = screenHome
		s.cancel = nil
		if msg.err != nil {
			s.result = s.th.dire.Render("error: " + msg.err.Error())
		} else {
			s.result = s.th.highlight.Render(msg.summary)
		}
		return s, nil

	case progressMsg:
		s.progLog = append(s.progLog, string(msg))
		if len(s.progLog) > 8 { // keep only the last few lines
			s.progLog = s.progLog[len(s.progLog)-8:]
		}
		return s, waitProgress(s.progress)
	}

	switch s.screen {
	case screenScan:
		return s.updateScan(msg)
	case screenScout, screenAllies:
		return s.updateScout(msg)
	case screenStats:
		return s.updateStats(msg)
	case screenPoolEdit:
		return s.updatePoolEdit(msg)
	case screenRulesEdit:
		return s.updateRulesEdit(msg)
	case screenRunning:
		// Allow cancelling/quitting a long op; ignore everything else.
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c", "q", "esc":
				if s.cancel != nil {
					s.cancel()
				}
				if key.String() == "ctrl+c" {
					return s, tea.Quit
				}
			}
		}
		return s, nil
	default:
		return s.updateHome(msg)
	}
}

func (s shell) updateHome(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "ctrl+c":
			return s, tea.Quit
		case "enter":
			it, ok := s.menu.SelectedItem().(menuItem)
			if !ok || !it.enabled {
				return s, nil
			}
			return s.activate(it.id)
		}
	}
	var cmd tea.Cmd
	s.menu, cmd = s.menu.Update(msg)
	return s, cmd
}

func (s shell) activate(id string) (tea.Model, tea.Cmd) {
	s.result = ""
	switch id {
	case "quit":
		return s, tea.Quit
	case "scan":
		cfg, err := s.cfg.BuildScan()
		if err != nil {
			s.result = s.th.dire.Render("cannot start scan: " + err.Error())
			return s, nil
		}
		s.scan = newModel(session.New(cfg))
		s.scanInit = true
		s.screen = screenScan
		// Prime the sub-model with the current size and its init cmd.
		var cmd tea.Cmd
		if s.width > 0 {
			sm, _ := s.scan.Update(tea.WindowSizeMsg{Width: s.width, Height: s.height})
			s.scan = sm.(model)
		}
		cmd = s.scan.Init()
		return s, cmd
	case "scout":
		s.alliesMode = false
		s.screen = screenScout
		s.input.Placeholder = "enemy ids or profile URLs, space-separated"
		s.input.SetValue("")
		s.input.Focus()
		return s, textinput.Blink
	case "allies":
		s.alliesMode = true
		s.screen = screenAllies
		s.input.Placeholder = "ally ids or profile URLs, space-separated"
		s.input.SetValue("")
		s.input.Focus()
		return s, textinput.Blink
	case "stats":
		if sv, err := s.cfg.Stats(); err != nil {
			s.result = s.th.dire.Render("cannot load stats: " + err.Error())
			return s, nil
		} else {
			s.stats = sv
		}
		s.statsSel = 0
		s.result = ""
		s.screen = screenStats
		return s, nil
	case "sync":
		return s.runOp("Syncing", func(ctx context.Context, p func(string)) (string, error) { return s.cfg.Sync(ctx, p) })
	case "train":
		return s.runOp("Training", func(ctx context.Context, p func(string)) (string, error) { return s.cfg.Train(ctx, p) })
	}
	return s, nil
}

func (s shell) runOp(label string, fn func(context.Context, func(string)) (string, error)) (tea.Model, tea.Cmd) {
	s.screen = screenRunning
	s.running = label
	s.progLog = nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	ch := make(chan string, 64)
	s.progress = ch
	// progress is non-blocking: if the UI lags behind, drop the line rather than
	// stall the worker.
	emit := func(line string) {
		select {
		case ch <- line:
		default:
		}
	}
	cmd := func() tea.Msg {
		summary, err := fn(ctx, emit)
		close(ch)
		return opDoneMsg{summary: summary, err: err}
	}
	return s, tea.Batch(cmd, s.spin.Tick, waitProgress(ch))
}

func (s shell) updateScout(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			s.screen = screenHome
			return s, nil
		case "enter":
			players := strings.Fields(s.input.Value())
			if len(players) == 0 {
				s.result = s.th.dim.Render("enter at least one player")
				return s, nil
			}
			if s.alliesMode {
				return s.runOp("Scouting allies", func(ctx context.Context, p func(string)) (string, error) {
					return s.cfg.ScoutAllies(ctx, players, p)
				})
			}
			return s.runOp("Scouting", func(ctx context.Context, p func(string)) (string, error) {
				return s.cfg.Scout(ctx, players, p)
			})
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

// updateStats handles the Statistics screen: launch scouts, edit the hero pool.
func (s shell) updateStats(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "q":
			s.screen = screenHome
			return s, nil
		case "up", "k":
			if s.statsSel > 0 {
				s.statsSel--
			}
			return s, nil
		case "down", "j":
			if s.statsSel < len(selectablePlayers(s.stats))-1 {
				s.statsSel++
			}
			return s, nil
		case "d", "x":
			if s.cfg.RemovePlayer == nil {
				return s, nil
			}
			sel := selectablePlayers(s.stats)
			if s.statsSel < 0 || s.statsSel >= len(sel) {
				return s, nil
			}
			p := sel[s.statsSel]
			summary, err := s.cfg.RemovePlayer(p.AccountID, p.Ally)
			if err != nil {
				s.result = s.th.dire.Render("remove: " + err.Error())
			} else {
				s.result = s.th.highlight.Render(summary)
			}
			if sv, e := s.cfg.Stats(); e == nil {
				s.stats = sv
			}
			if s.statsSel >= len(selectablePlayers(s.stats)) {
				s.statsSel = len(selectablePlayers(s.stats)) - 1
			}
			if s.statsSel < 0 {
				s.statsSel = 0
			}
			return s, nil
		case "c":
			if s.cfg.ClearLayer3 == nil {
				return s, nil
			}
			summary, err := s.cfg.ClearLayer3()
			if err != nil {
				s.result = s.th.dire.Render("clear layer 3: " + err.Error())
			} else {
				s.result = s.th.highlight.Render(summary)
			}
			if sv, e := s.cfg.Stats(); e == nil {
				s.stats = sv
			}
			return s, nil
		case "e":
			s.alliesMode = false
			s.screen = screenScout
			s.input.Placeholder = "enemy ids or profile URLs, space-separated"
			s.input.SetValue("")
			s.input.Focus()
			return s, textinput.Blink
		case "a":
			if s.cfg.ScoutAllies == nil {
				return s, nil
			}
			s.alliesMode = true
			s.screen = screenAllies
			s.input.Placeholder = "ally ids or profile URLs, space-separated"
			s.input.SetValue("")
			s.input.Focus()
			return s, textinput.Blink
		case "i":
			if s.cfg.SetItemRules == nil {
				return s, nil
			}
			s.screen = screenRulesEdit
			s.rules.SetValue(s.stats.ItemRules)
			if s.width > 0 {
				s.rules.SetWidth(s.width - 4)
				s.rules.SetHeight(s.height - 6)
			}
			s.rules.Focus()
			return s, textarea.Blink
		case "p":
			if s.cfg.SetHeroPool == nil {
				return s, nil
			}
			s.screen = screenPoolEdit
			s.input.Placeholder = "hero names or ids, comma-separated"
			// Pre-fill with the current pool names for easy editing.
			names := make([]string, 0, len(s.stats.HeroPool))
			for _, h := range s.stats.HeroPool {
				names = append(names, h.Name)
			}
			s.input.SetValue(strings.Join(names, " "))
			s.input.Focus()
			return s, textinput.Blink
		}
	}
	return s, nil
}

// updatePoolEdit handles editing the preferred hero pool (#4).
func (s shell) updatePoolEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			s.screen = screenStats
			return s, nil
		case "enter":
			tokens := splitCommaTokens(s.input.Value())
			summary, err := s.cfg.SetHeroPool(tokens)
			if err != nil {
				s.result = s.th.dire.Render("pool: " + err.Error())
			} else {
				s.result = s.th.highlight.Render(summary)
			}
			// Reload stats so the screen reflects the saved pool.
			if sv, e := s.cfg.Stats(); e == nil {
				s.stats = sv
			}
			s.screen = screenStats
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

// updateRulesEdit drives the multi-line advice-rule editor (#10).
func (s shell) updateRulesEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			s.rules.Blur()
			s.screen = screenStats
			return s, nil
		case "ctrl+s":
			summary, err := s.cfg.SetItemRules(s.rules.Value())
			if err != nil {
				s.result = s.th.dire.Render("rules: " + err.Error())
			} else {
				s.result = s.th.highlight.Render(summary)
			}
			if sv, e := s.cfg.Stats(); e == nil {
				s.stats = sv
			}
			s.rules.Blur()
			s.screen = screenStats
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.rules, cmd = s.rules.Update(msg)
	return s, cmd
}

func (s shell) viewRulesEdit() string {
	var b strings.Builder
	b.WriteString(s.th.title.Render("EDIT ADVICE RULES (#10)"))
	b.WriteString("\n")
	b.WriteString(s.th.dim.Render("syntax: hero,hero | hero => advice    •    else => fallback    •    # comment"))
	b.WriteString("\n\n")
	b.WriteString(s.rules.View())
	b.WriteString("\n\n")
	b.WriteString(s.th.dim.Render("ctrl+s: save   •   esc: cancel"))
	return b.String()
}

func (s shell) updateScan(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "esc" {
		// esc belongs to the sub-model when it has its own modal open: the help
		// overlay, the enemy-add input, or an active hero filter. Only when none of
		// those are open does esc mean "leave the draft".
		if s.scan.showHelp || s.scan.addingEnemy || s.scan.addingReason ||
			(!s.scan.gsiMode && s.scan.list.FilterState() != list.Unfiltered) {
			s.scanExitArmed = false
			sm, cmd := s.scan.Update(msg)
			s.scan = sm.(model)
			return s, cmd
		}
		// Accidental-exit guard: require a second esc to actually leave the draft.
		if !s.scanExitArmed {
			s.scanExitArmed = true
			s.scan.status = "press esc again to leave the draft (any other key cancels)"
			return s, nil
		}
		s.scanExitArmed = false
		s.screen = screenHome
		return s, nil
	}
	// Any other key disarms the exit guard.
	if _, ok := msg.(tea.KeyMsg); ok {
		s.scanExitArmed = false
	}
	sm, cmd := s.scan.Update(msg)
	s.scan = sm.(model)
	return s, cmd
}

func (s shell) View() string {
	switch s.screen {
	case screenScan:
		return s.scan.View() + "\n" + s.th.dim.Render("esc: back to menu")
	case screenScout, screenAllies:
		return s.viewScout()
	case screenStats:
		return s.viewStats()
	case screenPoolEdit:
		return s.viewPoolEdit()
	case screenRulesEdit:
		return s.viewRulesEdit()
	case screenRunning:
		var b strings.Builder
		fmt.Fprintf(&b, "\n  %s %s…\n\n", s.spin.View(), s.running)
		if len(s.progLog) == 0 {
			b.WriteString("  " + s.th.dim.Render("starting…") + "\n")
		}
		for _, line := range s.progLog {
			b.WriteString("  " + s.th.dim.Render(line) + "\n")
		}
		b.WriteString("\n  " + s.th.dim.Render("esc: cancel   •   ctrl+c: cancel & quit") + "\n")
		return b.String()
	default:
		return s.viewHome()
	}
}

func (s shell) viewHome() string {
	var b strings.Builder
	b.WriteString(s.menu.View())
	if line := s.autoLine(); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
	}
	if s.result != "" {
		b.WriteString("\n")
		b.WriteString(s.result)
	}
	return b.String()
}

// autoLine renders what the background collector is doing. It is the only sign
// the user has that data is still arriving, so it says which phase is running and
// how the last pass ended rather than a bare "working".
func (s shell) autoLine() string {
	if s.cfg.Auto == nil {
		return ""
	}
	st := s.cfg.Auto.Status()
	var text string
	switch st.Phase {
	case session.AutoSyncing, session.AutoTraining:
		text = fmt.Sprintf("auto %s: %s", st.Phase, firstNonEmpty(st.Line, "starting…"))
	case session.AutoPaused:
		text = "auto: paused for a manual operation"
	case session.AutoIdle:
		text = "auto: idle"
		if st.NextInSeconds > 0 {
			text = fmt.Sprintf("auto: next pass in %ds", st.NextInSeconds)
		}
		if st.Last != "" {
			text += " • last: " + st.Last
		}
	default:
		return ""
	}
	if st.Error != "" {
		return s.th.dire.Render("auto: " + st.Error)
	}
	return s.th.dim.Render(truncate(text, max(s.width-4, 20)))
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func (s shell) viewScout() string {
	var b strings.Builder
	who := "OPPONENTS"
	if s.alliesMode {
		who = "ALLIES"
	}
	b.WriteString(s.th.title.Render("SCOUT " + who))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Paste %s ids or profile URLs (OpenDota / STRATZ / Steam):\n\n", strings.ToLower(who)))
	b.WriteString(s.input.View())
	b.WriteString("\n\n")
	b.WriteString(s.th.dim.Render("enter: fetch & freeze   •   esc: back"))
	return s.th.border.Width(s.width - 2).Render(b.String())
}

func (s shell) viewPoolEdit() string {
	var b strings.Builder
	b.WriteString(s.th.title.Render("EDIT HERO POOL"))
	b.WriteString("\n\n")
	b.WriteString("Type preferred heroes (names or ids, comma-separated):\n\n")
	b.WriteString(s.input.View())
	b.WriteString("\n\n")
	b.WriteString(s.th.dim.Render("enter: save   •   esc: cancel"))
	return s.th.border.Width(s.width - 2).Render(b.String())
}
