package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// initialPage seeds a model with a first page that has a next page available, so
// the lazy-pagination machinery is armed. Returns the driven model.
func initialPage(t *testing.T, issues int) tea.Model {
	t.Helper()
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues:           mkItems(issues, "issue"),
		issueTotal:       issues * 4,
		issueEndCursor:   "CURSOR1",
		issueHasNextPage: true,
	})
	return tm
}

// Scrolling within paginateThreshold rows of the last loaded item fires a
// next-page fetch for the active tab, carrying the stored cursor; the resulting
// moreDataMsg appends its items without disturbing the cursor.
func TestScrollNearEndLoadsMorePage(t *testing.T) {
	var gotTab tab
	var gotAfter string
	calls := 0
	orig := fetchMoreItems
	fetchMoreItems = func(_ *githubConn, tb tab, after string) tea.Cmd {
		calls++
		gotTab, gotAfter = tb, after
		return func() tea.Msg {
			return moreDataMsg{tab: tb, items: mkItems(50, "issue"), endCursor: "CURSOR2", hasNextPage: true}
		}
	}
	t.Cleanup(func() { fetchMoreItems = orig })

	tm := initialPage(t, 50)

	// Jump to the last loaded item — within the threshold of the end.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if calls != 1 {
		t.Fatalf("fetchMoreItems called %d times, want 1", calls)
	}
	if gotTab != tabIssues || gotAfter != "CURSOR1" {
		t.Errorf("fetchMoreItems(tab=%v, after=%q), want (issues, CURSOR1)", gotTab, gotAfter)
	}

	// Deliver the page; it should be appended (50 -> 100) and cursor advanced.
	idxBefore := tm.(model).issueList.Index()
	tm, _ = tm.Update(moreDataMsg{tab: tabIssues, items: mkItems(50, "issue"), endCursor: "CURSOR2", hasNextPage: true})
	mm := tm.(model)
	if got := len(mm.issueList.Items()); got != 100 {
		t.Errorf("after append: %d items, want 100", got)
	}
	if mm.issueCursor != "CURSOR2" || !mm.issueHasNext || mm.issuePages != 2 {
		t.Errorf("pagination state = cursor %q hasNext %v pages %d", mm.issueCursor, mm.issueHasNext, mm.issuePages)
	}
	if mm.issueLoadingMore {
		t.Error("loadingMore not cleared after page delivered")
	}
	if got := mm.issueList.Index(); got != idxBefore {
		t.Errorf("append moved cursor: %d -> %d", idxBefore, got)
	}
}

// While a next-page fetch is in flight (loadingMore), further scrolls must not
// fire a duplicate fetch.
func TestNoDuplicateLoadMoreWhileInFlight(t *testing.T) {
	calls := 0
	orig := fetchMoreItems
	fetchMoreItems = func(_ *githubConn, tb tab, _ string) tea.Cmd {
		calls++
		return func() tea.Msg { return nil } // never delivers, so loadingMore stays set
	}
	t.Cleanup(func() { fetchMoreItems = orig })

	tm := initialPage(t, 50)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if calls != 1 {
		t.Errorf("fetchMoreItems called %d times, want 1 (in-flight guard failed)", calls)
	}
}

// No next page (hasNextPage false) means no fetch, however far the user scrolls.
func TestNoLoadMoreWhenNoNextPage(t *testing.T) {
	calls := 0
	orig := fetchMoreItems
	fetchMoreItems = func(_ *githubConn, _ tab, _ string) tea.Cmd {
		calls++
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { fetchMoreItems = orig })

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(30, "issue"), issueHasNextPage: false})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if calls != 0 {
		t.Errorf("fetchMoreItems called %d times with no next page, want 0", calls)
	}
}

