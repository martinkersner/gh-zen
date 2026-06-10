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
