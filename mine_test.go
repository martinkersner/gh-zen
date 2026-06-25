package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// stubMineFetch swaps fetchMineItems for the test duration, recording the number
// of calls and returning the given dataMsg-producing cmd.
func stubMineFetch(t *testing.T, msg dataMsg) *int {
	t.Helper()
	calls := 0
	orig := fetchMineItems
	fetchMineItems = func(*githubConn) tea.Cmd {
		calls++
		return func() tea.Msg { return msg }
	}
	t.Cleanup(func() { fetchMineItems = orig })
	return &calls
}

// stubAllFetch swaps fetchIssuesAndPRs, recording its call count so the test can
// assert the off-scope path is taken when mineOnly is cleared.
func stubAllFetch(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := fetchIssuesAndPRs
	fetchIssuesAndPRs = func(*githubConn) tea.Cmd {
		calls++
		return func() tea.Msg { return dataMsg{} }
	}
	t.Cleanup(func() { fetchIssuesAndPRs = orig })
	return &calls
}

func pressKey(tm tea.Model, s string) (tea.Model, tea.Cmd) {
	return tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// Pressing `m` flips mineOnly, routes the refetch through fetchMineItems, sets
// the loading flag, and resets both lists to the top.
func TestMineToggleFlipsAndFetches(t *testing.T) {
	mineCalls := stubMineFetch(t, dataMsg{issues: mkItems(2, "issue"), prs: mkItems(1, "pr")})

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue"), prs: mkItems(10, "pr")})

	// Move the cursor down so the reset-to-top is observable.
	for i := 0; i < 4; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	}

	tm, cmd := pressKey(tm, "m")
	mm := tm.(model)
	if !mm.mineOnly {
		t.Fatal("mineOnly not set after pressing m")
	}
	if !mm.loading {
		t.Error("loading flag not set after toggle")
	}
	if mm.issueList.Index() != 0 || mm.prList.Index() != 0 {
		t.Errorf("lists not reset to top: issue=%d pr=%d", mm.issueList.Index(), mm.prList.Index())
	}
	if cmd == nil {
		t.Fatal("toggle returned no cmd")
	}
	cmd() // run the fetch cmd
	if *mineCalls != 1 {
		t.Errorf("fetchMineItems called %d times, want 1", *mineCalls)
	}
}

// Pressing `m` a second time toggles back off and routes through the repo
// connection path (fetchIssuesAndPRs), not the mine search.
func TestMineToggleOffRoutesToRepoFetch(t *testing.T) {
	stubMineFetch(t, dataMsg{})
	allCalls := stubAllFetch(t)

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	tm, _ = pressKey(tm, "m") // on
	if !tm.(model).mineOnly {
		t.Fatal("mineOnly not set after first toggle")
	}
	tm, cmd := pressKey(tm, "m") // off
	if tm.(model).mineOnly {
		t.Fatal("mineOnly still set after second toggle")
	}
	cmd()
	if *allCalls != 1 {
		t.Errorf("fetchIssuesAndPRs called %d times after toggling off, want 1", *allCalls)
	}
}

// While mineOnly is on, the status bar surfaces the scope label.
func TestStatusBarShowsMineScope(t *testing.T) {
	stubMineFetch(t, dataMsg{issues: mkItems(2, "issue")})

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(5, "issue")})

	if strings.Contains(tm.(model).renderStatusBar(), mineScopeLabel) {
		t.Fatalf("status bar shows %q before toggle: %q", mineScopeLabel, tm.(model).renderStatusBar())
	}
	tm, _ = pressKey(tm, "m")
	if !strings.Contains(tm.(model).renderStatusBar(), mineScopeLabel) {
		t.Errorf("status bar missing %q while mineOnly: %q", mineScopeLabel, tm.(model).renderStatusBar())
	}
}

// When both the mine scope and the rate-limit backoff notice want the middle,
// the bar shows them together as `@me · <notice>`.
func TestStatusBarComposesMineScopeAndRateLimitNotice(t *testing.T) {
	stubMineFetch(t, dataMsg{issues: mkItems(2, "issue")})

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(5, "issue")})
	tm, _ = pressKey(tm, "m")

	mm := tm.(model)
	mm.conn.setRateLimit(rateLimitNode{Remaining: 1, ResetAt: time.Now().Add(time.Hour)})

	bar := ansi.Strip(mm.renderStatusBar())
	if !strings.Contains(bar, mineScopeLabel+" · rate limit low") {
		t.Errorf("composed bar missing %q joining scope+notice: %q", mineScopeLabel+" · rate limit low", bar)
	}
}

