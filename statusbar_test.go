package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// The list view's status bar shows the current mode and the core key hints,
// including how to quit.
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
	if !strings.Contains(bar, "Issues") {
		t.Errorf("status bar missing mode %q: %q", "Issues", bar)
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

// Switching to the PRs tab is reflected in the status bar mode.
func TestStatusBarPRMode(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "a", type_: "issue"}},
		prs:    []list.Item{item{number: 2, title: "b", type_: "pr"}},
	})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})

	bar := tm.(model).renderStatusBar()
	if !strings.Contains(bar, "PRs") {
		t.Errorf("status bar missing PR mode: %q", bar)
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
	if !strings.Contains(bar, "/ be") {
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
	if !strings.Contains(bar, "/ al") {
		t.Errorf("status bar missing active filter query: %q", bar)
	}
	if strings.Contains(bar, "filter:") {
		t.Errorf("status bar should not show 'filter:' prefix: %q", bar)
	}
	if strings.Contains(bar, "Issues") {
		t.Errorf("status bar should not show mode label in filter display: %q", bar)
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

	// As runes are typed the live value renders one space after the slash.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b', 'e'}})
	bar = tm.(model).renderStatusBar()
	if !strings.Contains(bar, "/ be") {
		t.Errorf("status bar missing live typed filter value: %q", bar)
	}
}
