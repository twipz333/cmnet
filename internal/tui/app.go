// Package tui is the bubbletea view layer (The Elm Architecture). It only renders
// what the deterministic core returns and never computes scores itself, so the UI
// can never be a source of divergence between machines (§6/§8).
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"cmnet/internal/draft"
	"cmnet/internal/objectives"
	"cmnet/internal/recommender"
	"cmnet/internal/session"
)

// Config is everything serve must supply to launch a front end. It is an alias for
// the session type: the TUI and the web UI are two views over the same state, and
// neither may own the vocabulary.
type Config = session.Config

type model struct {
	sess       *session.Session
	keys       keyMap
	th         theme
	help       help.Model
	list       list.Model
	enemyInput textinput.Model

	// Everything below the line is a read-only mirror of the session, refreshed by
	// sync() after each transition. The view code renders from these, so it can
	// never mutate state or recompute anything itself (§8).
	cfg     Config
	view    draft.View         // what the recommender reads (draft or latest snapshot)
	result  recommender.Result //
	live    *draft.Live        // latest in-game state from GSI (clock/phase), M6
	alerts  []objectives.Alert // upcoming objective alerts derived from live clock
	gsiSeen bool               // a GSI event has arrived at least once

	nameByID     map[int]string
	gsiMode      bool
	addingEnemy  bool // bottom enemy-add input is open (#3)
	busy         string
	themeIdx     int
	showHelp     bool
	showMatrices bool // synergy/counter overlay for the selected hero
	width        int
	height       int
	status       string
}

// addEnemyDoneMsg carries the result of a mid-draft enemy scout (#3).
type addEnemyDoneMsg struct {
	summary string
	err     error
}

// Run launches the TUI on a fresh session and blocks until the user quits.
func Run(cfg Config) error { return RunSession(session.New(cfg)) }

