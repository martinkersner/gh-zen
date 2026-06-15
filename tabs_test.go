package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTabCountsAfterFetch(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	issues := []list.Item{
		item{number: 1, title: "a", type_: "issue"},
		item{number: 2, title: "b", type_: "issue"},
	}
	prs := []list.Item{
		item{number: 3, title: "c", type_: "pr"},
	}
	tm, _ = tm.Update(dataMsg{issues: issues, prs: prs})

	mm := tm.(model)
	if got := mm.tabCount(tabIssues); got != 2 {
		t.Errorf("tabCount(issues) = %d, want 2", got)
	}
	if got := mm.tabCount(tabPRs); got != 1 {
		t.Errorf("tabCount(prs) = %d, want 1", got)
	}

	tabs := mm.renderTabs()
	if !strings.Contains(tabs, "Issues (2)") {
		t.Errorf("tabs missing %q: %q", "Issues (2)", tabs)
	}
	if !strings.Contains(tabs, "PRs (1)") {
		t.Errorf("tabs missing %q: %q", "PRs (1)", tabs)
	}
}

// The list renders directly under the tab row, with no blank line between them.
func TestListRendersDirectlyUnderTabs(t *testing.T) {
	m := newModel()
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "alpha", type_: "issue"}},
	})

	lines := strings.Split(tm.(model).View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("view too short: %q", lines)
	}
	// Line 0 is the tab row; line 1 must be the first list item, not a blank.
	if strings.TrimSpace(lines[1]) == "" {
		t.Errorf("blank line between tabs and list: %q", lines[:3])
	}
	if !strings.Contains(lines[1], "#1 alpha") {
		t.Errorf("first list row not directly under tabs, got %q", lines[1])
	}
}

func TestTabCountsZeroBeforeFetch(t *testing.T) {
	m := newModel()
	if got := m.tabCount(tabIssues); got != 0 {
		t.Errorf("tabCount(issues) = %d, want 0", got)
	}
	if got := m.tabCount(tabPRs); got != 0 {
		t.Errorf("tabCount(prs) = %d, want 0", got)
	}
}

// While the initial fetch is in flight (m.loading == true) the count is unknown,
// so the brackets must show "?" rather than a misleading "0".
func TestTabBracketsShowQuestionMarkWhileLoading(t *testing.T) {
	m := newModel() // newModel sets loading == true
	if !m.loading {
		t.Fatal("expected newModel to start with loading == true")
	}
	tabs := m.renderTabs()
	if !strings.Contains(tabs, "Issues (?)") {
		t.Errorf("tabs missing %q while loading: %q", "Issues (?)", tabs)
	}
	if !strings.Contains(tabs, "PRs (?)") {
		t.Errorf("tabs missing %q while loading: %q", "PRs (?)", tabs)
	}
}

// Once a fetch genuinely returns zero items the brackets must show the real
// "0", not "?" — loading is cleared even on an empty result.
func TestTabBracketsShowZeroAfterEmptyFetch(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: nil, prs: nil})

	mm := tm.(model)
	if mm.loading {
		t.Fatal("expected loading cleared after fetch")
	}
	tabs := mm.renderTabs()
	if !strings.Contains(tabs, "Issues (0)") {
		t.Errorf("tabs missing %q after empty fetch: %q", "Issues (0)", tabs)
	}
	if !strings.Contains(tabs, "PRs (0)") {
		t.Errorf("tabs missing %q after empty fetch: %q", "PRs (0)", tabs)
	}
}

// The empty-state "No items." text must be indented to column 1 so it aligns
// with the tab labels and the item titles in a populated list (issue #68).
func TestEmptyStateAlignedToColumnOne(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: nil, prs: nil})
	mm := tm.(model)

	for _, tc := range []struct {
		name string
		view string
	}{
		{"issues", mm.issueList.View()},
		{"prs", mm.prList.View()},
	} {
		if !strings.HasPrefix(tc.view, " No items.") {
			t.Errorf("%s empty view not indented to column 1: %q", tc.name, tc.view)
		}
	}
}
