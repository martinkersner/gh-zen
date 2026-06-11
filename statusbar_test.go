package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// The list view's status bar no longer shows a bare mode label (the tabs row
// above already conveys mode); it shows only the core key hints when not
// filtering. The mode lives in renderTabs.
func TestStatusBarListMode(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "a", type_: "issue"}},
		prs:    []list.Item{item{number: 2, title: "b", type_: "pr"}},
	})

	mm := tm.(model)
	bar := mm.renderStatusBar()
	// The bare mode word is redundant with the tabs row and must not appear in
	// the status bar when not filtering.
	if strings.Contains(bar, "Issues") {
		t.Errorf("status bar should not show bare mode label: %q", bar)
	}
	// The tabs row is the single source of truth for the mode.
	if !strings.Contains(mm.renderTabs(), "Issues") {
		t.Errorf("tabs row missing mode label: %q", mm.renderTabs())
	}
	// The inline shortcut list is collapsed into a single `? help` hint; the
	// full list lives in the overlay (see TestHelpOverlay*).
	if !strings.Contains(bar, "? help") {
		t.Errorf("status bar missing help hint: %q", bar)
	}
	if strings.Contains(bar, "q/esc quit") {
		t.Errorf("status bar should no longer enumerate shortcuts: %q", bar)
	}
}

// Switching to the PRs tab is reflected in the tabs row, not the status bar
// (which no longer carries a bare mode label).
func TestStatusBarPRMode(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "a", type_: "issue"}},
		prs:    []list.Item{item{number: 2, title: "b", type_: "pr"}},
	})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})

	mm := tm.(model)
	if strings.Contains(mm.renderStatusBar(), "PRs") {
		t.Errorf("status bar should not show bare PR mode label: %q", mm.renderStatusBar())
	}
	if !strings.Contains(mm.renderTabs(), "PRs") {
		t.Errorf("tabs row missing PR mode label: %q", mm.renderTabs())
	}
}

// The detail view's status bar shows the item kind and back/scroll hints
// instead of the list hints.
func TestStatusBarDetailMode(t *testing.T) {
	m := newModel()
	m.issueList.SetItems([]list.Item{item{number: 5, title: "x", body: "y", type_: "issue"}})
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	mm := tm.(model)
	if !mm.detailOpen {
		t.Fatal("detail did not open")
	}
	bar := mm.renderStatusBar()
	// The detail bar also collapses its shortcut list into `? help`.
	if !strings.Contains(bar, "? help") {
		t.Errorf("detail status bar missing help hint: %q", bar)
	}
	if strings.Contains(bar, "ctrl+n/ctrl+p scroll") {
		t.Errorf("detail status bar should no longer enumerate shortcuts: %q", bar)
	}
	// The view itself must include the bar.
	if !strings.Contains(mm.View(), "? help") {
		t.Errorf("detail view missing status bar")
	}
}

// While searching in the detail view the bar shows the slash-prefixed query
// exactly like the list filter — no "Issue · search: …" / "Pull Request · …"
// formatting — and only the `? help` hint (no per-view shortcut list).
func TestStatusBarDetailSearchParity(t *testing.T) {
	m := newModel()
	m.issueList.SetItems([]list.Item{item{number: 5, title: "x", body: "alpha beta", type_: "issue"}})
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Enter in-detail search and type a query.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b', 'e'}})

	mm := tm.(model)
	if !mm.detailSearching {
		t.Fatal("detail search did not start")
	}
	bar := mm.renderStatusBar()
	if !strings.Contains(bar, "/be") {
		t.Errorf("detail search bar missing slash-prefixed query: %q", bar)
	}
	if strings.Contains(bar, "search:") {
		t.Errorf("detail search bar should not show 'search:' prefix: %q", bar)
	}
	if strings.Contains(bar, "Issue") {
		t.Errorf("detail search bar should not show item kind while searching: %q", bar)
	}
	// Hints must match the list: only the compact `? help`, no shortcut list.
	if !strings.Contains(bar, "? help") {
		t.Errorf("detail search bar missing help hint: %q", bar)
	}
	if strings.Contains(bar, "cancel") || strings.Contains(bar, "match") {
		t.Errorf("detail search bar should not enumerate search shortcuts: %q", bar)
	}
}

// On a narrow terminal the bar must stay a single row (no wrap) so it doesn't
// overflow the one reserved status-bar line.
func TestStatusBarFitsNarrowWidth(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 20, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{item{number: 1, title: "a", type_: "issue"}}})

	bar := tm.(model).renderStatusBar()
	if strings.Contains(bar, "\n") {
		t.Errorf("status bar wrapped onto multiple rows: %q", bar)
	}
}