// RunSession launches the TUI over an existing session, for callers that build the
// session themselves (so the same state can be handed to another front end).
func RunSession(sess *session.Session) error {
	p := tea.NewProgram(newModel(sess), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(sess *session.Session) model {
	cfg := sess.Config()
	nameByID := make(map[int]string, len(cfg.Heroes))
	for _, h := range cfg.Heroes {
		nameByID[h.ID] = h.Label()
	}

	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Heroes"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)

	ei := textinput.New()
	ei.Placeholder = "enemy id or profile URL"
	ei.CharLimit = 256

	m := model{
		sess:       sess,
		keys:       defaultKeys(),
		th:         defaultTheme(),
		help:       help.New(),
		list:       l,
		enemyInput: ei,
		nameByID:   nameByID,
		gsiMode:    cfg.GSIEvents != nil,
	}
	m.sync()
	if !m.gsiMode {
		m.rebuildList()
	}
	return m
}

func (m model) Init() tea.Cmd {
	if m.gsiMode {
		return waitForGSI(m.cfg.GSIEvents)
	}
	return nil
}

// sync pulls the session's state into the model's read-only mirror and resizes the
// layout around it. Every transition goes through the session first and lands here,
// so the TUI renders exactly what any other front end would (§6/§8).
func (m *model) sync() {
	m.cfg = m.sess.Config()
	m.view = m.sess.View()
	m.result = m.sess.Result()
	m.live = m.sess.Live()
	m.alerts = m.sess.Alerts()
	m.gsiSeen = m.sess.GSISeen()
	m.relayout()
}

// relayout sizes the hero list to leave room for whatever optional bottom panels
// (pool, position meta, enemy-add input) are currently shown, so the frame fits
// the terminal instead of overflowing (#5).
func (m *model) relayout() {
	if m.width == 0 {
		return
	}
	reserve := 0
	if len(m.result.PoolAlert) > 0 {
		reserve += len(m.result.PoolAlert) + 3
	}
	if len(m.result.PositionMeta) > 0 {
		reserve += len(m.result.PositionMeta) + 3
	}
	if len(m.result.ItemAdvice) > 0 {
		reserve += len(m.result.ItemAdvice) + 3
	}
	if m.addingEnemy || m.busy != "" {
		reserve += 3
	}
	h := listHeight(m.height) - reserve
	if h < 4 {
		h = 4
	}
	m.list.SetSize(listWidth(m.width), h)
}

// rebuildList refreshes the hero picker to exclude already picked/banned heroes.
func (m *model) rebuildList() {
	avail := m.sess.AvailableHeroes()
	items := make([]list.Item, 0, len(avail))
	for _, h := range avail {
		items = append(items, heroItem{h: h})
	}
	m.list.SetItems(items)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.relayout()
		return m, nil

	case gsiMsg:
		// Read-only GSI mode: the session adopts the snapshot, recomputes and
		// refreshes the objective alerts; we only mirror it and ring the bell.
		if msg.snap != nil {
			ring := m.sess.ApplyGSI(msg.snap)
			m.sync()
			var bell tea.Cmd
			if ring {
				bell = beepCmd()
			}
			return m, tea.Batch(bell, waitForGSI(m.cfg.GSIEvents))
		}
		return m, waitForGSI(m.cfg.GSIEvents)

	case addEnemyDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = "add enemy: " + msg.err.Error()
		} else {
			m.status = msg.summary
		}
		m.sync()
		return m, nil

	case tea.KeyMsg:
		// The bottom enemy-add input (#3) captures keys while open.
		if m.addingEnemy {
			switch msg.String() {
			case "esc":
				m.addingEnemy = false
				m.enemyInput.Blur()
				m.relayout()
				return m, nil
			case "enter":
				url := strings.TrimSpace(m.enemyInput.Value())
				m.addingEnemy = false
				m.enemyInput.Blur()
				if url == "" {
					m.relayout()
					return m, nil
				}
				m.busy = "adding enemy…"
				m.relayout()
				return m, m.addEnemyCmd(url)
			default:
				var cmd tea.Cmd
				m.enemyInput, cmd = m.enemyInput.Update(msg)
				return m, cmd
			}
		}
		// Help overlay: esc (or ? again) closes it (#7).
		if m.showHelp {
			if msg.String() == "esc" || key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.Quit) {
				m.showHelp = false
			}
			return m, nil
		}
		// Matrices overlay: esc/v/q closes it; other keys browse the hero list so the
		// matchup panel follows the current selection (recomputed in View).
		if m.showMatrices {
			if msg.String() == "esc" || key.Matches(msg, m.keys.Matrices) || key.Matches(msg, m.keys.Quit) {
				m.showMatrices = false
				return m, nil
			}
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}
		// While the list filter is open, let the list consume keystrokes so the
		// user can type a query (q, ?, etc. must not trigger global actions).
		filtering := !m.gsiMode && m.list.FilterState() == list.Filtering
		switch {
		case !filtering && key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case !filtering && key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp
			return m, nil
		case !filtering && key.Matches(msg, m.keys.Theme):
			m.themeIdx = (m.themeIdx + 1) % themeCount()
			m.th = themeByIndex(m.themeIdx)
			return m, nil
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.Side):
			side := m.sess.ToggleSide()
			m.status = "our side → " + side.String()
			m.sync()
			return m, nil
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.FirstPick):
			nf, err := m.sess.ToggleFirstPick()
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.sync()
			m.rebuildList()
			m.status = "first-pick → " + nf.String() + " (draft reset)"
			return m, nil
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.Matrices):
			if m.sess.HasMatrices() {
				m.showMatrices = true
			} else {
				m.status = "no synergy/counter matrices in this bundle"
			}
			return m, nil
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.StepAction):
			if na, err := m.sess.ToggleStepAction(); err == nil {
				m.status = "current step → " + na.String()
				m.sync()
			}
			return m, nil
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.StepTeam):
			if opp, err := m.sess.ToggleStepTeam(); err == nil {
				m.status = "current step team → " + opp.String()
				m.sync()
			}
			return m, nil
		// Scouting an enemy does not touch the board, so it stays available while
		// GSI drives the draft — that is exactly when it is most useful.
		case !filtering && m.cfg.AddEnemy != nil && key.Matches(msg, m.keys.AddEnemy):
			m.addingEnemy = true
			m.enemyInput.SetValue("")
			m.enemyInput.Focus()
			m.relayout()
			return m, textinput.Blink
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.Undo):
			if err := m.sess.Undo(); err != nil {
				m.status = err.Error()
			} else {
				m.status = ""
				m.sync()
				m.rebuildList()
			}
			return m, nil
		case !m.gsiMode && !filtering && key.Matches(msg, m.keys.Redo):
			if err := m.sess.Redo(); err != nil {
				m.status = err.Error()
			} else {
				m.status = ""
				m.sync()
				m.rebuildList()
			}
			return m, nil
		case !m.gsiMode && key.Matches(msg, m.keys.Apply):
			if it, ok := m.list.SelectedItem().(heroItem); ok {
				if err := m.sess.Apply(it.h.ID); err != nil {
					m.status = err.Error()
				} else {
					m.status = ""
					// Clear the filter so the next step starts from the full list
					// instead of the stale query the user just typed.
					m.list.ResetFilter()
					m.sync()
					m.rebuildList()
				}
			}
			return m, nil
		}
	}

	// In GSI mode there is no hero picker to drive.
	if m.gsiMode {
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// addEnemyCmd scouts one enemy profile off the UI goroutine (#3). The session
// swaps in the rebuilt recommender itself, so the message only carries the outcome.
func (m model) addEnemyCmd(url string) tea.Cmd {
	sess := m.sess
	return func() tea.Msg {
		summary, err := sess.AddEnemy(url)
		return addEnemyDoneMsg{summary: summary, err: err}
	}
}

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.viewHelp()
	}
	if m.showMatrices {
		return m.viewMatrices()
	}
	return m.viewMain()
}
