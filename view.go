package main

import (
	"fmt"
	"strconv"
	"strings"

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
		// Underline the title on top of the accent foreground so it reads as the
		// view's heading and stays visually distinct from the body — both from
		// glamour's gray paragraph text and from any leading markdown heading in
		// the body, which glamour also renders bold-and-colored (issue #137).
		Underline(true).
		Foreground(accentColor).
		// Indent to column 1 so the title lines up with list items
		// (NormalTitle PaddingLeft(1)) and the rest of the app.
		PaddingLeft(1).
		// One blank line below the title separates it from the body. This
		// adds a row to lipgloss.Height(detailHeader()), so the viewport
		// sizing (which measures the rendered header) stays correct.
		MarginBottom(1)
	// Constrain to the terminal width so a long title wraps deterministically;
	// skip the constraint before the first resize (width 0) to avoid clamping to
	// zero columns. lipgloss subtracts PaddingLeft from this Width, so the
	// rendered block (padding + content) still fits the terminal exactly.
	//
	// When the item has no labels the bottom margin is kept on the title so the
	// rendered height (and thus viewport sizing) is unchanged from before. With
	// labels the margin moves to the chip row so the blank separator line still
	// sits below the whole block, and the chips occupy the row between.
	if m.width > 0 {
		titleStyle = titleStyle.Width(m.width)
	}
	title := fmt.Sprintf("#%d %s", m.detailItem.number, m.detailItem.title)
	// The chip row carries PaddingLeft(1), so its content budget is one column
	// narrower than the terminal. A non-positive budget (width 0 before the first
	// resize) disables the clamp in renderLabelChips.
	chipBudget := 0
	if m.width > 0 {
		chipBudget = m.width - 1
	}
	chips := renderLabelChips(m.detailItem.labels, chipBudget)
	if chips == "" {
		return titleStyle.Render(title)
	}
	titleStyle = titleStyle.MarginBottom(0)
	chipRow := lipgloss.NewStyle().PaddingLeft(1).MarginBottom(1).Render(chips)
	return lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), chipRow)
}

// renderLabelChips renders a row of colored chips, one per label, joined by a
// single space. Each chip uses the label's GitHub hex color as its background
// with a black/white foreground picked for contrast (mirroring GitHub's own
// luminance-based choice). Returns "" when there are no labels so the detail
// header collapses to the title-only layout unchanged.
//
// maxWidth is the displayed-column budget for the chip row content (the caller
// adds PaddingLeft(1), so it must pass m.width-1). Chips are emitted in order
// until the next chip (plus its leading space) would exceed the budget; the
// remaining labels are summarized by a trailing "+N" overflow marker that is
// itself fitted within the budget. This keeps the row within the terminal
// width with no mid-chip wrapping or right-edge overflow. A maxWidth <= 0 (no
// resize yet) disables the clamp and renders every chip, preserving prior
// behavior.
func renderLabelChips(labels []label, maxWidth int) string {
	if len(labels) == 0 {
		return ""
	}

	rendered := make([]string, 0, len(labels))
	for _, l := range labels {
		style := lipgloss.NewStyle().Padding(0, 1)
		if c := normalizeHexColor(l.color); c != "" {
			style = style.Background(lipgloss.Color(c)).Foreground(lipgloss.Color(labelTextColor(c)))
		}
		rendered = append(rendered, style.Render(l.name))
	}

	if maxWidth <= 0 {
		return strings.Join(rendered, " ")
	}

	// Greedily pack chips until the next one (plus the joining space) would
	// overflow the budget, reserving room for a "+N" marker for the rest.
	used := 0 // displayed width consumed so far
	kept := rendered[:0:0]
	for i, chip := range rendered {
		w := lipgloss.Width(chip)
		sep := 0
		if i > 0 {
			sep = 1 // single space between chips
		}
		// Width still needed for a "+N" marker covering everything from i on.
		overflowW := 0
		if i < len(rendered) {
			marker := overflowChip(len(rendered) - i)
			overflowW = 1 + lipgloss.Width(marker) // space + marker
		}
		// Last chip needs no trailing overflow marker.
		reserve := overflowW
		if i == len(rendered)-1 {
			reserve = 0
		}
		if used+sep+w+reserve > maxWidth {
			break
		}
		used += sep + w
		kept = append(kept, chip)
	}

	if len(kept) == len(rendered) {
		return strings.Join(kept, " ")
	}

	dropped := len(rendered) - len(kept)
	marker := overflowChip(dropped)
	if len(kept) == 0 {
		// Not even one chip fits alongside a marker. Show the marker alone if it
		// fits, otherwise show the first chip (better a single overflowing chip
		// than nothing); the title block above is the width-of-record clamp.
		if lipgloss.Width(marker) <= maxWidth {
			return marker
		}
		return rendered[0]
	}
	return strings.Join(kept, " ") + " " + marker
}

// overflowChip renders the "+N" marker shown when n trailing label chips are
// dropped to keep the row within the terminal width. It uses the same padding
// as a real chip so it lines up with the row.
func overflowChip(n int) string {
	return lipgloss.NewStyle().Padding(0, 1).Render(fmt.Sprintf("+%d", n))
}

// normalizeHexColor returns a "#rrggbb" lipgloss color string from a GitHub
// label color (a 6-hex-digit string with no leading '#', e.g. "d73a4a"),
// tolerating an optional leading '#'. Returns "" for anything that isn't a
// valid 6-digit hex so the caller falls back to an uncolored chip rather than
// emitting a malformed escape.
func normalizeHexColor(c string) string {
	c = strings.TrimPrefix(c, "#")
	if len(c) != 6 {
		return ""
	}
	if _, err := strconv.ParseUint(c, 16, 32); err != nil {
		return ""
	}
	return "#" + c
}

// labelTextColor picks a readable foreground (black or white) for a chip whose
// background is the given "#rrggbb" color, using a perceived-luminance
// threshold so text stays legible on both light and dark labels (the same
// heuristic GitHub applies). The input is assumed valid (from normalizeHexColor).
func labelTextColor(hex string) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		return "#ffffff"
	}
	r := float64((v >> 16) & 0xff)
	g := float64((v >> 8) & 0xff)
	b := float64(v & 0xff)
	// Rec. 601 perceived luminance; >= 128 reads as a light background.
	if 0.299*r+0.587*g+0.114*b >= 128 {
		return "#000000"
	}
	return "#ffffff"
}
