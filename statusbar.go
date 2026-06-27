package main

import (
	"strings"

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
// refresh, lazy diff); background auto-refresh ticks stay silent. The middle
// centers the active "mine only" (involves:@me) scope as `@me` and/or the
// rate-limit backoff notice (shown together as `@me · <notice>` when both are
// active). The right side shows context-aware key hints. It is rendered in both
// the list and detail views.
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
		// The active "mine only" (involves:@me) scope is surfaced as `@me` centered
		// in the middle of the bar (see the gap fill below), so the left side shows
		// only the filter query.
	}

	hasLeft := left != ""
	left = leftStyle.Render(left)
	// Width of the left side BEFORE the transient loading indicator is prefixed.
	// The middle `@me`/notice keep-or-drop decision is gated on this base width so
	// the indicator can never flip it (see the drop guard below).
	baseLeftW := lipgloss.Width(left)

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

	// Fill the gap between the left text and the right-side hints with the
	// centered middle content. Two things can occupy the middle: the active "mine
	// only" (involves:@me) scope and, while the auto-refresh poll is backed off on
	// a low GraphQL budget, the rate-limit notice. When both are active they show
	// together as `@me · <notice>`; when only one is active that one is centered
	// alone; when neither is active the middle stays empty. The `@me` scope is
	// styled in the muted bar color so it reads as a quiet status, while the notice
	// keeps the diff-delete (red) color so it reads as an alert.
	//
	// The content is anchored to the terminal's TRUE center (m.width/2), not the
	// center of the (left-shrinking) gap — otherwise a growing filter query on the
	// left would push it rightward. It holds that fixed column until the query
	// grows long enough to reach it; at that point it is dropped whole (not pushed)
	// so the query takes the space. It is also dropped if it can't fit before the
	// right-side hints, so the bar never wraps past its single reserved line.
	middle := lipgloss.NewStyle().Width(gap).Render("")
	var parts []string
	if m.mineOnly {
		parts = append(parts, barStyle.Render(mineScopeLabel))
	}
	if notice := m.rateLimitNotice(); notice != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(diffDelColor).Render(notice))
	}
	if len(parts) > 0 {
		styled := strings.Join(parts, barStyle.Render(" · "))
		midW := lipgloss.Width(styled)
		// leftPad is the distance from the start of the gap region (which begins
		// right after `left`) to the content's fixed center column, computed from
		// the REAL left width (loading indicator included) so the content stays
		// anchored to the terminal's true center whether or not the indicator is
		// showing.
		leftPad := (m.width-midW)/2 - lipgloss.Width(left)
		// The keep-or-drop decision must NOT depend on the transient loading
		// indicator, otherwise `@me` would appear/disappear as loading toggles.
		// Gate the left-collision check on baseLeftPad (the distance using the
		// non-loading left width): a long filter query in the base left is a real
		// collision that still drops `@me` whole, but the indicator never flips it.
		// The right-side hint collision (leftPad+midW <= gap) is already
		// left-width-independent — width(left) cancels — so it ignores both.
		baseLeftPad := (m.width-midW)/2 - baseLeftW
		if baseLeftPad >= 1 && leftPad+midW <= gap {
			// Outside the narrow band where the indicator would physically overlap
			// the anchored content, leftPad >= 0 keeps `@me` pinned to the same
			// column with or without the indicator. Clamp at 0 inside that band so
			// the content shifts right by the overlap instead of panicking.
			pad := leftPad
			if pad < 0 {
				pad = 0
			}
			middle = lipgloss.NewStyle().Width(gap).Render(strings.Repeat(" ", pad) + styled)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, left, middle, hints)
}
