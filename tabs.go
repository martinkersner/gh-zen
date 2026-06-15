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

// tabCount returns the number of items fetched for the given tab.
func (m model) tabCount(t tab) int {
	switch t {
	case tabPRs:
		return len(m.prList.Items())
	default:
		return len(m.issueList.Items())
	}
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
