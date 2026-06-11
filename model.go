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

// detailHeaderHeight is the number of lines the detail view reserves above the
// scrollable body: the title line, the meta line, and the meta's bottom margin.
const detailHeaderHeight = 3

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
}

func newItemDelegate() itemDelegate {
	return itemDelegate{styles: list.NewDefaultItemStyles()}
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

	isSelected := index == m.Index()
	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""
	isFiltered := m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied

	switch {
	case emptyFilter:
		title = s.DimmedTitle.Render(title)
	case isSelected:
		if isFiltered {
			unmatched := s.SelectedTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			title = lipgloss.StyleRunes(title, m.MatchesForItem(index), matched, unmatched)
		}
		title = s.SelectedTitle.Render(title)
	default:
		if isFiltered {
			unmatched := s.NormalTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			title = lipgloss.StyleRunes(title, m.MatchesForItem(index), matched, unmatched)
		}
		title = s.NormalTitle.Render(title)
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

	// Cache: "issue_42" or "pr_7" -> body
	bodyCache map[string]string
	// Cache: "pr_7" -> diff text (PRs only)
	diffCache map[string]string

	// Async state
	loading bool
	err     error
}

func newModel() model {
	m := model{
		tabs:      []string{"Issues", "Pull Requests"},
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
func (m model) refreshCurrentView() tea.Cmd {
	if m.detailOpen && m.detailItem != nil {
		// In the PR diff sub-view, refresh the diff rather than the body so the
		// visible content is what actually gets updated.
		if m.detailShowDiff {
			return m.cmdFetchDiff(m.detailItem)
		}
		return m.cmdFetchBody(m.detailItem)
	}
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
		if m.detailOpen {
			switch msg.String() {
			case "esc", "q":
				m.detailOpen = false
				m.detailItem = nil
				m.detailShowDiff = false
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
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
			}
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
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
	// Reserve the tabs row + blank line above the list, plus the status bar row
	// pinned to the bottom, so neither overlaps the scrollable list.
	listHeight := m.height - 3 - statusBarHeight
	m.issueList.SetSize(m.width, listHeight)
	m.prList.SetSize(m.width, listHeight)
}

// detailViewportSize computes the width/height for the detail body viewport
// from the terminal size, reserving detailHeaderHeight lines for the title and
// meta plus statusBarHeight for the bottom status bar. Heights/widths are
// clamped to a minimum of 1 so tiny terminals don't produce negative dimensions.
func detailViewportSize(width, height int) (int, int) {
	w := width - 2
	if w < 1 {
		w = 1
	}
	h := height - detailHeaderHeight - statusBarHeight
	if h < 1 {
		h = 1
	}
	return w, h
}

// detailBodyContent returns the body text (or a loading placeholder) wrapped to
// the viewport width.
func (m model) detailBodyContent() string {
	body := m.detailBody
	if m.detailLoading {
		body = "Loading body..."
	}
	w, _ := detailViewportSize(m.width, m.height)
	return lipgloss.NewStyle().Width(w).Render(body)
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
		w, _ := detailViewportSize(m.width, m.height)
		return lipgloss.NewStyle().Width(w).Render("Loading diff...")
	}
	return colorizeDiff(m.detailDiff)
}

// openDetailViewport sizes the detail viewport, loads the current body, and
// anchors it at the top so the title is always visible when a detail view opens.
func (m *model) openDetailViewport() {
	w, h := detailViewportSize(m.width, m.height)
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
	w, h := detailViewportSize(m.width, m.height)
	m.detailViewport.Width = w
	m.detailViewport.Height = h
	m.detailViewport.SetContent(m.detailContent())
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

	if m.detailOpen {
		return m.renderDetail()
	}

	return m.renderList()
}

func (m model) renderList() string {
	var b string

	// Tabs
	b += m.renderTabs() + "\n\n"

	// Error / Loading still show the bar so quit help is always visible.
	if m.err != nil {
		b += fmt.Sprintf("Error: %v\n", m.err)
	} else if m.loading {
		b += "Loading..."
	} else {
		b += m.currentList().View()
	}

	b += "\n" + m.renderStatusBar()

	return b
}

// renderStatusBar renders the one-line bar pinned to the bottom of the screen.
// The left side shows context (current mode, or the active filter query when
// filtering); the right side shows context-aware key hints. It is rendered in
// both the list and detail views.
func (m model) renderStatusBar() string {
	leftStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))

	var left, hints string
	if m.detailOpen {
		kind := "Issue"
		isPR := m.detailItem != nil && m.detailItem.type_ == "pr"
		if isPR {
			kind = "Pull Request"
		}
		left = kind
		hints = "q/esc back · ctrl+n/ctrl+p scroll · r refresh"
		// PRs gain a key to toggle between the body and the diff view.
		if isPR {
			verb := "diff"
			if m.detailShowDiff {
				verb = "body"
			}
			hints = fmt.Sprintf("q/esc back · ctrl+n/ctrl+p scroll · d %s · r refresh", verb)
		}
	} else {
		mode := "Issues"
		if m.activeTab == tabPRs {
			mode = "Pull Requests"
		}
		left = mode
		// Surface the active filter query so the user can see what they typed.
		cur := m.currentList()
		switch cur.FilterState() {
		case list.Filtering, list.FilterApplied:
			if q := cur.FilterValue(); q != "" {
				left = fmt.Sprintf("%s · filter: %s", mode, q)
			}
		}
		hints = "q/esc quit · tab switch · / filter · enter open"
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
		label := fmt.Sprintf("%s (%d)", t, m.tabCount(tab(i)))
		tabs = append(tabs, style.Render(label))
	}
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

func (m model) renderDetail() string {
	if m.detailItem == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89")).MarginBottom(1)

	title := titleStyle.Render(fmt.Sprintf("#%d %s", m.detailItem.number, m.detailItem.title))
	metaText := fmt.Sprintf("[%s]", m.detailItem.type_)
	if m.detailShowDiff {
		metaText = fmt.Sprintf("[%s · diff]", m.detailItem.type_)
	}
	meta := metaStyle.Render(metaText)

	return lipgloss.JoinVertical(lipgloss.Left, title, meta, m.detailViewport.View(), m.renderStatusBar())
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
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return "", err
	}
	repo, err := repository.Current()
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

func fetchIssuesAndPRs() tea.Cmd {
	return func() tea.Msg {
		client, err := api.DefaultGraphQLClient()
		if err != nil {
			return errMsg{err}
		}

		repo, err := repository.Current()
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
