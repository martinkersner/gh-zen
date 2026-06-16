package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// openCall records the arguments ghOpenInBrowser was invoked with.
type openCall struct {
	itemType string
	number   int
	called   bool
}

// withStubOpen swaps the package-level ghOpenInBrowser with a stub for the
// duration of a test so pressing `o` never launches a real browser. It returns a
// pointer to the recorded call so assertions can inspect the args.
func withStubOpen(t *testing.T) *openCall {
	t.Helper()
	rec := &openCall{}
	orig := ghOpenInBrowser
	ghOpenInBrowser = func(itemType string, number int) error {
		rec.called = true
		rec.itemType = itemType
		rec.number = number
		return nil
	}
	t.Cleanup(func() { ghOpenInBrowser = orig })
	return rec
}

// Pressing `o` in list mode opens the highlighted item in the browser with the
// correct gh subcommand and number.
func TestOpenInBrowserFromListIssue(t *testing.T) {
	rec := withStubOpen(t)
	tm := listModel(t) // issue #1 selected on the Issues tab

	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	cmd() // execute the side-effecting command

	if !rec.called {
		t.Fatal("o did not invoke ghOpenInBrowser")
	}
	if rec.itemType != "issue" || rec.number != 1 {
		t.Errorf("got (%q, %d), want (issue, 1)", rec.itemType, rec.number)
	}
}

// Pressing `o` on the PRs tab opens the highlighted PR with the pr subcommand.
func TestOpenInBrowserFromListPR(t *testing.T) {
	rec := withStubOpen(t)
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab}) // switch to PRs tab (#2)

	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	cmd()

	if !rec.called {
		t.Fatal("o did not invoke ghOpenInBrowser")
	}
	if rec.itemType != "pr" || rec.number != 2 {
		t.Errorf("got (%q, %d), want (pr, 2)", rec.itemType, rec.number)
	}
}

// Pressing `o` in the detail view opens the item shown in the detail view.
func TestOpenInBrowserFromDetail(t *testing.T) {
	rec := withStubOpen(t)
	m := openDetailWithBody(t, "body", 80, 24) // issue #1 detail open
	var tm tea.Model = m

	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	cmd()

	if !rec.called {
		t.Fatal("o did not invoke ghOpenInBrowser")
	}
	if rec.itemType != "issue" || rec.number != 1 {
		t.Errorf("got (%q, %d), want (issue, 1)", rec.itemType, rec.number)
	}
}

// Pressing `o` on an empty list is a safe no-op (no panic, no gh call).
func TestOpenInBrowserEmptyListNoOp(t *testing.T) {
	rec := withStubOpen(t)
	m := newModel()
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{}, prs: []list.Item{}})

	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd != nil {
		cmd()
	}
	if rec.called {
		t.Error("o on an empty list should not invoke ghOpenInBrowser")
	}
}
