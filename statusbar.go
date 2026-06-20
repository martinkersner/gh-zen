package main

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderStatusBar renders the one-line bar pinned to the bottom of the screen.
// In the list view the left side shows the active filter query when filtering
// (otherwise it is empty — the mode is conveyed by the tabs row above); in the
// detail view it shows the item kind. While a user-visible fetch is in flight
// the left side is prefixed with a dim loading indicator (see loadingIndicator)
// so activity stays visible even when the body is already populated (manual
// refresh, lazy diff); background auto-refresh ticks stay silent. The right side
// shows context-aware key hints. It is rendered in both the list and detail
// views.
func (m model) renderStatusBar() string {
	// Every status-bar element shares one uniform color (the muted dim gray
	// formerly used only for the `? help` hint) so the bar reads as a single
	// quiet line rather than a mix of bold blue + dim gray.
	barStyle := lipgloss.NewStyle().Foreground(mutedColor)
	leftStyle := barStyle
	hintStyle := barStyle
	loadingStyle := barStyle

	// The full shortcut list now lives in the `?` overlay (see renderHelp); the
	// bar shows only the compact hint so it stays uncluttered — identical in
	// every view/mode, including while searching.
	left, hints := "", helpHint
	if m.detailOpen {
		kind := "Issue"
		if m.detailItem != nil && m.detailItem.type_ == "pr" {
			kind = "Pull Request"
		}
		left = kind
		// In-detail search renders identically to the list filter: the shared
		// searchBarLeft helper is the single source of truth for the `/ <q>`
		// display, so there is no per-view query format.
		if m.detailSearching {
			left = searchBarLeft(m.detailQuery, true)
		}
	} else {
		// The bare mode word ("Issues"/"PRs") is redundant with the tabs row
		// (renderTabs) directly above, which already shows `Issues (N)` / `PRs (N)`
		// and highlights the active one — so the left side stays empty unless a
		// filter is active.
		// Surface the filter query so the user can see what they typed. While
		// typing (Filtering) the list's built-in input is hidden, so render the
		// live, editable value here even when empty — that makes the bar the one
		// place the filter lives, both during typing and once applied.
		cur := m.currentList()
		switch cur.FilterState() {
		case list.Filtering:
			left = searchBarLeft(cur.FilterValue(), true)
		case list.FilterApplied:
			if q := cur.FilterValue(); q != "" {
				left = searchBarLeft(q, false)
			}
		}
	}

	hasLeft := left != ""
	left = leftStyle.Render(left)

	// While a user-visible fetch is in flight (initial load, manual refresh, or a
	// lazily-fetched detail body / PR diff) surface a dim indicator on the left
	// rather than relying solely on the body placeholder — this also covers a
	// manual refresh where the body is already populated. Background auto-refresh
	// ticks do not set these flags (see refreshCurrentView), so the bar stays
	// quiet on every interval. The indicator clears automatically once the
	// loading flags are reset on completion or error.
	if m.loading || m.detailLoading || m.detailDiffLoading {
		// Label a diff fetch distinctly ("loading diff…") since that is the only
		// feedback now that the diff sub-view is not blanked with a placeholder.
		// A generic body/list fetch in flight takes precedence over the diff label
		// when both are set.
		label := loadingIndicator
		if m.detailDiffLoading && !m.loading && !m.detailLoading {
			label = loadingDiffIndicator
		}
		indicator := loadingStyle.Render(label)
		if hasLeft {
			left = indicator + leftStyle.Render(" · ") + left
		} else {
			left = indicator
		}
	}

	hints = hintStyle.Render(hints)

	// Lay the hints out flush-right, padding the gap to the terminal width. When
	// the two halves don't fit, join them with a single space and truncate to
	// the terminal width so the bar never wraps onto a second row (which would
	// overflow the single reserved status-bar line).
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(hints)
	if gap < 1 {
		bar := lipgloss.JoinHorizontal(lipgloss.Left, left, " ", hints)
		return ansi.Truncate(bar, m.width, "…")
	}

	// Fill the gap between the left text and the right-side hints. While the
	// auto-refresh poll is backed off on a low GraphQL budget, center the notice
	// in that gap so it reads as the middle of the bar; if it can't fit, fall back
	// to an empty spacer so the bar never wraps past its single reserved line. The
	// notice uses the diff-delete (red) color so it reads as an alert against the
	// otherwise-muted bar.
	middle := lipgloss.NewStyle().Width(gap).Render("")
	if notice := m.rateLimitNotice(); notice != "" {
		styled := lipgloss.NewStyle().Foreground(diffDelColor).Render(notice)
		if lipgloss.Width(styled) <= gap {
			middle = lipgloss.PlaceHorizontal(gap, lipgloss.Center, styled)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, left, middle, hints)
}
