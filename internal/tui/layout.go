package tui

// Layout helpers. The hero picker takes a fixed-ish left column; the rest of the
// width is split between the recommendation and enemy-prediction panels.

const (
	minListWidth = 24
	maxListWidth = 36
	statusRows   = 1
)

func listWidth(total int) int {
	w := total / 4
	if w < minListWidth {
		w = minListWidth
	}
	if w > maxListWidth {
		w = maxListWidth
	}
	if w > total-10 {
		w = total - 10
	}
	return w
}

func listHeight(total int) int {
	// leave room for the draft board (top) and status bar (bottom).
	h := total - boardRows - statusRows - 2
	if h < 5 {
		h = 5
	}
	return h
}

// boardRows is the rendered height budget of the draft board panel.
const boardRows = 6
