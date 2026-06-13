package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

type tab int

const (
	tabIssues tab = iota
	tabPRs
)

// detailHeaderHeight is the minimum number of lines the detail view reserves
// above the scrollable body (a single-line title). The actual reserved height is
// measured from the rendered header via lipgloss.Height, since a long title can
// wrap to multiple rows on a narrow terminal; this constant is the fallback used
// by call sites that only need the viewport width.
const detailHeaderHeight = 1

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

// itemDelegate renders single-line items. Unlike list.DefaultDelegate it keeps
// the selected row highlighted while the filter input is active, so ctrl+n/
// ctrl+p are visible during search.
type itemDelegate struct {
	styles list.DefaultItemStyles
	// number is the distinct color applied to the "#<number>" prefix so it
	// stands out from the title text in both normal and selected rows.
	number lipgloss.Style
}

func newItemDelegate() itemDelegate {
	return itemDelegate{
		styles: list.NewDefaultItemStyles(),
		number: lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Inline(true),
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
	textwidth := m.Width() - s.NormalTitle.GetPaddingLeft() - s.NormalTitle.GetPaddingRight()
	title = ansi.Truncate(title, textwidth, "…")
	prefixLen := numberPrefixLen(title)

	isSelected := index == m.Index()
	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""
	isFiltered := m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied

	switch {
	case emptyFilter:
		// Whole row dimmed (empty filter input); skip the number accent so the
		// row reads as inactive.
		title = s.DimmedTitle.Render(title)
	case isSelected:
		title = renderTitle(title, prefixLen, m.MatchesForItem(index), isFiltered, s.SelectedTitle, s.FilterMatch, d.number)
	default:
		title = renderTitle(title, prefixLen, m.MatchesForItem(index), isFiltered, s.NormalTitle, s.FilterMatch, d.number)
	}
	fmt.Fprint(w, title)
}

type model struct {
	width  int
	height int

	tabs      []string
	activeTab tab

	// One list per tab, persisted so selection & scroll are remembered
	issueList list.Model
	prList    list.Model

	// Detail pane
	detailOpen     bool
	detailLoading  bool
	detailItem     *item
	detailBody     string
	detailViewport viewport.Model

	// Diff sub-view of the detail pane (PRs only). detailShowDiff toggles the
	// viewport between the body and the PR's diff; detailDiffLoading mirrors
	// detailLoading for the lazily-fetched diff.
	detailShowDiff    bool
	detailDiff        string
	detailDiffLoading bool

	// In-detail search (editor-style find within the open detail body).
	// detailSearching is true while the user is typing/navigating a query;
	// detailQuery is the live query; detailMatches are all occurrences in the
	// wrapped body in reading order; detailActiveMatch indexes the highlighted
	// (current) match within detailMatches.
	detailSearching   bool
	detailQuery       string
	detailMatches     []searchMatch
	detailActiveMatch int

	// Cache: "issue_42" or "pr_7" -> body
	bodyCache map[string]string
	// Cache: "pr_7" -> diff text (PRs only)
	diffCache map[string]string

	// showHelp toggles the keyboard-shortcuts overlay. When set, View renders
	// the help overlay (see renderHelp) over the current view; `?` or esc closes
	// it. It reflects the shortcuts of whichever view (list/detail) is active.
	showHelp bool

	// Async state
	loading bool
	err     error
}

func newModel() model {
	m := model{
		tabs:      []string{"Issues", "PRs"},
		activeTab: tabIssues,
		loading:   true,
		bodyCache: make(map[string]string),
		diffCache: make(map[string]string),
		issueList: list.New([]list.Item{}, newItemDelegate(), 0, 0),
		prList:    list.New([]list.Item{}, newItemDelegate(), 0, 0),
	}
	// Use strict case-insensitive substring matching instead of the default
	// fuzzy (subsequence) filter.
	m.issueList.Filter = substringFilter
	m.prList.Filter = substringFilter
	m.issueList.SetShowHelp(false)
	m.prList.SetShowHelp(false)
	m.issueList.SetShowTitle(false)
	m.prList.SetShowTitle(false)
	m.issueList.SetShowStatusBar(false)
	m.prList.SetShowStatusBar(false)
	// Filtering stays enabled (so `/` works and items filter), but the list's
	// built-in filter input/prompt line above the list is hidden; the live
	// filter is rendered in the bottom status bar instead (see renderStatusBar).
	m.issueList.SetShowFilter(false)
	m.prList.SetShowFilter(false)

	// Indent the empty-state ("No items.") text to column 2 so it aligns with
	// the tab labels and the list item titles above it (the bubbles default
	// NoItems style has no PaddingLeft). Preserve the default subdued color.
	m.issueList.Styles.NoItems = m.issueList.Styles.NoItems.PaddingLeft(2)
	m.prList.Styles.NoItems = m.prList.Styles.NoItems.PaddingLeft(2)

	// Move with ctrl+n / ctrl+p (and arrows); drop j/k.
	up := key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("ctrl+p", "up"))
	down := key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("ctrl+n", "down"))
	for _, l := range []*list.Model{&m.issueList, &m.prList} {
		l.KeyMap.CursorUp = up
		l.KeyMap.CursorDown = down
	}
	return m
}

