package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshInterval is the single source of truth for how often the current view
// auto-refreshes. Kept as a named constant here (a Go settings file) rather than
// hardcoded inline; a user-editable config is a possible follow-up.
const refreshInterval = 5 * time.Second

// statusBarHeight is the number of rows reserved at the bottom of the screen for
// the persistent status bar. The per-list and detail viewport height calcs
// subtract this so the bar never overlaps scrollable content.
const statusBarHeight = 1

// tickMsg is emitted by the auto-refresh ticker. Each tick triggers a refresh of
// the current view and re-arms the ticker.
type tickMsg time.Time

// tickCmd arms the auto-refresh ticker for one refreshInterval.
func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