// The auto-refresh tick routes through the mine fetch path while mineOnly is on
// (and pagination hasn't kicked in).
func TestMineTickRefreshesViaMineFetch(t *testing.T) {
	mineCalls := stubMineFetch(t, dataMsg{issues: mkItems(2, "issue")})

	m := newModel()
	m.mineOnly = true
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(3, "issue")})

	*mineCalls = 0 // ignore any setup fetches
	tm, cmd := tm.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("tick returned no cmd")
	}
	// Drain the batch so the refresh cmd runs (the ticker re-arm cmd is harmless).
	drainCmd(cmd)
	if *mineCalls < 1 {
		t.Errorf("tick did not route through fetchMineItems (calls=%d)", *mineCalls)
	}
}

// Lazy pagination routes through fetchMoreMineItems for the active tab while
// mineOnly is on, and the page appends to that one tab using its own cursor.
func TestMinePaginationRoutesAndSplits(t *testing.T) {
	var gotTab tab
	var gotAfter string
	calls := 0
	orig := fetchMoreMineItems
	fetchMoreMineItems = func(_ *githubConn, tb tab, after string) tea.Cmd {
		calls++
		gotTab = tb
		gotAfter = after
		return func() tea.Msg {
			return moreDataMsg{
				tab:       tb,
				items:     mkItems(2, "issue"),
				endCursor: "IC2", hasNextPage: false,
			}
		}
	}
	t.Cleanup(func() { fetchMoreMineItems = orig })

	m := newModel()
	m.mineOnly = true
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Each tab carries its own cursor/hasNext (independent type-scoped searches).
	tm, _ = tm.Update(dataMsg{
		issues:         mkItems(50, "issue"),
		prs:            mkItems(5, "pr"),
		issueEndCursor: "IC", issueHasNextPage: true,
		prEndCursor: "PC", prHasNextPage: false,
	})

	// Jump to the last issue, within the pagination threshold of the end: the
	// issues tab's load-more fires with the issues cursor.
	tm, _ = pressKey(tm, "G")
	if calls != 1 {
		t.Fatalf("fetchMoreMineItems called %d times, want 1", calls)
	}
	if gotTab != tabIssues {
		t.Errorf("tab = %v, want tabIssues", gotTab)
	}
	if gotAfter != "IC" {
		t.Errorf("after = %q, want IC", gotAfter)
	}

	// Deliver the issues page: issues append (50->52), PRs untouched (still 5);
	// only the issues cursor/pages advance.
	tm, _ = tm.Update(moreDataMsg{
		tab:       tabIssues,
		items:     mkItems(2, "issue"),
		endCursor: "IC2", hasNextPage: false,
	})
	mm := tm.(model)
	if got := len(mm.issueList.Items()); got != 52 {
		t.Errorf("issues = %d, want 52", got)
	}
	if got := len(mm.prList.Items()); got != 5 {
		t.Errorf("prs = %d, want 5 (untouched)", got)
	}
	if mm.issuePages != 2 || mm.prPages != 1 {
		t.Errorf("pages = issue %d pr %d, want issue 2, pr 1", mm.issuePages, mm.prPages)
	}
	if mm.issueCursor != "IC2" {
		t.Errorf("issue cursor = %q, want IC2", mm.issueCursor)
	}
	if mm.prCursor != "PC" {
		t.Errorf("pr cursor = %q, want PC (untouched)", mm.prCursor)
	}
}

// The `m  toggle mine` shortcut appears in the list-view help.
func TestMineShortcutInHelp(t *testing.T) {
	m := newModel()
	found := false
	for _, s := range m.currentShortcuts() {
		if s.keys == "m" && strings.Contains(s.desc, "mine") {
			found = true
		}
	}
	if !found {
		t.Errorf("list-view shortcuts missing 'm toggle mine': %+v", m.currentShortcuts())
	}
}

// drainCmd runs a (possibly batched) tea.Cmd to completion, executing every
// produced message-cmd so stubbed fetch funcs registered inside the batch run.
func drainCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if bm, ok := msg.(tea.BatchMsg); ok {
		for _, c := range bm {
			drainCmd(c)
		}
	}
}