// Once extra pages are loaded, the background tick must refresh the visible
// window in place (fetchVisibleItems) rather than refetching the whole list
// (fetchIssuesAndPRs) — which would reorder it and reset pagination.
func TestTickRefreshesVisibleWhenPaginated(t *testing.T) {
	fullFetches, visibleFetches := 0, 0
	var visibleTab tab
	origFetch := fetchIssuesAndPRs
	fetchIssuesAndPRs = func(*githubConn) tea.Cmd {
		fullFetches++
		return func() tea.Msg { return dataMsg{} }
	}
	origMore := fetchMoreItems
	fetchMoreItems = func(_ *githubConn, tb tab, _ string) tea.Cmd {
		return func() tea.Msg {
			return moreDataMsg{tab: tb, items: mkItems(50, "issue"), endCursor: "C2", hasNextPage: true}
		}
	}
	origVisible := fetchVisibleItems
	fetchVisibleItems = func(_ *githubConn, tb tab, _ []int) tea.Cmd {
		visibleFetches++
		visibleTab = tb
		return func() tea.Msg { return visibleRefreshMsg{tab: tb} }
	}
	t.Cleanup(func() {
		fetchIssuesAndPRs = origFetch
		fetchMoreItems = origMore
		fetchVisibleItems = origVisible
	})

	tm := initialPage(t, 50)

	// Before paginating, a tick dispatches a full list refresh, not a visible one.
	tm, _ = tm.Update(tickMsg{})
	if fullFetches != 1 || visibleFetches != 0 {
		t.Fatalf("tick at page 1: full=%d visible=%d, want 1/0", fullFetches, visibleFetches)
	}

	// Load a second page (deliver the page the G-triggered fetch would yield).
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	tm, _ = tm.Update(moreDataMsg{tab: tabIssues, items: mkItems(50, "issue"), endCursor: "C2", hasNextPage: true})
	if !tm.(model).paginated() {
		t.Fatal("setup: expected paginated() true after loading a page")
	}

	// Now a tick must refresh the visible window, not refetch the whole list.
	tm, _ = tm.Update(tickMsg{})
	if fullFetches != 1 {
		t.Errorf("tick refetched the whole list while paginated (full=%d, want 1)", fullFetches)
	}
	if visibleFetches != 1 || visibleTab != tabIssues {
		t.Errorf("tick visible-refresh: count=%d tab=%v, want 1/issues", visibleFetches, visibleTab)
	}
}

// The in-place visible refresh patches title/closed on the matching loaded rows
// without changing count, order, the cursor, or other fields (author/body).
func TestVisibleRefreshPatchesInPlace(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{
		item{number: 1, title: "old one", type_: "issue", author: "alice", body: "b1"},
		item{number: 2, title: "old two", type_: "issue", author: "bob", body: "b2"},
	}})
	// Move the cursor off row 0 so we can assert it's preserved.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	idxBefore := tm.(model).issueList.Index()

	tm, _ = tm.Update(visibleRefreshMsg{tab: tabIssues, items: map[int]refreshedItem{
		1: {title: "new one", closed: true},
		2: {title: "old two"}, // unchanged
	}})

	mm := tm.(model)
	items := mm.issueList.Items()
	if len(items) != 2 {
		t.Fatalf("item count changed: %d, want 2", len(items))
	}
	got1 := items[0].(item)
	if got1.title != "new one" || !got1.closed {
		t.Errorf("row 0 not patched: %+v", got1)
	}
	// Untouched fields must survive the patch.
	if got1.author != "alice" || got1.body != "b1" {
		t.Errorf("row 0 lost fields: %+v", got1)
	}
	if mm.issueList.Index() != idxBefore {
		t.Errorf("cursor moved: %d -> %d", idxBefore, mm.issueList.Index())
	}
}

// A full (re)load via dataMsg resets pagination to page 1 — manual `r` reloads
// from the top, dropping appended pages and restarting the cursor.
func TestDataMsgResetsPagination(t *testing.T) {
	orig := fetchMoreItems
	fetchMoreItems = func(_ *githubConn, tb tab, _ string) tea.Cmd {
		return func() tea.Msg {
			return moreDataMsg{tab: tb, items: mkItems(50, "issue"), endCursor: "C2", hasNextPage: true}
		}
	}
	t.Cleanup(func() { fetchMoreItems = orig })

	tm := initialPage(t, 50)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	tm, _ = tm.Update(moreDataMsg{tab: tabIssues, items: mkItems(50, "issue"), endCursor: "C2", hasNextPage: true})
	if tm.(model).issuePages != 2 {
		t.Fatalf("setup: expected 2 pages, got %d", tm.(model).issuePages)
	}

	tm, _ = tm.Update(dataMsg{
		issues:           mkItems(50, "issue"),
		issueEndCursor:   "FRESH",
		issueHasNextPage: true,
	})
	mm := tm.(model)
	if mm.issuePages != 1 || mm.issueCursor != "FRESH" {
		t.Errorf("after reload: pages %d cursor %q, want 1 / FRESH", mm.issuePages, mm.issueCursor)
	}
	if got := len(mm.issueList.Items()); got != 50 {
		t.Errorf("after reload: %d items, want 50 (appended page dropped)", got)
	}
}

// A failed next-page fetch clears the in-flight guard so a later scroll retries.
func TestLoadMoreErrorClearsGuard(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(50, "issue"), issueEndCursor: "C1", issueHasNextPage: true})

	// Force the in-flight state, then deliver an error.
	mm := tm.(model)
	mm.issueLoadingMore = true
	var tm2 tea.Model = mm
	tm2, _ = tm2.Update(moreDataMsg{tab: tabIssues, err: errTest})
	if tm2.(model).issueLoadingMore {
		t.Error("loadingMore not cleared after error")
	}
	if got := len(tm2.(model).issueList.Items()); got != 50 {
		t.Errorf("error appended items: %d, want 50", got)
	}
}

var errTest = errTestType("boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }
