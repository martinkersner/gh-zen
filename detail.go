package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// detailViewportSize computes the width/height for the detail body viewport from
// the terminal size, reserving headerHeight lines for the (possibly wrapped)
// title plus statusBarHeight for the bottom status bar. headerHeight is the
// measured rendered height of the title block (see detailHeader); pass
// detailHeaderHeight when only the width return is needed. Heights/widths are
// clamped to a minimum of 1 so tiny terminals don't produce negative dimensions.
func detailViewportSize(width, height, headerHeight int) (int, int) {
	w := width - 2
	if w < 1 {
		w = 1
	}
	h := height - headerHeight - statusBarHeight
	if h < 1 {
		h = 1
	}
	return w, h
}

// composeDetailBody folds an issue/PR body and its conversation comments into a
// single markdown document that flows through the same render/search/cache path
// as a bare body. Comments are appended under a "Comments" section, each as a
// bold author attribution followed by the comment body, in the order GitHub
// returned them (chronological), separated by horizontal rules. Folding into one
// markdown string (rather than carrying structured comments through the model)
// keeps the existing bodyMsg/detailBody/bodyCache lifecycle and in-detail search
// untouched; bold authors give clear per-comment attribution within that.
//
// With no comments it returns the body verbatim so the empty-state and
// "Loading body..." handling in detailWrappedLines stay exactly as before.
func composeDetailBody(body string, comments []comment) string {
	if len(comments) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n## Comments\n")
	for i, c := range comments {
		author := c.author
		if author == "" {
			author = "(unknown)"
		}
		// A horizontal rule separates consecutive comments; the first sits flush
		// under the section heading (no leading rule).
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(fmt.Sprintf("\n**@%s**\n\n", author))
		b.WriteString(c.body)
		b.WriteString("\n")
	}
	return b.String()
}

// detailMatchStyle / detailActiveMatchStyle style search hits in the detail
// body: every match gets the muted highlight, the current (active) match a
// brighter one so it stands out as ctrl+n/ctrl+p step through occurrences. They
// are rebuilt by rebuildThemeStyles (see theme.go) on a palette change.
var (
	detailMatchStyle       lipgloss.Style
	detailActiveMatchStyle lipgloss.Style
)

// detailWrappedLines returns the detail body rendered as styled terminal
// markdown (GitHub-flavored: headings, lists, code fences, emphasis, links)
// wrapped to the viewport width, split into individual lines. The returned
// lines contain glamour's ANSI escapes and are the single source of truth for
// what the viewport displays; detailPlainLines projects them back to visible
// text so search match offsets stay aligned with this output (see findMatches).
//
// The "Loading body..." placeholder and an empty body are wrapped with lipgloss
// rather than run through markdown rendering, so they render as plain text.
func (m model) detailWrappedLines() []string {
	w, _ := detailViewportSize(m.width, m.height, detailHeaderHeight)
	if m.detailLoading {
		wrapped := lipgloss.NewStyle().Width(w).Render("Loading body...")
		return strings.Split(wrapped, "\n")
	}
	if strings.TrimSpace(m.detailBody) == "" {
		wrapped := lipgloss.NewStyle().Width(w).Render(m.detailBody)
		return strings.Split(wrapped, "\n")
	}
	return strings.Split(renderMarkdown(m.detailBody, w), "\n")
}

// detailPlainLines returns the visible-text projection of detailWrappedLines:
// the same lines with glamour's ANSI escapes stripped. Search matches are
// computed over these so their line/column (rune) offsets line up with what
// highlightStyledLine re-styles on the styled lines.
func (m model) detailPlainLines() []string {
	styled := m.detailWrappedLines()
	plain := make([]string, len(styled))
	for i, line := range styled {
		plain[i] = ansi.Strip(line)
	}
	return plain
}

// detailBodyContent returns the viewport content for the detail body. When an
// in-detail search query is active, every match is highlighted and the active
// match is styled distinctly; otherwise the wrapped body is returned verbatim.
func (m model) detailBodyContent() string {
	lines := m.detailWrappedLines()
	if len(m.detailMatches) == 0 {
		return strings.Join(lines, "\n")
	}
	// Per-line, mark which rune columns belong to the active match vs. any
	// other match, then highlight each line in a single pass.
	type lineHL struct {
		active map[int]bool
		other  map[int]bool
	}
	byLine := make(map[int]*lineHL)
	for i, mt := range m.detailMatches {
		hl := byLine[mt.line]
		if hl == nil {
			hl = &lineHL{active: map[int]bool{}, other: map[int]bool{}}
			byLine[mt.line] = hl
		}
		set := hl.other
		if i == m.detailActiveMatch {
			set = hl.active
		}
		for k := 0; k < mt.length; k++ {
			set[mt.startCol+k] = true
		}
	}
	for li, hl := range byLine {
		if li < 0 || li >= len(lines) {
			continue
		}
		// lines contain glamour's ANSI escapes, so overlay the match styling
		// with the ANSI-aware highlighter (column offsets come from the plain
		// projection findMatches ran against).
		lines[li] = highlightStyledLine(lines[li], hl.active, hl.other)
	}
	return strings.Join(lines, "\n")
}

