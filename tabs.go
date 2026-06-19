package main

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

func (m model) renderTabs() string {
	var tabs []string
	for i, t := range m.tabs {
		style := lipgloss.NewStyle().Padding(0, 1)
		if tab(i) == m.activeTab {
			style = style.Foreground(accentColor).Bold(true)
		} else {
			style = style.Foreground(mutedColor)
		}
		label := fmt.Sprintf("%s (%s)", t, m.tabCountLabel(tab(i)))
		tabs = append(tabs, style.Render(label))
	}
	// The tab style's own padding-left of 1 already starts the first tab's text
	// at column 1, aligning with the list items below (NormalTitle padding-left
	// is 1). No extra left pad is needed.
	return lipgloss.JoinHorizontal(lipgloss.Left, tabs...)
}

// tabCount returns the count shown in a tab's brackets: the repo's true open
// total (connection totalCount) when known, which can exceed the fetched item
// count since the query caps at 50. Falls back to the fetched length when the
// total is unknown (zero) — e.g. before the first fetch or in tests that build
// a dataMsg without totals — so the count never under-reports what's on screen.
func (m model) tabCount(t tab) int {
	var total, fetched int
	switch t {
	case tabPRs:
		total, fetched = m.prTotal, len(m.prList.Items())
	default:
		total, fetched = m.issueTotal, len(m.issueList.Items())
	}
	if total > fetched {
		return total
	}
	return fetched
}

// tabCountLabel renders the bracket contents for a tab: "?" while the initial
// fetch is still in flight (the count is unknown, not zero), otherwise the real
// count — including "0" once a fetch genuinely returns no items.
func (m model) tabCountLabel(t tab) string {
	if m.loading {
		return "?"
	}
	return strconv.Itoa(m.tabCount(t))
}