func (m *model) currentList() *list.Model {
	if m.activeTab == tabPRs {
		return &m.prList
	}
	return &m.issueList
}

// refreshCurrentView re-fetches whatever the user is currently looking at: the
// focused item's body when the detail view is open, otherwise the issues/PRs
// lists. Selection and scroll position are preserved by the respective message
// handlers (dataMsg restores the list index; bodyMsg keeps the viewport offset
// on a same-item refresh).
//
// It sets the matching loading flag so the status-bar indicator (see
// renderStatusBar) reflects in-flight background fetches even though the body
// stays populated; the flag is cleared by the corresponding message handler on
// completion or error. Pointer receiver so the flag set persists in the caller.
func (m *model) refreshCurrentView() tea.Cmd {
	if m.detailOpen && m.detailItem != nil {
		// In the PR diff sub-view, refresh the diff rather than the body so the
		// visible content is what actually gets updated.
		if m.detailShowDiff {
			m.detailDiffLoading = true
			return m.cmdFetchDiff(m.detailItem)
		}
		m.detailLoading = true
		return m.cmdFetchBody(m.detailItem)
	}
	m.loading = true
	return fetchIssuesAndPRs()
}

func (m *model) setListItems(tabIdx tab, items []list.Item) {
	switch tabIdx {
	case tabIssues:
		applyListItems(&m.issueList, items)
	case tabPRs:
		applyListItems(&m.prList, items)
	}
}

// applyListItems replaces a list's items and, when a filter is active,
// recomputes the filtered set synchronously. list.Model.SetItems returns a
// tea.Cmd that produces a FilterMatchesMsg to refresh filteredItems; on a
// refresh (dataMsg) that cmd would otherwise be discarded, leaving the visible
// list stale and VisibleItems() empty until the next keystroke. Running the cmd
// inline and feeding its message back through the list applies the new filtered
// set immediately, so the visible list reflects the new items and a subsequent
// restoreIndex clamps against the correct visible count.
func applyListItems(l *list.Model, items []list.Item) {
	cmd := l.SetItems(items)
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	updated, _ := l.Update(msg)
	*l = updated
}