// enterDetailSearch starts in-detail search mode with an empty query.
func (m *model) enterDetailSearch() {
	m.detailSearching = true
	m.detailQuery = ""
	m.detailMatches = nil
	m.detailActiveMatch = 0
}

// exitDetailSearch leaves search mode and clears the query/highlight, restoring
// the plain body. The viewport scroll position is preserved.
func (m *model) exitDetailSearch() {
	m.detailSearching = false
	m.detailQuery = ""
	m.detailMatches = nil
	m.detailActiveMatch = 0
	offset := m.detailViewport.YOffset
	m.detailViewport.SetContent(m.detailBodyContent())
	m.detailViewport.SetYOffset(offset)
}

// refreshDetailSearch recomputes matches for the current query against the
// wrapped body, resets the active match to the first hit, re-renders the
// highlighted content, and scrolls to the active match so the user sees a hit as
// they type.
func (m *model) refreshDetailSearch() {
	m.detailMatches = findMatches(m.detailPlainLines(), m.detailQuery)
	m.detailActiveMatch = 0
	offset := m.detailViewport.YOffset
	m.detailViewport.SetContent(m.detailBodyContent())
	if len(m.detailMatches) == 0 {
		m.detailViewport.SetYOffset(offset)
		return
	}
	m.scrollToActiveMatch()
}

// jumpDetailMatch advances the active match to the next (forward) or previous
// match, wrapping around, re-renders so the new active match is styled, and
// scrolls it into view. A no-op when there are no matches.
func (m *model) jumpDetailMatch(forward bool) {
	n := len(m.detailMatches)
	if n == 0 {
		return
	}
	if forward {
		m.detailActiveMatch = nextMatchIndex(m.detailActiveMatch, n)
	} else {
		m.detailActiveMatch = prevMatchIndex(m.detailActiveMatch, n)
	}
	m.detailViewport.SetContent(m.detailBodyContent())
	m.scrollToActiveMatch()
}

// scrollToActiveMatch scrolls the viewport just enough to bring the active
// match's line into view, leaving the offset unchanged when it's already
// visible.
func (m *model) scrollToActiveMatch() {
	if m.detailActiveMatch < 0 || m.detailActiveMatch >= len(m.detailMatches) {
		return
	}
	line := m.detailMatches[m.detailActiveMatch].line
	maxOffset := m.detailViewport.TotalLineCount() - m.detailViewport.Height
	offset := scrollOffsetFor(line, m.detailViewport.YOffset, m.detailViewport.Height, maxOffset)
	m.detailViewport.SetYOffset(offset)
}

// detailContent returns the content currently shown in the detail viewport:
// the PR diff when the diff sub-view is toggled on, otherwise the body.
func (m model) detailContent() string {
	if m.detailShowDiff {
		return m.detailDiffContent()
	}
	return m.detailBodyContent()
}

// detailDiffContent returns the PR diff sub-view content. While loading the view
// is left blank (no placeholder) — the in-flight fetch is surfaced in the status
// bar instead (see renderStatusBar / loadingDiffIndicator). On a fetch error it
// shows the stored error text. When the changed-files overview is toggled on it
// shows that pane. Otherwise it renders the parsed diff in the active layout
// (unified or side-by-side). The file-header line offsets used by file
// navigation are produced as a side effect by the renderer and are not
// recomputed here — see refreshDiffView.
func (m model) detailDiffContent() string {
	w, _ := detailViewportSize(m.width, m.height, detailHeaderHeight)
	if m.detailDiffLoading {
		return ""
	}
	if m.detailDiffErr != nil {
		return lipgloss.NewStyle().Width(w).Render(m.detailDiffErrText())
	}
	if m.detailShowOverview {
		return renderFileOverview(m.detailFiles, m.detailActiveFile, w)
	}
	content, _ := m.renderDiffStructure(w)
	return content
}

// renderDiffStructure renders the parsed diff in the active layout (unified or
// side-by-side) and returns the content plus the per-file header line offsets
// from the same render pass, so callers that need both (refreshDiffView) don't
// render twice. When there is no parsed structure (empty/unrecognized diff) it
// returns the plain colorized fallback and nil offsets.
func (m model) renderDiffStructure(width int) (string, []int) {
	if len(m.detailFiles) == 0 {
		// No parsed structure (empty diff or unrecognized format); fall back to
		// the plain colorized text so nothing is silently dropped.
		return colorizeDiff(m.detailDiff), nil
	}
	if m.detailSplitView {
		return renderSideBySide(m.detailFiles, width)
	}
	return renderUnified(m.detailFiles, width)
}

// diffFileOffsets returns the file-header line offsets for the current diff
// layout, recomputed against the current viewport width. Used by file
// navigation to scroll the viewport to a file's header. Returns nil when not in
// a structured diff view (loading, error, overview, or unparsed).
func (m model) diffFileOffsets() []int {
	if m.detailDiffLoading || m.detailShowOverview || len(m.detailFiles) == 0 {
		return nil
	}
	if m.detailDiffErr != nil {
		return nil
	}
	w, _ := detailViewportSize(m.width, m.height, detailHeaderHeight)
	_, offsets := m.renderDiffStructure(w)
	return offsets
}

// setDetailDiff stores a freshly delivered diff and parses it into per-file
// structure for the diff sub-view, resetting the active-file cursor to the
// first file.
func (m *model) setDetailDiff(diff string) {
	m.detailDiff = diff
	m.detailDiffErr = nil
	m.detailFiles = parseDiff(diff)
	m.detailActiveFile = 0
}

// detailDiffErrText is the user-facing message shown in place of the diff when a
// fetch failed. Empty when there is no error.
func (m model) detailDiffErrText() string {
	if m.detailDiffErr == nil {
		return ""
	}
	return fmt.Sprintf("Error loading diff: %v", m.detailDiffErr)
}

// refreshDiffView re-renders the diff sub-view content into the viewport and
// recomputes the file-header offsets for the current layout/width in a single
// render pass. Scroll position is preserved. While loading the view is left
// blank (the in-flight fetch shows in the status bar, not here); for the
// error/overview cases (no structured diff) it renders the message/pane. All of
// these clear the file-header offsets.
func (m *model) refreshDiffView() {
	offset := m.detailViewport.YOffset
	w, _ := detailViewportSize(m.width, m.height, detailHeaderHeight)
	switch {
	case m.detailDiffLoading:
		m.detailFileOffsets = nil
		m.detailViewport.SetContent("")
	case m.detailDiffErr != nil:
		m.detailFileOffsets = nil
		m.detailViewport.SetContent(lipgloss.NewStyle().Width(w).Render(m.detailDiffErrText()))
	case m.detailShowOverview:
		m.detailFileOffsets = nil
		m.detailViewport.SetContent(renderFileOverview(m.detailFiles, m.detailActiveFile, w))
	default:
		content, offsets := m.renderDiffStructure(w)
		m.detailFileOffsets = offsets
		m.detailViewport.SetContent(content)
	}
	m.detailViewport.SetYOffset(offset)
}

// jumpToFile scrolls the diff viewport so the file at index i's header is at the
// top, clamping i into range and updating the active-file cursor (so the
// overview highlight follows). A no-op when there are no file offsets.
func (m *model) jumpToFile(i int) {
	n := len(m.detailFileOffsets)
	if n == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	m.detailActiveFile = i
	maxOffset := m.detailViewport.TotalLineCount() - m.detailViewport.Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	off := m.detailFileOffsets[i]
	if off > maxOffset {
		off = maxOffset
	}
	m.detailViewport.SetYOffset(off)
}

// openDetailViewport sizes the detail viewport, loads the current body, and
// anchors it at the top so the title is always visible when a detail view opens.
func (m *model) openDetailViewport() {
	w, h := detailViewportSize(m.width, m.height, lipgloss.Height(m.detailHeader()))
	m.detailViewport = viewport.New(w, h)
	// Add j/k as scroll aliases alongside the default arrow/pgup/pgdn keys.
	m.detailViewport.KeyMap.Up.SetKeys(append(m.detailViewport.KeyMap.Up.Keys(), "k")...)
	m.detailViewport.KeyMap.Down.SetKeys(append(m.detailViewport.KeyMap.Down.Keys(), "j")...)
	m.detailViewport.SetContent(m.detailContent())
	m.detailViewport.GotoTop()
}

// resizeDetailViewport updates the viewport dimensions and re-wraps its content
// after a terminal resize while the detail view is open.
func (m *model) resizeDetailViewport() {
	w, h := detailViewportSize(m.width, m.height, lipgloss.Height(m.detailHeader()))
	m.detailViewport.Width = w
	m.detailViewport.Height = h
	// Re-wrapping at the new width moves match line/column offsets, so recompute
	// them before re-rendering the highlighted content.
	m.recomputeDetailMatches()
	// The diff layout (and thus file-header offsets, plus the unified fallback
	// threshold for the split view) depends on width, so recompute offsets too.
	if m.detailShowDiff {
		m.detailFileOffsets = m.diffFileOffsets()
	}
	m.detailViewport.SetContent(m.detailContent())
}

// recomputeDetailMatches re-locates the current query's matches against the
// freshly wrapped body and clamps the active match index to the new count. A
// no-op when not searching. Used after the body or wrap width changes so the
// highlight stays aligned with what's rendered.
func (m *model) recomputeDetailMatches() {
	if !m.detailSearching || m.detailQuery == "" {
		m.detailMatches = nil
		m.detailActiveMatch = 0
		return
	}
	m.detailMatches = findMatches(m.detailPlainLines(), m.detailQuery)
	if m.detailActiveMatch >= len(m.detailMatches) {
		m.detailActiveMatch = 0
	}
}
