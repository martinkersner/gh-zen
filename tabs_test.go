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
	if !strings.Contains(tabs, "Pull Requests (1)") {
		t.Errorf("tabs missing %q: %q", "Pull Requests (1)", tabs)
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
