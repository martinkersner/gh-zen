package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func mkItems(n int, type_ string) []list.Item {
	items := make([]list.Item, n)
	for i := 0; i < n; i++ {
		items[i] = item{number: i + 1, title: "item", type_: type_}
	}
	return items
}

func TestRestoreIndex(t *testing.T) {
	cases := []struct {
		name  string
		count int
		set   int
		want  int
	}{
		{"middle preserved", 5, 3, 3},
		{"clamped to last when shrunk", 2, 4, 1},
		{"empty stays zero", 0, 3, 0},
		{"negative coerced to zero", 5, -2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := list.New(mkItems(c.count, "issue"), newItemDelegate(), 80, 24)
			restoreIndex(&l, c.set)
			if got := l.Index(); got != c.want {
				t.Errorf("restoreIndex(count=%d, set=%d): got %d, want %d", c.count, c.set, got, c.want)
			}
		})
	}
}

// A refresh (new dataMsg) must keep the list cursor where it was instead of
// snapping back to the top.
func TestRefreshPreservesSelection(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue"), prs: mkItems(3, "pr")})

	// Move the cursor down a few rows.
	for i := 0; i < 4; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if got := tm.(model).issueList.Index(); got != 4 {
		t.Fatalf("setup: expected index 4, got %d", got)
	}

	// A refresh delivers a fresh dataMsg; selection should be retained.
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue"), prs: mkItems(3, "pr")})
	if got := tm.(model).issueList.Index(); got != 4 {
		t.Errorf("after refresh: index = %d, want 4 (selection not preserved)", got)
	}
}

// A refresh that returns fewer items clamps the selection to the last item.
func TestRefreshClampsSelectionWhenListShrinks(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue")})
	for i := 0; i < 8; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if got := tm.(model).issueList.Index(); got != 8 {
		t.Fatalf("setup: expected index 8, got %d", got)
	}

	tm, _ = tm.Update(dataMsg{issues: mkItems(3, "issue")})
	if got := tm.(model).issueList.Index(); got != 2 {
		t.Errorf("after shrink: index = %d, want 2 (last item)", got)
	}
}

// A tick re-arms the ticker (returns a command) and does not change which view
// is showing.
func TestTickReArmsTicker(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(2, "issue")})

	tm, cmd := tm.Update(tickMsg{})
	if cmd == nil {
		t.Error("tick returned nil cmd; ticker not re-armed")
	}
	if tm.(model).detailOpen {
		t.Error("tick should not open the detail view")
	}
}

// While the filter input is active, a tick still re-arms the ticker but must not
// dispatch the data fetch (which would reshuffle the list under the user). This
// asserts the actual skip — that fetchIssuesAndPRs is NOT invoked — rather than
// just that the ticker re-armed: deleting the `!m.currentList().SettingFilter()`
// guard in the tickMsg handler must make this test fail.
func TestTickSkipsRefreshWhileFiltering(t *testing.T) {
	// Stub the data fetch so a dispatched refresh is observable (and never hits
	// the network). The counter records whether the tick dispatched it.
	fetches := 0
	orig := fetchIssuesAndPRs
	fetchIssuesAndPRs = func() tea.Cmd {
		fetches++
		return func() tea.Msg { return dataMsg{} }
	}
	t.Cleanup(func() { fetchIssuesAndPRs = orig })

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(5, "issue")})

	// Enter filter mode.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !tm.(model).issueList.SettingFilter() {
		t.Fatal("setup: expected to be in filter input")
	}

	// A tick while filtering: the ticker must still re-arm, but the data fetch
	// must be skipped so the visible filtered list isn't reshuffled.
	before := fetches
	tm, cmd := tm.Update(tickMsg{})
	if cmd == nil {
		t.Error("tick returned nil cmd while filtering; ticker not re-armed")
	}
	if fetches != before {
		t.Errorf("tick dispatched a data fetch while filtering (count %d -> %d); refresh not skipped", before, fetches)
	}

	// Sanity: once the filter is cancelled a tick DOES dispatch the fetch, so the
	// skip above is specific to the filtering state (not a dead fetch seam).
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tm.(model).issueList.SettingFilter() {
		t.Fatal("setup: filter not cancelled by esc")
	}
	before = fetches
	tm, _ = tm.Update(tickMsg{})
	if fetches != before+1 {
		t.Errorf("tick did not dispatch a data fetch when not filtering (count %d -> %d)", before, fetches)
	}
}

// Pressing r in the list view dispatches a refresh command.
func TestRKeyTriggersListRefresh(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(2, "issue")})

	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Error("r in list view returned nil cmd; no refresh dispatched")
	}
	// A user-triggered refresh must raise the loading flag so the status-bar
	// indicator reflects the in-flight fetch (cmd != nil alone is satisfied by
	// the list's own pass-through cmd even if no refresh ran).
	if !tm.(model).loading {
		t.Error("r in list view did not set loading=true")
	}
}