// When a filter query is active, the bar surfaces the typed query.
func TestStatusBarShowsFilterQuery(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{
			item{number: 1, title: "alpha", type_: "issue"},
			item{number: 2, title: "beta", type_: "issue"},
		},
	})

	// Enter filter mode and type a query.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'l'}})

	bar := tm.(model).renderStatusBar()
	if !strings.Contains(bar, "/al") {
		t.Errorf("status bar missing active filter query: %q", bar)
	}
	if strings.Contains(bar, "filter:") {
		t.Errorf("status bar should not show 'filter:' prefix: %q", bar)
	}
	if strings.Contains(bar, "Issues") {
		t.Errorf("status bar should not show mode label in filter display: %q", bar)
	}
}

// While a fetch is in flight the status bar surfaces a loading indicator that
// clears once data arrives — covering the list view's initial load/refresh,
// where the body may already be populated.
func TestStatusBarShowsLoadingIndicator(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// newModel starts with loading=true, so the bar must show the indicator.
	if !strings.Contains(tm.(model).renderStatusBar(), loadingIndicator) {
		t.Errorf("status bar missing loading indicator while loading: %q", tm.(model).renderStatusBar())
	}

	// Once data arrives the loading flag clears and so must the indicator.
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "a", type_: "issue"}},
	})
	if mm := tm.(model); mm.loading {
		t.Fatal("loading flag should clear after dataMsg")
	}
	if strings.Contains(tm.(model).renderStatusBar(), loadingIndicator) {
		t.Errorf("status bar should clear loading indicator once loaded: %q", tm.(model).renderStatusBar())
	}
}

// The loading indicator also clears when a fetch errors, so the bar never
// reports activity that has already failed.
func TestStatusBarClearsLoadingIndicatorOnError(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(errMsg{err: errors.New("boom")})

	if mm := tm.(model); mm.loading {
		t.Fatal("loading flag should clear after errMsg")
	}
	if strings.Contains(tm.(model).renderStatusBar(), loadingIndicator) {
		t.Errorf("status bar should clear loading indicator on error: %q", tm.(model).renderStatusBar())
	}
}

// A lazily-fetched detail body / PR diff has no body placeholder once the body
// is populated, so the status-bar indicator is the only feedback — it must show
// while detailLoading or detailDiffLoading is set.
func TestStatusBarShowsLoadingIndicatorForDetail(t *testing.T) {
	m := newModel()
	m.loading = false
	m.detailOpen = true
	m.detailItem = &item{number: 5, title: "x", type_: "pr"}
	m.detailLoading = true
	m.width = 80

	if !strings.Contains(m.renderStatusBar(), loadingIndicator) {
		t.Errorf("status bar missing loading indicator while detailLoading: %q", m.renderStatusBar())
	}

	// Indicator must coexist with the item kind already shown on the left.
	if !strings.Contains(m.renderStatusBar(), "Pull Request") {
		t.Errorf("status bar dropped item kind while loading: %q", m.renderStatusBar())
	}

	m.detailLoading = false
	m.detailDiffLoading = true
	if !strings.Contains(m.renderStatusBar(), loadingIndicator) {
		t.Errorf("status bar missing loading indicator while detailDiffLoading: %q", m.renderStatusBar())
	}

	m.detailDiffLoading = false
	if strings.Contains(m.renderStatusBar(), loadingIndicator) {
		t.Errorf("status bar should clear loading indicator when detail loads: %q", m.renderStatusBar())
	}
}

// While typing a filter (the list is in the SettingFilter state) the live,
// editable query is shown in the status bar — not above the list. The built-in
// filter prompt line is suppressed (SetShowFilter(false)), so the bar is the
// only place the in-progress filter appears.
func TestStatusBarShowsLiveFilterWhileTyping(t *testing.T) {
	m := newModel()
	if m.issueList.ShowFilter() {
		t.Fatal("list built-in filter bar should be hidden; SetShowFilter(false) expected")
	}

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{
			item{number: 1, title: "alpha", type_: "issue"},
			item{number: 2, title: "beta", type_: "issue"},
		},
	})

	// Enter filter mode (typing) without yet typing any runes.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := tm.(model)
	if !mm.currentList().SettingFilter() {
		t.Fatal("list not in SettingFilter state after pressing /")
	}
	bar := tm.(model).renderStatusBar()
	// Empty query while typing shows just the slash in the bottom-left — no
	// 'filter:' prefix and no mode label.
	if !strings.Contains(bar, "/") {
		t.Errorf("status bar missing slash for live filter input while typing: %q", bar)
	}
	if strings.Contains(bar, "filter:") {
		t.Errorf("status bar should not show 'filter:' prefix: %q", bar)
	}

	// As runes are typed the live value renders directly after the slash.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b', 'e'}})
	bar = tm.(model).renderStatusBar()
	if !strings.Contains(bar, "/be") {
		t.Errorf("status bar missing live typed filter value: %q", bar)
	}
}
