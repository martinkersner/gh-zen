package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// label is a single GitHub label on an issue/PR: its display name and the hex
// color GitHub returns (e.g. "d73a4a", no leading '#'), rendered directly as a
// chip in the detail view (see detailHeader).
type label struct {
	name  string
	color string
}

type item struct {
	number int
	title  string
	body   string
	type_  string  // "issue" or "pr"
	author string  // opener's login, populated by the list fetch and the detail fetch (fetchBody)
	labels []label // GitHub labels, populated by the detail fetch (fetchBody)
}

// FilterValue mirrors Title() so substring search matches both the issue/PR
// number and the title, and matched rune indexes line up with the rendered
// row for highlighting.
func (i item) FilterValue() string { return fmt.Sprintf("#%d %s", i.number, i.title) }
func (i item) Title() string       { return fmt.Sprintf("#%d %s", i.number, i.title) }
func (i item) Description() string { return "" }

// numberStyle is the distinct color applied to the "#<number>" prefix so it
// stands out from the title text. It is a package global (read live by Render)
// so rebuildThemeStyles can refresh it on a palette change; otherwise a theme
// switch would leave the list prefix in the original accent color.
var numberStyle lipgloss.Style

// authorStyle is the color applied to the right-aligned "@author" on each list
// row so it reads as distinct from the title text. Like numberStyle it is a
// package global (read live by Render) so rebuildThemeStyles refreshes it on a
// palette change. It currently shares the accent color with numberStyle but is
// kept separate so the author color can diverge from the number prefix later.
var authorStyle lipgloss.Style

// itemDelegate renders single-line items. Unlike list.DefaultDelegate it keeps
// the selected row highlighted while the filter input is active, so ctrl+n/
// ctrl+p are visible during search.
type itemDelegate struct {
	styles list.DefaultItemStyles
}

func newItemDelegate() itemDelegate {
	s := list.NewDefaultItemStyles()
	// Shrink the left gutter by 1 column so the whole app sits at column 1
	// instead of column 2. The selected row is marked by its accent foreground
	// alone — no left border, no vertical bar (issue #132). All three styles pad
	// left 1 with no border, so every row has horizontal frame size 1 and moving
	// the cursor causes no horizontal shift (the no-shift invariant from #122).
	s.NormalTitle = s.NormalTitle.PaddingLeft(1)
	s.DimmedTitle = s.DimmedTitle.PaddingLeft(1)
	// Drop the bubbles-default 1-col left border on the selected row (all four
	// sides off) and pad left 1 instead, so its frame stays size 1 to match the
	// normal/dimmed rows.
	s.SelectedTitle = s.SelectedTitle.
		Border(lipgloss.NormalBorder(), false, false, false, false).
		PaddingLeft(1)
	return itemDelegate{
		styles: s,
	}
}

// numberPrefixLen returns the rune count of the "#<number> " prefix (including
// the trailing space) for an item title of the form "#123 title". It is used to
// color just the number prefix in a distinct style. Returns 0 if the title does
// not start with the expected prefix.
func numberPrefixLen(title string) int {
	if !strings.HasPrefix(title, "#") {
		return 0
	}
	if sp := strings.IndexByte(title, ' '); sp >= 0 {
		return len([]rune(title[:sp+1]))
	}
	return len([]rune(title))
}

