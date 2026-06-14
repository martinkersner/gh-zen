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

type item struct {
	number int
	title  string
	body   string
	type_  string // "issue" or "pr"
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

// itemDelegate renders single-line items. Unlike list.DefaultDelegate it keeps
// the selected row highlighted while the filter input is active, so ctrl+n/
// ctrl+p are visible during search.
type itemDelegate struct {
	styles list.DefaultItemStyles
}

func newItemDelegate() itemDelegate {
	return itemDelegate{
		styles: list.NewDefaultItemStyles(),
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
func renderTitle(title string, prefixLen int, matches []int, isFiltered bool, rowStyle, filterMatch, numberStyle lipgloss.Style) string {
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
	// style's own horizontal frame (padding + border). SelectedTitle pads 1 but
	// adds a 1-col left border, while NormalTitle pads 2 with no border; using
	// each style's GetHorizontalFrameSize keeps long titles truncating at the
	// same visible column regardless of selection.
	rowStyle := s.NormalTitle
	switch {
	case emptyFilter:
		rowStyle = s.DimmedTitle
	case isSelected:
		rowStyle = s.SelectedTitle
	}
	textwidth := m.Width() - rowStyle.GetHorizontalFrameSize()
	title = ansi.Truncate(title, textwidth, "…")
	prefixLen := numberPrefixLen(title)

	switch {
	case emptyFilter:
		// Whole row dimmed (empty filter input); skip the number accent so the
		// row reads as inactive.
		title = s.DimmedTitle.Render(title)
	case isSelected:
		title = renderTitle(title, prefixLen, m.MatchesForItem(index), isFiltered, s.SelectedTitle, s.FilterMatch, numberStyle)
	default:
		title = renderTitle(title, prefixLen, m.MatchesForItem(index), isFiltered, s.NormalTitle, s.FilterMatch, numberStyle)
	}
	fmt.Fprint(w, title)
}