// restoreIndex re-selects idx on l, clamped to the current item count, so a
// refresh that replaced the items keeps the cursor where it was (or on the last
// item if the list shrank). A negative/empty result is left at 0. The bound uses
// the visible (filtered) item count, matching the index space Index()/Select()
// operate in, so it stays correct when a filter is applied.
func restoreIndex(l *list.Model, idx int) {
	n := len(l.VisibleItems())
	if n == 0 {
		return
	}
	if idx >= n {
		idx = n - 1
	}
	if idx < 0 {
		idx = 0
	}
	l.Select(idx)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchIssuesAndPRs(), tickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateListSize()
		if m.detailOpen {
			m.resizeDetailViewport()
		}

	case tea.KeyMsg:
		// The help overlay captures keys while open: `?` or esc closes it,
		// ctrl+c still quits, and every other key is swallowed so it can't
		// act on the obscured view underneath.
		if m.showHelp {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "?", "esc", "ctrl+g":
				m.showHelp = false
			}
			return m, nil
		}

		if m.detailOpen {
			// ctrl+c always quits, even mid-search.
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}

			// In-detail search mode: typing builds the query, esc exits and
			// clears the highlight, and ctrl+n/ctrl+p jump between matches
			// instead of scrolling one line.
			if m.detailSearching {
				switch msg.Type {
				case tea.KeyEsc, tea.KeyCtrlG:
					m.exitDetailSearch()
					return m, nil
				case tea.KeyCtrlN:
					m.jumpDetailMatch(true)
					return m, nil
				case tea.KeyCtrlP:
					m.jumpDetailMatch(false)
					return m, nil
				case tea.KeyBackspace:
					if r := []rune(m.detailQuery); len(r) > 0 {
						m.detailQuery = string(r[:len(r)-1])
						m.refreshDetailSearch()
					}
					return m, nil
				case tea.KeyRunes, tea.KeySpace:
					m.detailQuery += string(msg.Runes)
					m.refreshDetailSearch()
					return m, nil
				}
				// Any other key (arrows, pgup/pgdn) still scrolls the viewport
				// while keeping the search query and highlight intact.
				var cmd tea.Cmd
				m.detailViewport, cmd = m.detailViewport.Update(msg)
				return m, cmd
			}

			switch msg.String() {
			case "?":
				m.showHelp = true
				return m, nil
			case "esc", "q", "ctrl+g":
				m.detailOpen = false
				m.detailItem = nil
				m.detailShowDiff = false
				// Clear any in-flight detail/diff loading so the status-bar
				// indicator doesn't stick: once the item is nil the bodyMsg/diffMsg
				// handlers' cacheKey guard no longer matches and would never reset
				// these flags.
				m.detailLoading = false
				m.detailDiffLoading = false
				m.exitDetailSearch()
				return m, nil
			case "/":
				m.enterDetailSearch()
				return m, nil
			case "ctrl+n":
				m.detailViewport.ScrollDown(1)
				return m, nil
			case "ctrl+p":
				m.detailViewport.ScrollUp(1)
				return m, nil
			case "d":
				// Toggle between the body and the PR diff. No-op for issues,
				// which have no diff. Lazy-fetch the diff on first toggle.
				if m.detailItem != nil && m.detailItem.type_ == "pr" {
					return m.toggleDiff()
				}
				return m, nil
			case "r":
				return m, m.refreshCurrentView()
			}
			// Forward scroll keys (arrows, pgup/pgdn, j/k) to the viewport.
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}

		// While typing in the filter input, let the list consume every key
		// (except ctrl+c) so they don't trigger quit / tab-switch / detail.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.currentList().SettingFilter() {
			// Allow moving the selection without leaving the filter input.
			switch msg.String() {
			case "ctrl+n":
				m.currentList().CursorDown()
				return m, nil
			case "ctrl+p":
				m.currentList().CursorUp()
				return m, nil
			case "ctrl+g":
				// Cancel the filter, mirroring esc. The list cancels on esc, so
				// forward a synthetic esc rather than typing a literal ctrl+g.
				cur := m.currentList()
				updated, cmd := cur.Update(tea.KeyMsg{Type: tea.KeyEsc})
				*cur = updated
				return m, cmd
			}
			break
		}

		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "q", "ctrl+c", "ctrl+g":
			return m, tea.Quit
		case "r":
			return m, m.refreshCurrentView()
		case "tab", "l", "right":
			m.activeTab = (m.activeTab + 1) % tab(len(m.tabs))
			m.updateListSize()
		case "shift+tab", "h", "left":
			m.activeTab = (m.activeTab - 1 + tab(len(m.tabs))) % tab(len(m.tabs))
			m.updateListSize()
		case "enter":
			it := m.selectedItem()
			if it != nil {
				m.detailOpen = true
				m.detailItem = it
				m.detailShowDiff = false
				m.detailBody = m.cachedBody(it)
				m.detailLoading = m.detailBody == ""
				m.openDetailViewport()
				if m.detailLoading {
					return m, m.cmdFetchBody(it)
				}
			}
		}

	case dataMsg:
		m.loading = false
		m.err = nil
		// Preserve the cursor position across refreshes: capture each list's
		// index before repopulating, then restore it clamped to the new length
		// so an auto/manual refresh doesn't jump the selection back to the top.
		issueIdx := m.issueList.Index()
		prIdx := m.prList.Index()
		m.setListItems(tabIssues, msg.issues)
		m.setListItems(tabPRs, msg.prs)
		restoreIndex(&m.issueList, issueIdx)
		restoreIndex(&m.prList, prIdx)
		m.updateListSize()

		// Prefetch first 5 bodies for the active tab
		src := msg.issues
		if m.activeTab == tabPRs {
			src = msg.prs
		}
		for i, it := range src {
			if i >= 5 {
				break
			}
			ii := it.(item)
			if _, ok := m.bodyCache[cacheKey(&ii)]; !ok {
				cmds = append(cmds, m.cmdPrefetchBody(&ii))
			}
		}

	case bodyMsg:
		if msg.err != nil {
			// A failed body fetch (e.g. background refresh) keeps the cached
			// content rather than blanking the view.
			if m.detailOpen && m.detailItem != nil && cacheKey(m.detailItem) == msg.key {
				m.detailLoading = false
			}
			break
		}
		m.bodyCache[msg.key] = msg.body
		if m.detailOpen && m.detailItem != nil && cacheKey(m.detailItem) == msg.key {
			// Preserve the current scroll position across a refresh so the user
			// isn't yanked back to the top; SetYOffset clamps to the new content.
			offset := m.detailViewport.YOffset
			m.detailBody = m.cachedBody(m.detailItem)
			m.detailLoading = false
			if !m.detailShowDiff {
				// Re-locate matches against the (possibly changed) body so the
				// highlight stays valid; clamp the active match if the count shrank.
				m.recomputeDetailMatches()
				m.detailViewport.SetContent(m.detailContent())
				m.detailViewport.SetYOffset(offset)
			}
		}

	case diffMsg:
		if msg.err != nil {
			// A failed diff fetch clears the loading state; if the diff view is
			// open it falls back to showing the fetch error in place of the diff.
			if m.detailOpen && m.detailItem != nil && cacheKey(m.detailItem) == msg.key {
				m.detailDiffLoading = false
				if m.detailShowDiff {
					m.detailDiff = fmt.Sprintf("Error loading diff: %v", msg.err)
					m.detailViewport.SetContent(m.detailContent())
				}
			}
			break
		}
		m.diffCache[msg.key] = msg.diff
		if m.detailOpen && m.detailItem != nil && cacheKey(m.detailItem) == msg.key {
			m.detailDiff = msg.diff
			m.detailDiffLoading = false
			if m.detailShowDiff {
				// Preserve scroll position so an auto/manual refresh of an already
				// open diff doesn't yank the viewport back to the top. On the first
				// toggle the offset is already 0, so this still opens at the top.
				offset := m.detailViewport.YOffset
				m.detailViewport.SetContent(m.detailContent())
				m.detailViewport.SetYOffset(offset)
			}
		}

	case errMsg:
		m.loading = false
		m.err = msg.err

	case tickMsg:
		// Auto-refresh the current view, then re-arm the ticker. Skip the data
		// fetch while the user is mid-filter so the list isn't reshuffled under
		// them; the ticker still keeps running.
		if m.detailOpen || !m.currentList().SettingFilter() {
			cmds = append(cmds, m.refreshCurrentView())
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)
	}

	// Pass through to active list
	if !m.detailOpen {
		cur := m.currentList()
		newListModel, cmd := cur.Update(msg)
		*cur = newListModel
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) updateListSize() {
	// Reserve the tabs row above the list plus the status bar row pinned to the
	// bottom, so neither overlaps the scrollable list. The list now renders
	// directly under the tabs (no blank line), so only one row of chrome sits
	// above it.
	listHeight := m.height - 1 - statusBarHeight
	m.issueList.SetSize(m.width, listHeight)
	m.prList.SetSize(m.width, listHeight)
}

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

