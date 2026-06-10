package main

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

type tab int

const (
	tabIssues tab = iota
	tabPRs
)

type item struct {
	number int
	title  string
	body   string
	type_  string // "issue" or "pr"
}

func (i item) FilterValue() string { return i.title }
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
	detailOpen    bool
	detailLoading bool
	detailItem    *item
	detailBody    string

	// Cache: "issue_42" or "pr_7" -> body
	bodyCache map[string]string

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
		issueList: list.New([]list.Item{}, newItemDelegate(), 0, 0),
		prList:    list.New([]list.Item{}, newItemDelegate(), 0, 0),
	}
	m.issueList.SetShowHelp(false)
	m.prList.SetShowHelp(false)
	m.issueList.SetShowTitle(false)
	m.prList.SetShowTitle(false)

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

func (m *model) setListItems(tabIdx tab, items []list.Item) {
	switch tabIdx {
	case tabIssues:
		m.issueList.SetItems(items)
	case tabPRs:
		m.prList.SetItems(items)
	}
}

func (m model) Init() tea.Cmd {
	return fetchIssuesAndPRs()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateListSize()

	case tea.KeyMsg:
		if m.detailOpen {
			switch msg.String() {
			case "esc", "q":
				m.detailOpen = false
				m.detailItem = nil
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
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
				m.detailBody = m.cachedBody(it)
				if m.detailBody == "" {
					m.detailLoading = true
					return m, m.cmdFetchBody(it)
				}
				m.detailLoading = false
			}
		}

	case dataMsg:
		m.loading = false
		m.err = nil
		m.setListItems(tabIssues, msg.issues)
		m.setListItems(tabPRs, msg.prs)
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
		m.bodyCache[msg.key] = msg.body
		if m.detailOpen && m.detailItem != nil && cacheKey(m.detailItem) == msg.key {
			m.detailBody = m.cachedBody(m.detailItem)
			m.detailLoading = false
		}

	case errMsg:
		m.loading = false
		m.err = msg.err
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
	listHeight := m.height - 3
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

	// Error
	if m.err != nil {
		b += fmt.Sprintf("Error: %v\n", m.err)
		return b
	}

	// Loading
	if m.loading {
		b += "Loading..."
		return b
	}

	// List
	cur := m.currentList()
	b += cur.View()

	return b
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
		tabs = append(tabs, style.Render(t))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, tabs...)
}

func (m model) renderDetail() string {
	if m.detailItem == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89")).MarginBottom(1)
	bodyStyle := lipgloss.NewStyle().Width(m.width - 2)

	title := titleStyle.Render(fmt.Sprintf("#%d %s", m.detailItem.number, m.detailItem.title))
	meta := metaStyle.Render(fmt.Sprintf("[%s]  (esc/q to close)", m.detailItem.type_))

	body := m.detailBody
	if m.detailLoading {
		body = "Loading body..."
	}
	bodyRendered := bodyStyle.Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, title, meta, bodyRendered)
}

func cacheKey(it *item) string {
	return fmt.Sprintf("%s_%d", it.type_, it.number)
}

func (m model) cmdPrefetchBody(it *item) tea.Cmd {
	return func() tea.Msg {
		return bodyMsg{key: cacheKey(it), body: it.body}
	}
}

func (m model) cmdFetchBody(it *item) tea.Cmd {
	return func() tea.Msg {
		return bodyMsg{key: cacheKey(it), body: it.body}
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
}

type errMsg struct {
	err error
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
