package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// detailNumberStyle colors the detail-header "#<number>" prefix in the palette's
// distinct "fun" Number color (issue #149), separate from the bold-accent title
// text. It is a package global (read live by detailHeader) so rebuildThemeStyles
// refreshes it on a palette switch, mirroring row.go's numberStyle.
var detailNumberStyle lipgloss.Style

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

	// The close-issue dialog likewise replaces the underlying view while open.
	if m.showCloseDialog {
		return m.renderCloseDialog()
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
	// The title block is a width-constrained, padded frame wrapping an inline
	// body. The body splits the "#<number>" prefix (rendered in the distinct
	// "fun" Number color, issue #149) from the title text (the bold accent), so
	// the number stands out instead of blending into the title. The frame carries
	// no foreground so the per-segment colors below survive (mirrors row.go's
	// frame-only wrapper). Width/padding live on the frame; the inline segments
	// must be Inline(true) so they don't re-pad.
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(accentColor).
		Inline(true)
	frameStyle := lipgloss.NewStyle().
		// Indent to column 1 so the title lines up with list items
		// (NormalTitle PaddingLeft(1)) and the rest of the app.
		PaddingLeft(1)
	// Constrain to the terminal width so a long title wraps deterministically;
	// skip the constraint before the first resize (width 0) to avoid clamping to
	// zero columns. lipgloss subtracts PaddingLeft from this Width, so the
	// rendered block (padding + content) still fits the terminal exactly.
	if m.width > 0 {
		frameStyle = frameStyle.Width(m.width)
	}
	numberPrefix := fmt.Sprintf("#%d", m.detailItem.number)
	titleBody := detailNumberStyle.Render(numberPrefix) + titleStyle.Render(" "+m.detailItem.title)
	title := frameStyle.Render(titleBody)

	// The header is a vertical stack of rows: the title and an optional metadata
	// row carrying the opener's login (left) and the label chips (right) on one
	// justified line (issue #153). The last row carries MarginBottom(1) so a
	// single blank separator line sits below the whole block (this row counts
	// toward lipgloss.Height(detailHeader()), keeping the viewport sizing
	// correct); all preceding rows carry no bottom margin.
	rows := []string{title}

	// The metadata row carries PaddingLeft(1), so its content budget is one column
	// narrower than the terminal. A non-positive budget (width 0 before the first
	// resize) disables clamping/justification (renderMetaRow then lays the
	// segments out at their natural width).
	rowWidth := 0
	if m.width > 0 {
		rowWidth = m.width - 1
	}
	if meta := renderMetaRow(m.detailItem.author, m.detailItem.labels, rowWidth); meta != "" {
		rows = append(rows, lipgloss.NewStyle().PaddingLeft(1).Render(meta))
	}

	// Apply the trailing blank-line separator to whichever row ends up last.
	last := len(rows) - 1
	rows[last] = lipgloss.NewStyle().MarginBottom(1).Render(rows[last])
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderMetaRow lays the opener's login and the label chips onto a single
// justified row for the detail header (issue #153): the "@author" is left-aligned
// and styled muted (so it reads as secondary to the bold accent title), the label
// chips are right-aligned, and the gap between them is filled with spaces so the
// chips sit one column shy of the right edge. Returns "" when both segments are
// empty so the header collapses to the title-only layout unchanged.
//
// width is the displayed-column budget for the row content (the caller adds
// PaddingLeft(1), so it passes m.width-1). A one-column gutter is reserved on the
// right so the right-most chip's trailing background padding never paints the
// terminal's final column (where it would bleed to the edge and read wider than
// the inner chips). The chips get whatever the author and a one-column gap leave
// of the remaining budget, so a long login can't push the chips off the row
// (renderLabelChips clamps them with a "+N" overflow marker). With only labels
// present they still right-align; with only an author present it sits at the left
// as before. A width <= 0 (no resize yet) disables justification and the chip
// clamp, rendering author then chips at their natural width.
func renderMetaRow(author string, labels []label, width int) string {
	left := ""
	if author != "" {
		left = lipgloss.NewStyle().Foreground(mutedColor).Render("@" + author)
	}

	// Reserve a one-column gutter on the right (mirroring the caller's
	// PaddingLeft(1)) so no chip paints the terminal's final column. Skip it when
	// width <= 0 (no resize yet), which already disables clamping/justification.
	if width > 0 {
		width--
	}

	// Chips get the budget left after the author and a single-column gap, so the
	// author always wins the space and the row never overflows.
	chipBudget := width
	if chipBudget > 0 && left != "" {
		chipBudget = max(chipBudget-lipgloss.Width(left)-1, 0)
	}
	right := renderLabelChips(labels, chipBudget)

	switch {
	case left == "" && right == "":
		return ""
	case right == "":
		// Author only: left-aligned, as the standalone author row was before.
		return left
	case left == "":
		// Labels only: right-align them within the budget.
		pad := width - lipgloss.Width(right)
		if pad < 1 {
			return right
		}
		return strings.Repeat(" ", pad) + right
	default:
		// Both present: author left, chips flush right, gap filled between. A
		// non-positive budget (or one too narrow to separate them) falls back to a
		// single-space join so nothing is dropped.
		pad := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
		return left + strings.Repeat(" ", pad) + right
	}
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
