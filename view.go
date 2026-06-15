package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	// The whole render reads the palette globals/derived styles (theme.go),
	// which applyPalette mutates from Update. Hold the read lock for the entire
	// render so a concurrent palette switch can't tear a color value mid-frame
	// (see paletteMu, issue #115). The reads are scattered across the render
	// tree, so locking at this single boundary is simpler and less error-prone
	// than locking each call site, and the lock is released as soon as the frame
	// string is built.
	paletteMu.RLock()
	defer paletteMu.RUnlock()

	if m.width == 0 {
		return ""
	}

	// The shortcuts overlay replaces the underlying view while open so it reads
	// as a focused, centered menu rather than text bleeding through.
	if m.showHelp {
		return m.renderHelp()
	}

	// The palette picker likewise replaces the underlying view while open.
	if m.showSettings {
		return m.renderSettings()
	}

	if m.detailOpen {
		return m.renderDetail()
	}

	return m.renderList()
}

func (m model) renderList() string {
	var b string

	// Tabs, followed by a blank line that separates navigation from content. The
	// list reserves two rows above it for this (see updateListSize); keep the two
	// in sync or the bottom item collides with the status bar.
	b += m.renderTabs() + "\n\n"

	// Errors still take over the body. Loading is surfaced solely by the
	// status-bar indicator (see renderStatusBar) and the tab "(?)" counts, so
	// the list stays visible during refreshes instead of being replaced by a
	// top-of-screen "Loading..." line.
	if m.err != nil {
		b += fmt.Sprintf("Error: %v\n", m.err)
	} else {
		b += m.currentList().View()
	}

	b += "\n" + m.renderStatusBar()

	return b
}

func (m model) renderDetail() string {
	if m.detailItem == nil {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Left, m.detailHeader(), m.detailViewport.View(), m.renderStatusBar())
}

// detailHeader renders the detail view's title block, width-constrained to the
// terminal width so a long "#<n> <title>" wraps deterministically. The viewport
// height is derived from lipgloss.Height of this block, so the full (wrapped)
// title stays visible at the top while the body scrolls below it.
func (m model) detailHeader() string {
	if m.detailItem == nil {
		return ""
	}
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(accentColor).
		// Indent to column 2 so the title lines up with list items
		// (NormalTitle PaddingLeft(2)) and the rest of the app.
		PaddingLeft(2).
		// One blank line below the title separates it from the body. This
		// adds a row to lipgloss.Height(detailHeader()), so the viewport
		// sizing (which measures the rendered header) stays correct.
		MarginBottom(1)
	// Constrain to the terminal width so a long title wraps deterministically;
	// skip the constraint before the first resize (width 0) to avoid clamping to
	// zero columns. lipgloss subtracts PaddingLeft from this Width, so the
	// rendered block (padding + content) still fits the terminal exactly.
	if m.width > 0 {
		titleStyle = titleStyle.Width(m.width)
	}
	return titleStyle.Render(fmt.Sprintf("#%d %s", m.detailItem.number, m.detailItem.title))
}
