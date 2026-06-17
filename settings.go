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

// rowAuthorGap is the minimum number of blank columns kept between a list row's
// title and the right-aligned "@author" so the two never visually touch when the
// title is truncated to make room for the author (see itemDelegate.Render).
const rowAuthorGap = 1

// loadingIndicator is the unobtrusive marker shown on the left of the status bar
// while a user-visible fetch is in flight (initial load, manual refresh, or a
// lazily-fetched detail body). A PR diff fetch uses the diff-specific label
// (loadingDiffIndicator) instead. It keeps activity visible without blanking
// content. Background auto-refresh ticks deliberately do not raise it
// (see refreshCurrentView) so the bar doesn't flicker every interval. Rendered
// dim/faint with no leading glyph so it reads as quiet, not attention-grabbing.
const loadingIndicator = "loading…"

// loadingDiffIndicator is the status-bar label shown specifically while the PR
// diff is being fetched (lazy first-toggle or background prefetch on detail
// entry). It replaces the old in-view "Loading diff..." placeholder: the diff
// sub-view is no longer blanked while loading; activity is surfaced only here.
const loadingDiffIndicator = "loading diff…"

// tickMsg is emitted by the auto-refresh ticker. Each tick triggers a refresh of
// the current view and re-arms the ticker.
type tickMsg time.Time

// tickCmd arms the auto-refresh ticker for one refreshInterval.
func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
