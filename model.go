package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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
	// detailDiffErr holds a failed diff fetch so the render/offset branches can
	// detect the error state from its own field rather than sniffing detailDiff
	// for a sentinel message. Nil means no error; the user-facing "Error loading
	// diff" text is formatted from it at display time.
	detailDiffErr error

	// Parsed view of the current PR diff and its presentation toggles. The diff
	// text (detailDiff) is parsed once per delivery into detailFiles; the diff
	// sub-view then renders from that structure. detailSplitView selects the
	// side-by-side (split) layout over unified; detailShowOverview overlays the
	// changed-files overview pane; detailActiveFile is the file that file
	// navigation ([ / ]) last jumped to (also highlighted in the overview).
	detailFiles        []fileDiff
	detailFileOffsets  []int
	detailSplitView    bool
	detailShowOverview bool
	detailActiveFile   int

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

	// Move with j/k, ctrl+n / ctrl+p (and arrows). bubbletea's list disables the
	// cursor keymap while filtering, so j/k stay literal input in the filter box.
	up := key.NewBinding(key.WithKeys("up", "ctrl+p", "k"), key.WithHelp("ctrl+p", "up"))
	down := key.NewBinding(key.WithKeys("down", "ctrl+n", "j"), key.WithHelp("ctrl+n", "down"))
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
// For a user-triggered refresh (background == false) it sets the matching
// loading flag so the status-bar indicator (see renderStatusBar) reflects the
// in-flight fetch even though the body stays populated; the flag is cleared by
// the corresponding message handler on completion or error. For a background
// auto-refresh tick (background == true) the fetch still runs but the loading
// flag is left untouched, so the indicator does not flicker on every interval
// when the view is already populated. Pointer receiver so the flag set persists
// in the caller.
func (m *model) refreshCurrentView(background bool) tea.Cmd {
	if m.detailOpen && m.detailItem != nil {
		// In the PR diff sub-view, refresh the diff rather than the body so the
		// visible content is what actually gets updated.
		if m.detailShowDiff {
			if !background {
				m.detailDiffLoading = true
			}
			return m.cmdFetchDiff(m.detailItem)
		}
		if !background {
			m.detailLoading = true
		}
		return m.cmdFetchBody(m.detailItem)
	}
	if !background {
		m.loading = true
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
				m.detailSplitView = false
				m.detailShowOverview = false
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
			case "s":
				// Toggle the side-by-side (split) layout. Only meaningful in the
				// diff sub-view; narrow terminals fall back to unified at render.
				if m.detailShowDiff {
					m.detailSplitView = !m.detailSplitView
					m.detailViewport.GotoTop()
					m.detailActiveFile = 0
					m.refreshDiffView()
				}
				return m, nil
			case "f":
				// Toggle the changed-files overview pane over the diff.
				if m.detailShowDiff {
					m.detailShowOverview = !m.detailShowOverview
					m.detailViewport.GotoTop()
					m.refreshDiffView()
				}
				return m, nil
			case "]":
				// Jump to the next changed file's header.
				if m.detailShowDiff && !m.detailShowOverview {
					m.jumpToFile(m.detailActiveFile + 1)
				}
				return m, nil
			case "[":
				// Jump to the previous changed file's header.
				if m.detailShowDiff && !m.detailShowOverview {
					m.jumpToFile(m.detailActiveFile - 1)
				}
				return m, nil
			case "g":
				// Jump to the top of the detail viewport.
				m.detailViewport.GotoTop()
				return m, nil
			case "G":
				// Jump to the bottom of the detail viewport.
				m.detailViewport.GotoBottom()
				return m, nil
			case "r":
				return m, m.refreshCurrentView(false)
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
			return m, m.refreshCurrentView(false)
		case "tab", "l", "right":
			m.activeTab = (m.activeTab + 1) % tab(len(m.tabs))
			m.updateListSize()
		case "shift+tab", "h", "left":
			m.activeTab = (m.activeTab - 1 + tab(len(m.tabs))) % tab(len(m.tabs))
			m.updateListSize()
		case "g":
			// Jump the selection to the first item.
			cur := m.currentList()
			if len(cur.VisibleItems()) > 0 {
				cur.Select(0)
			}
			return m, nil
		case "G":
			// Jump the selection to the last item.
			cur := m.currentList()
			if n := len(cur.VisibleItems()); n > 0 {
				cur.Select(n - 1)
			}
			return m, nil
		case "enter":
			it := m.selectedItem()
			if it != nil {
				m.detailOpen = true
				m.detailItem = it
				m.detailShowDiff = false
				m.detailSplitView = false
				m.detailShowOverview = false
				m.detailBody = m.cachedBody(it)
				m.detailLoading = m.detailBody == ""
				m.openDetailViewport()
				if m.detailLoading {
					cmds = append(cmds, m.cmdFetchBody(it))
				}
				// Prefetch the PR diff in the background so the first `d` toggle
				// usually serves from diffCache instead of blocking on a fetch. No
				// detailDiffLoading flag is set: this is silent (like body prefetch),
				// and the diffMsg handler caches it. If the user toggles to the diff
				// before this lands, toggleDiff sees the cache miss, sets the loading
				// flag itself, and the prefetch result populates the view on arrival.
				if it.type_ == "pr" {
					if _, ok := m.diffCache[cacheKey(it)]; !ok {
						cmds = append(cmds, m.cmdFetchDiff(it))
					}
				}
				return m, tea.Batch(cmds...)
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
					m.detailDiffErr = msg.err
					m.detailDiff = ""
					m.detailFiles = nil
					m.detailFileOffsets = nil
					m.detailViewport.SetContent(m.detailContent())
				}
			}
			break
		}
		m.diffCache[msg.key] = msg.diff
		if m.detailOpen && m.detailItem != nil && cacheKey(m.detailItem) == msg.key {
			m.setDetailDiff(msg.diff)
			m.detailDiffLoading = false
			if m.detailShowDiff {
				// Preserve scroll position so an auto/manual refresh of an already
				// open diff doesn't yank the viewport back to the top. On the first
				// toggle the offset is already 0, so this still opens at the top.
				m.refreshDiffView()
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
			cmds = append(cmds, m.refreshCurrentView(true))
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
		// Leaving the diff resets its presentation toggles so the next open
		// starts on the unified, no-overview view.
		m.detailShowOverview = false
		m.detailViewport.SetContent(m.detailContent())
		m.detailViewport.GotoTop()
		return m, nil
	}
	// Showing the diff: use the cache if present, otherwise fetch it.
	if cached, ok := m.diffCache[cacheKey(m.detailItem)]; ok {
		m.setDetailDiff(cached)
		m.detailDiffLoading = false
		m.detailFileOffsets = m.diffFileOffsets()
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