// detailMatchStyle / detailActiveMatchStyle style search hits in the detail
// body: every match gets the muted highlight, the current (active) match a
// brighter one so it stands out as ctrl+n/ctrl+p step through occurrences.
var (
	detailMatchStyle       = lipgloss.NewStyle().Background(lipgloss.Color("#3b4261")).Foreground(lipgloss.Color("#c0caf5"))
	detailActiveMatchStyle = lipgloss.NewStyle().Background(lipgloss.Color("#e0af68")).Foreground(lipgloss.Color("#1a1b26")).Bold(true)
)

// detailWrappedLines returns the detail body (or a loading placeholder) wrapped
// to the viewport width, split into individual lines. The same wrapping is used
// both to render the viewport content and to locate search matches, so match
// line/column offsets line up exactly with what's displayed.
func (m model) detailWrappedLines() []string {
	body := m.detailBody
	if m.detailLoading {
		body = "Loading body..."
	}
	w, _ := detailViewportSize(m.width, m.height, detailHeaderHeight)
	wrapped := lipgloss.NewStyle().Width(w).Render(body)
	return strings.Split(wrapped, "\n")
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
		lines[li] = highlightLine(lines[li], hl.active, hl.other)
	}
	return strings.Join(lines, "\n")
}

// highlightLine renders line styling the runes in active with the active-match
// style and those in other with the normal-match style, in a single pass.
// Doing it in one pass (rather than two lipgloss.StyleRunes calls) avoids
// re-indexing into a string that already contains ANSI escapes, which would
// misalign the second set of indices once the first call inserted styling.
func highlightLine(line string, active, other map[int]bool) string {
	var b strings.Builder
	for i, r := range []rune(line) {
		switch {
		case active[i]:
			b.WriteString(detailActiveMatchStyle.Render(string(r)))
		case other[i]:
			b.WriteString(detailMatchStyle.Render(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
	m.detailMatches = findMatches(m.detailWrappedLines(), m.detailQuery)
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

// detailDiffContent returns the PR diff (or a loading placeholder) with added/
// removed lines colored, wrapped to the viewport width.
func (m model) detailDiffContent() string {
	if m.detailDiffLoading {
		w, _ := detailViewportSize(m.width, m.height, detailHeaderHeight)
		return lipgloss.NewStyle().Width(w).Render("Loading diff...")
	}
	return colorizeDiff(m.detailDiff)
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
	m.detailMatches = findMatches(m.detailWrappedLines(), m.detailQuery)
	if m.detailActiveMatch >= len(m.detailMatches) {
		m.detailActiveMatch = 0
	}
}

func (m model) selectedItem() *item {
	cur := m.currentList()
	if i, ok := cur.SelectedItem().(item); ok {
		return &i
	}
	return nil
}

func (m model) cachedBody(it *item) string {
	if it == nil {
		return ""
	}
	if body, ok := m.bodyCache[cacheKey(it)]; ok && body != "" {
		return body
	}
	if it.body != "" {
		return it.body
	}
	return ""
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	// The shortcuts overlay replaces the underlying view while open so it reads
	// as a focused, centered menu rather than text bleeding through.
	if m.showHelp {
		return m.renderHelp()
	}

	if m.detailOpen {
		return m.renderDetail()
	}

	return m.renderList()
}

func (m model) renderList() string {
	var b string

	// Tabs
	b += m.renderTabs() + "\n"

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

// renderStatusBar renders the one-line bar pinned to the bottom of the screen.
// In the list view the left side shows the active filter query when filtering
// (otherwise it is empty — the mode is conveyed by the tabs row above); in the
// detail view it shows the item kind. While a fetch is in flight the left side
// is prefixed with a loading indicator (see loadingIndicator) so activity stays
// visible even when the body is already populated (refresh, lazy diff). The
// right side shows context-aware key hints. It is rendered in both the list and
// detail views.
func (m model) renderStatusBar() string {
	leftStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))

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

	// While any fetch is in flight (initial load, refresh, or a lazily-fetched
	// detail body / PR diff) surface an unobtrusive indicator on the left rather
	// than relying solely on the body placeholder — this also covers background
	// refreshes where the body is already populated. It clears automatically once
	// the loading flags are reset on completion or error.
	if m.loading || m.detailLoading || m.detailDiffLoading {
		if left == "" {
			left = loadingIndicator
		} else {
			left = loadingIndicator + " · " + left
		}
	}

	left = leftStyle.Render(left)
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
	return lipgloss.JoinHorizontal(lipgloss.Left, left, lipgloss.NewStyle().Width(gap).Render(""), hints)
}

func (m model) renderTabs() string {
	var tabs []string
	for i, t := range m.tabs {
		style := lipgloss.NewStyle().Padding(0, 1)
		if tab(i) == m.activeTab {
			style = style.Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
		} else {
			style = style.Foreground(lipgloss.Color("#565f89"))
		}
		label := fmt.Sprintf("%s (%s)", t, m.tabCountLabel(tab(i)))
		tabs = append(tabs, style.Render(label))
	}
	// Extra left pad of 1 so the first tab's text starts at column 2, aligning
	// with the list items below (NormalTitle padding-left is 2; the tab style's
	// own padding-left is only 1).
	return lipgloss.NewStyle().PaddingLeft(1).Render(lipgloss.JoinHorizontal(lipgloss.Left, tabs...))
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
		Foreground(lipgloss.Color("#7aa2f7"))
	// Constrain to the terminal width so a long title wraps deterministically;
	// skip the constraint before the first resize (width 0) to avoid clamping to
	// zero columns.
	if m.width > 0 {
		titleStyle = titleStyle.Width(m.width)
	}
	return titleStyle.Render(fmt.Sprintf("#%d %s", m.detailItem.number, m.detailItem.title))
}

func cacheKey(it *item) string {
	return fmt.Sprintf("%s_%d", it.type_, it.number)
}

func (m model) cmdPrefetchBody(it *item) tea.Cmd {
	return func() tea.Msg {
		return bodyMsg{key: cacheKey(it), body: it.body}
	}
}

// cmdFetchBody re-fetches the focused item's content (the body) from GitHub. PR
// diffs are fetched separately (cmdFetchDiff) since they aren't part of the
// GraphQL body query and are only pulled when the diff sub-view is opened.
func (m model) cmdFetchBody(it *item) tea.Cmd {
	key := cacheKey(it)
	number := it.number
	isPR := it.type_ == "pr"
	return func() tea.Msg {
		body, err := fetchBody(number, isPR)
		if err != nil {
			return bodyMsg{key: key, err: err}
		}
		return bodyMsg{key: key, body: body}
	}
}

// toggleDiff flips the detail viewport between the body and the PR diff for the
// focused PR. The diff is lazily fetched and cached on first view (mirroring the
// body cache); subsequent toggles reuse the cache. The viewport is re-anchored
// at the top so the start of the newly-shown content is visible.
func (m model) toggleDiff() (tea.Model, tea.Cmd) {
	m.detailShowDiff = !m.detailShowDiff
	if !m.detailShowDiff {
		m.detailViewport.SetContent(m.detailContent())
		m.detailViewport.GotoTop()
		return m, nil
	}
	// Showing the diff: use the cache if present, otherwise fetch it.
	if cached, ok := m.diffCache[cacheKey(m.detailItem)]; ok {
		m.detailDiff = cached
		m.detailDiffLoading = false
		m.detailViewport.SetContent(m.detailContent())
		m.detailViewport.GotoTop()
		return m, nil
	}
	m.detailDiffLoading = true
	m.detailViewport.SetContent(m.detailContent())
	m.detailViewport.GotoTop()
	return m, m.cmdFetchDiff(m.detailItem)
}

// cmdFetchDiff fetches the focused PR's diff. Kept off the body path because the
// diff isn't part of the GraphQL query; it's pulled on demand via fetchDiff.
func (m model) cmdFetchDiff(it *item) tea.Cmd {
	key := cacheKey(it)
	number := it.number
	return func() tea.Msg {
		diff, err := fetchDiff(number)
		if err != nil {
			return diffMsg{key: key, err: err}
		}
		return diffMsg{key: key, diff: diff}
	}
}

// --- GitHub integration ---

// graphQLClient is the minimal slice of *api.GraphQLClient the fetch layer
// actually uses: a single Do(query, variables, &response) call. Depending on
// this interface (rather than the concrete client) lets tests inject a fake
// that returns canned responses/errors so the query-building and
// response-parsing logic can be exercised fully offline.
type graphQLClient interface {
	Do(query string, variables map[string]interface{}, response interface{}) error
}

// repoInfo is the resolved owner/name of the repository the fetch layer queries
// against. It mirrors the fields used from repository.Repository so tests can
// supply a fixed repo without reading `gh` config / resolving a git remote.
type repoInfo struct {
	Owner string
	Name  string
}

// newGraphQLClient and currentRepo are the injection seams for the GitHub I/O
// layer. Production wires the real client/repo resolution (api default client,
// repository.Current); tests swap them for fakes returning canned data/errors.
// Kept as package vars to match the existing fetchIssuesAndPRs/ghDiff pattern.
var (
	newGraphQLClient = func() (graphQLClient, error) {
		return api.DefaultGraphQLClient()
	}
	currentRepo = func() (repoInfo, error) {
		repo, err := repository.Current()
		if err != nil {
			return repoInfo{}, err
		}
		return repoInfo{Owner: repo.Owner, Name: repo.Name}, nil
	}
)

type dataMsg struct {
	issues []list.Item
	prs    []list.Item
}

type bodyMsg struct {
	key  string
	body string
	err  error
}

type diffMsg struct {
	key  string
	diff string
	err  error
}

type errMsg struct {
	err error
}

// fetchBody pulls the current body for a single issue or PR from GitHub. It is a
// blocking call meant to run inside a tea.Cmd. Kept separate from the list fetch
// so the detail diff (fetchDiff) can grow independently.
func fetchBody(number int, isPR bool) (string, error) {
	client, err := newGraphQLClient()
	if err != nil {
		return "", err
	}
	repo, err := currentRepo()
	if err != nil {
		return "", err
	}

	field := "issue"
	if isPR {
		field = "pullRequest"
	}
	query := fmt.Sprintf(`
		query($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) {
				%s(number: $number) {
					body
				}
			}
		}
	`, field)
	variables := map[string]interface{}{
		"owner":  repo.Owner,
		"repo":   repo.Name,
		"number": number,
	}

	type response struct {
		Repository struct {
			Issue struct {
				Body string `json:"body"`
			} `json:"issue"`
			PullRequest struct {
				Body string `json:"body"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	var resp response
	if err := client.Do(query, variables, &resp); err != nil {
		return "", err
	}
	if isPR {
		return resp.Repository.PullRequest.Body, nil
	}
	return resp.Repository.Issue.Body, nil
}

// ghDiff shells out to `gh pr diff <number>` and returns its stdout. It is a
// package var so tests can stub the network/`gh` call and keep the diff plumbing
// hermetic.
var ghDiff = func(number int) (string, error) {
	stdout, stderr, err := gh.Exec("pr", "diff", strconv.Itoa(number))
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s: %w", msg, err)
		}
		return "", err
	}
	return stdout.String(), nil
}

// fetchDiff pulls a single PR's diff from GitHub via `gh pr diff`. It is a
// blocking call meant to run inside a tea.Cmd.
func fetchDiff(number int) (string, error) {
	return ghDiff(number)
}

// colorizeDiff applies green/red foreground colors to added/removed diff lines
// (leaving hunk headers and context lines unstyled) so the diff is readable in
// the detail viewport. Plain-text rendering is the fallback when the input has
// no recognizable diff markers.
func colorizeDiff(diff string) string {
	if diff == "" {
		return ""
	}
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e"))
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7"))
	lines := strings.Split(diff, "\n")
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
			lines[i] = metaStyle.Render(ln)
		case strings.HasPrefix(ln, "@@"):
			lines[i] = metaStyle.Render(ln)
		case strings.HasPrefix(ln, "+"):
			lines[i] = addStyle.Render(ln)
		case strings.HasPrefix(ln, "-"):
			lines[i] = delStyle.Render(ln)
		}
	}
	return strings.Join(lines, "\n")
}

// fetchIssuesAndPRs returns the cmd that loads the issue/PR lists from GitHub.
// It is a package var (like ghDiff) so tests can swap in a hermetic data source
// and drive the program offline without hitting the network.
var fetchIssuesAndPRs = func() tea.Cmd {
	return func() tea.Msg {
		client, err := newGraphQLClient()
		if err != nil {
			return errMsg{err}
		}

		repo, err := currentRepo()
		if err != nil {
			return errMsg{err}
		}

		query := `
			query($owner: String!, $repo: String!) {
				repository(owner: $owner, name: $repo) {
					issues(first: 50, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						nodes {
							number
							title
							body
						}
					}
					pullRequests(first: 50, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
						nodes {
							number
							title
							body
						}
					}
				}
			}
		`
		variables := map[string]interface{}{
			"owner": repo.Owner,
			"repo":  repo.Name,
		}

		type node struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
		}
		type response struct {
			Repository struct {
				Issues struct {
					Nodes []node `json:"nodes"`
				} `json:"issues"`
				PullRequests struct {
					Nodes []node `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}

		var resp response
		if err := client.Do(query, variables, &resp); err != nil {
			return errMsg{err}
		}

		var issues, prs []list.Item
		for _, n := range resp.Repository.Issues.Nodes {
			issues = append(issues, item{number: n.Number, title: n.Title, body: n.Body, type_: "issue"})
		}
		for _, n := range resp.Repository.PullRequests.Nodes {
			prs = append(prs, item{number: n.Number, title: n.Title, body: n.Body, type_: "pr"})
		}

		return dataMsg{issues: issues, prs: prs}
	}
}
