package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func mkKey(s string) tea.KeyMsg {
	switch s {
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "/":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestFilterMove(t *testing.T) {
	m := newModel()
	items := []list.Item{
		item{number: 1, title: "alpha", type_: "issue"},
		item{number: 2, title: "alfa-two", type_: "issue"},
		item{number: 3, title: "alfa-three", type_: "issue"},
		item{number: 4, title: "beta", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// open filter, type "alf"
	for _, k := range []string{"/", "a", "l", "f"} {
		tm, _ = tm.Update(mkKey(k))
	}
	mm := tm.(model)
	t.Logf("after filter: state=%v idx=%d settingFilter=%v visible=%d",
		mm.issueList.FilterState(), mm.issueList.Index(), mm.issueList.SettingFilter(), len(mm.issueList.VisibleItems()))

	before := mm.View()

	tm, _ = tm.Update(mkKey("ctrl+n"))
	mm = tm.(model)
	after := mm.View()
	t.Logf("after ctrl+n: idx=%d", mm.issueList.Index())
	if mm.issueList.Index() == 0 {
		t.Errorf("cursor did not move with ctrl+n while filtering")
	}
	if before == after {
		t.Errorf("view unchanged after ctrl+n: highlight not visible while filtering")
	}
}

// TestJKMove verifies j/k move the list cursor down/up when NOT filtering,
// mirroring ctrl+n/ctrl+p.
func TestJKMove(t *testing.T) {
	m := newModel()
	items := []list.Item{
		item{number: 1, title: "alpha", type_: "issue"},
		item{number: 2, title: "beta", type_: "issue"},
		item{number: 3, title: "gamma", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// j moves down.
	tm, _ = tm.Update(mkKey("j"))
	mm := tm.(model)
	if mm.issueList.Index() != 1 {
		t.Fatalf("after j: got index %d, want 1", mm.issueList.Index())
	}

	// j again moves down further.
	tm, _ = tm.Update(mkKey("j"))
	mm = tm.(model)
	if mm.issueList.Index() != 2 {
		t.Fatalf("after second j: got index %d, want 2", mm.issueList.Index())
	}

	// k moves back up.
	tm, _ = tm.Update(mkKey("k"))
	mm = tm.(model)
	if mm.issueList.Index() != 1 {
		t.Fatalf("after k: got index %d, want 1", mm.issueList.Index())
	}
}

// TestJKLiteralWhileFiltering verifies j/k are typed into the filter input and
// do NOT move the cursor while filtering is active.
func TestJKLiteralWhileFiltering(t *testing.T) {
	m := newModel()
	items := []list.Item{
		item{number: 1, title: "jvalue-one", type_: "issue"},
		item{number: 2, title: "jvalue-two", type_: "issue"},
		item{number: 3, title: "kother", type_: "issue"},
		item{number: 4, title: "beta", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// open filter, type "j" — must be literal input, not cursor movement.
	tm, _ = tm.Update(mkKey("/"))
	tm, _ = tm.Update(mkKey("j"))
	mm := tm.(model)
	if mm.issueList.Index() != 0 {
		t.Fatalf("j moved cursor while filtering: index %d, want 0", mm.issueList.Index())
	}
	if got := mm.issueList.FilterValue(); got != "j" {
		t.Fatalf("filter value after typing j: got %q, want %q", got, "j")
	}

	// k likewise stays literal.
	tm, _ = tm.Update(mkKey("k"))
	mm = tm.(model)
	if mm.issueList.Index() != 0 {
		t.Fatalf("k moved cursor while filtering: index %d, want 0", mm.issueList.Index())
	}
	if got := mm.issueList.FilterValue(); got != "jk" {
		t.Fatalf("filter value after typing jk: got %q, want %q", got, "jk")
	}
}
