package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds the application key bindings, surfaced in the help overlay.
type keyMap struct {
	Apply     key.Binding
	Undo      key.Binding
	Redo      key.Binding
	Filter    key.Binding
	Theme     key.Binding
	Side       key.Binding
	FirstPick  key.Binding
	StepAction key.Binding
	StepTeam   key.Binding
	Matrices   key.Binding
	AddEnemy   key.Binding
	Reason     key.Binding
	Outcome    key.Binding
	Help       key.Binding
	Quit       key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Apply: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "apply hero to current step"),
		),
		Undo: key.NewBinding(
			key.WithKeys("ctrl+z", "backspace"),
			key.WithHelp("ctrl+z", "undo last step"),
		),
		Redo: key.NewBinding(
			key.WithKeys("ctrl+y"),
			key.WithHelp("ctrl+y", "redo undone step"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter heroes"),
		),
		Theme: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "cycle theme"),
		),
		Side: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "swap our side (radiant/dire)"),
		),
		FirstPick: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "swap first-pick team (resets draft)"),
		),
		StepAction: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "toggle current step pick/ban"),
		),
		StepTeam: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "toggle current step team"),
		),
		Matrices: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "synergy/counter for selected hero"),
		),
		AddEnemy: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add enemy profile to scout"),
		),
		// §6.1: the reason a recommendation was refused is the one thing no amount of
		// corpus mining produces, so it gets a first-class key rather than a menu.
		Reason: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "why the last pick went against the advice"),
		),
		// The outcome closes the record out: without it a logged draft says what was
		// advised and chosen but never how it went, which is half a record.
		Outcome: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "record the result of this draft (w/l)"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Apply, k.Undo, k.StepAction, k.StepTeam, k.Matrices, k.Side, k.FirstPick, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Apply, k.Undo, k.Redo},
		{k.StepAction, k.StepTeam},
		{k.Filter, k.Theme, k.Matrices},
		{k.Side, k.FirstPick, k.AddEnemy},
		{k.Reason, k.Outcome},
		{k.Help, k.Quit},
	}
}