// A failed body refresh keeps the previously cached body instead of blanking it.
func TestBodyRefreshErrorKeepsCachedBody(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{
		item{number: 1, title: "x", body: "original body", type_: "issue"},
	}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !tm.(model).detailOpen {
		t.Fatal("setup: detail did not open")
	}
	key := cacheKey(tm.(model).detailItem)

	tm, _ = tm.Update(bodyMsg{key: key, err: errFake{}})
	mm := tm.(model)
	if mm.detailLoading {
		t.Error("detailLoading should be cleared after a failed refresh")
	}
	if mm.cachedBody(mm.detailItem) != "original body" {
		t.Errorf("cached body lost after failed refresh: %q", mm.cachedBody(mm.detailItem))
	}
}

// A late prefetch bodyMsg (bare list body, no comments) must not clobber a key a
// full fetch already populated with body+comments. Regression for the tick-can-
// enqueue-prefetch-while-full-fetch-in-flight race.
func TestPrefetchDoesNotClobberFullBody(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{
		item{number: 1, title: "x", body: "list body", type_: "issue"},
	}})
	key := cacheKey(&item{number: 1, type_: "issue"})

	// Full fetch lands first with body+comments.
	full := composeDetailBody("real body", []comment{{author: "alice", body: "hi"}}, 1)
	tm, _ = tm.Update(bodyMsg{key: key, body: full})
	// A late prefetch (bare body) arrives afterwards; it must be ignored.
	tm, _ = tm.Update(bodyMsg{key: key, body: "list body", prefetch: true})

	if got := tm.(model).bodyCache[key]; got != full {
		t.Errorf("prefetch clobbered full body+comments:\n got %q\nwant %q", got, full)
	}
}

// A refresh (dataMsg) delivered while a filter is applied must recompute the
// filtered/visible set so the visible list reflects the new items, and the
// restored selection must land on the correct visible row. Regression for the
// discarded SetItems cmd in setListItems (issue #18).
func TestRefreshReFiltersWhileFilterApplied(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Seed: two "alpha" items and one "beta" item.
	tm, _ = tm.Update(dataMsg{issues: []list.Item{
		item{number: 1, title: "alpha", type_: "issue"},
		item{number: 2, title: "alpha", type_: "issue"},
		item{number: 3, title: "beta", type_: "issue"},
	}})

	// Put the list into the FilterApplied state (browsing filtered results, not
	// mid-typing). SetFilterText computes the filtered set synchronously and
	// leaves filterState == FilterApplied, mirroring what the user sees after
	// typing a query and pressing enter.
	mm := tm.(model)
	mm.issueList.SetFilterText("alpha")
	if mm.issueList.FilterState() != list.FilterApplied {
		t.Fatalf("setup: expected FilterApplied, got %v", mm.issueList.FilterState())
	}
	if got := len(mm.issueList.VisibleItems()); got != 2 {
		t.Fatalf("setup: expected 2 visible alpha items, got %d", got)
	}
	// Select the second visible (filtered) item.
	mm.issueList.Select(1)
	tm = mm

	// Refresh: new data has three "alpha" items (and a beta). With the bug,
	// setListItems discards the SetItems cmd, so filteredItems is nil'd and not
	// recomputed: VisibleItems() goes empty and the selection restore clamps
	// against a stale count.
	tm, _ = tm.Update(dataMsg{issues: []list.Item{
		item{number: 4, title: "alpha", type_: "issue"},
		item{number: 5, title: "alpha", type_: "issue"},
		item{number: 6, title: "alpha", type_: "issue"},
		item{number: 7, title: "beta", type_: "issue"},
	}})
	mm = tm.(model)

	// Filter must still be applied and recomputed against the new items.
	if mm.issueList.FilterState() != list.FilterApplied {
		t.Fatalf("after refresh: filter state = %v, want FilterApplied", mm.issueList.FilterState())
	}
	visible := mm.issueList.VisibleItems()
	if len(visible) != 3 {
		t.Fatalf("after refresh: visible count = %d, want 3 (stale filtered set)", len(visible))
	}
	// All visible items must be the new "alpha" items (#4,#5,#6), none stale.
	wantNums := map[int]bool{4: true, 5: true, 6: true}
	for _, it := range visible {
		n := it.(item).number
		if !wantNums[n] {
			t.Errorf("after refresh: unexpected visible item #%d", n)
		}
	}
	// Selection preserved at index 1, now pointing at a valid (new) visible row.
	if got := mm.issueList.Index(); got != 1 {
		t.Errorf("after refresh: index = %d, want 1 (selection not preserved)", got)
	}
	if sel, ok := mm.issueList.SelectedItem().(item); !ok || !wantNums[sel.number] {
		t.Errorf("after refresh: selected item = %+v, want one of #4/#5/#6", mm.issueList.SelectedItem())
	}
}

type errFake struct{}

func (errFake) Error() string { return "fake fetch error" }