// renderTitle styles a single list row. The row's frame (left padding/border and
// base foreground) comes from rowStyle; the leading "#<number> " prefix is
// recolored with numberStyle so it stands out, and filter-matched runes get
// filterMatch layered on. Per-rune coloring is done on the inline body and then
// wrapped in a frame-only copy of rowStyle (foreground unset) so the embedded
// prefix/match colors survive instead of being flattened by an outer foreground.
//
// author (already truncated by the caller, empty to omit) is appended after the
// title, separated by pad blank columns so it sits right-aligned at the row edge,
// and colored with authorStyle so it reads as distinct from the title text.
func renderTitle(title string, prefixLen int, matches []int, isFiltered bool, rowStyle, filterMatch, numberStyle lipgloss.Style, author string, authorStyle lipgloss.Style, pad int) string {
	base := rowStyle.Inline(true)
	// number overrides the base foreground with the accent color; Inherit keeps
	// existing set fields, so clear the foreground first before inheriting it.
	number := base.UnsetForeground().Inherit(numberStyle)
	// matched layers the filter-match decoration (underline) on top of base; it
	// sets no foreground, so the row/number foreground shows through.
	matched := base.Inherit(filterMatch)
	if !isFiltered {
		matches = nil
	}

	matchSet := make(map[int]bool, len(matches))
	for _, idx := range matches {
		matchSet[idx] = true
	}
	// catFor categorizes a rune so consecutive runes sharing a style can be
	// rendered together: filter match wins, then the number prefix, then base.
	catFor := func(i int) int {
		switch {
		case matchSet[i]:
			return 2
		case i < prefixLen:
			return 1
		default:
			return 0
		}
	}
	styleFor := func(cat int) lipgloss.Style {
		switch cat {
		case 2:
			return matched
		case 1:
			return number
		default:
			return base
		}
	}
	runes := []rune(title)
	var b strings.Builder
	for i := 0; i < len(runes); {
		cat := catFor(i)
		j := i + 1
		for j < len(runes) && catFor(j) == cat {
			j++
		}
		b.WriteString(styleFor(cat).Render(string(runes[i:j])))
		i = j
	}
	if author != "" {
		// authoredStyle layers authorStyle's foreground over base, mirroring how
		// number is built, so the row frame's other fields survive but the author
		// gets its own color.
		authored := base.UnsetForeground().Inherit(authorStyle)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(authored.Render(author))
	}
	// Frame-only wrapper: keep padding/border from rowStyle but drop its
	// foreground so the per-rune colors above aren't overridden.
	frame := rowStyle.UnsetForeground()
	return frame.Render(b.String())
}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(item)
	if !ok || m.Width() <= 0 {
		return
	}
	s := &d.styles
	title := it.Title()

	isSelected := index == m.Index()
	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""
	isFiltered := m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied

	// Pick the row style first so the available text width is derived from that
	// style's own horizontal frame (padding + border). All three styles pad left
	// 1 with no border, so their horizontal frames match; using each style's
	// GetHorizontalFrameSize keeps long titles truncating at the same visible
	// column regardless of selection.
	rowStyle := s.NormalTitle
	switch {
	case emptyFilter:
		rowStyle = s.DimmedTitle
	case isSelected:
		rowStyle = s.SelectedTitle
	}
	textwidth := max(m.Width()-rowStyle.GetHorizontalFrameSize(), 0)

	// Right-align "@author" (issue #138-style attribution, now per list row). The
	// author always wins: the title is truncated first to leave room for a
	// rowAuthorGap-wide gap plus the author, so the author is never dropped — only
	// truncated itself if it alone would overflow the row. pad is the number of
	// blank columns between the (possibly truncated) title and the author, sized so
	// the author sits flush against the right edge.
	author := ""
	if it.author != "" {
		author = "@" + it.author
	}
	pad := 0
	if author != "" {
		authorWidth := ansi.StringWidth(author)
		avail := textwidth - authorWidth - rowAuthorGap
		if avail < 0 {
			// Author alone is wider than the row: drop the title and truncate the
			// author so the row still never overflows.
			author = ansi.Truncate(author, textwidth, "…")
			title = ""
		} else {
			title = ansi.Truncate(title, avail, "…")
		}
		pad = max(textwidth-ansi.StringWidth(title)-ansi.StringWidth(author), 0)
	} else {
		title = ansi.Truncate(title, textwidth, "…")
	}
	prefixLen := numberPrefixLen(title)

	switch {
	case emptyFilter:
		// Whole row dimmed (empty filter input); skip the number/author accent so
		// the row reads as inactive.
		content := title
		if author != "" {
			content += strings.Repeat(" ", pad) + author
		}
		title = s.DimmedTitle.Render(content)
	case isSelected:
		title = renderTitle(title, prefixLen, m.MatchesForItem(index), isFiltered, s.SelectedTitle, s.FilterMatch, numberStyle, author, authorStyle, pad)
	default:
		title = renderTitle(title, prefixLen, m.MatchesForItem(index), isFiltered, s.NormalTitle, s.FilterMatch, numberStyle, author, authorStyle, pad)
	}
	fmt.Fprint(w, title)
}
