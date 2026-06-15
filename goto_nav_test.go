package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// In list mode, `G` jumps the selection to the last item and `g` back to the
// first.
func TestListGotoFirstLast(t *testing.T) {
	m := newModel()
	items := []list.Item{
		item{number: 1, title: "first", type_: "issue"},
		item{number: 2, title: "second", type_: "issue"},
		item{number: 3, title: "third", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if idx := tm.(model).issueList.Index(); idx != 0 {
		t.Fatalf("setup: expected selection at 0, got %d", idx)
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if idx := tm.(model).issueList.Index(); idx != 2 {
		t.Errorf("G: want last index 2, got %d", idx)
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if idx := tm.(model).issueList.Index(); idx != 0 {
		t.Errorf("g: want first index 0, got %d", idx)
	}
}

// `g`/`G` on an empty list are safe no-ops (no panic from Select on an empty
// list).
func TestListGotoEmpty(t *testing.T) {
	m := newModel()
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
}

// In the detail view, `G` scrolls the viewport to the bottom and `g` back to the
// top.
func TestDetailGotoTopBottom(t *testing.T) {
	m := newModel()
	long := strings.Repeat("line of body text\n", 100)
	items := []list.Item{
		item{number: 7, title: "jump me", body: long, type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !tm.(model).detailViewport.AtTop() {
		t.Fatalf("setup: expected viewport at top")
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !tm.(model).detailViewport.AtBottom() {
		t.Errorf("G: want viewport at bottom, got offset=%d", tm.(model).detailViewport.YOffset)
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !tm.(model).detailViewport.AtTop() {
		t.Errorf("g: want viewport at top, got offset=%d", tm.(model).detailViewport.YOffset)
	}
}

// The help overlay lists the g/G shortcut in both list and detail modes.
func TestHelpOverlayListsGotoShortcut(t *testing.T) {
	// List mode.
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if view := tm.(model).View(); !strings.Contains(view, "g/G") {
		t.Errorf("list help overlay missing g/G shortcut: %q", view)
	}

	// Detail mode.
	m := openDetailWithBody(t, "body text", 80, 24)
	var dm tea.Model = m
	dm, _ = dm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if view := dm.(model).View(); !strings.Contains(view, "g/G") {
		t.Errorf("detail help overlay missing g/G shortcut: %q", view)
	}
}

// ctrl+g is the universal "back/cancel/quit" key: it closes the detail view,
// dismisses the help overlay, cancels an active filter, and quits from the list
// — each one line of Update logic. This single Update-level table test replaces
// four separate teatest e2e programs (one per case) that each spun up the full
// render loop to exercise that one line, cutting suite time while still covering
// every branch.
func TestCtrlGBehaviors(t *testing.T) {
	// quits reports whether running cmd yields a tea.QuitMsg.
	quits := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}

	t.Run("closes detail view", func(t *testing.T) {
		m := openDetailWithBody(t, "body text", 80, 24)
		var tm tea.Model = m
		if !tm.(model).detailOpen {
			t.Fatal("setup: detail not open")
		}
		tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if tm.(model).detailOpen {
			t.Error("ctrl+g did not close the detail view")
		}
		if quits(cmd) {
			t.Error("ctrl+g in detail should not quit")
		}
	})

	t.Run("dismisses help overlay", func(t *testing.T) {
		tm := listModel(t)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		if !tm.(model).showHelp {
			t.Fatal("setup: help overlay not open")
		}
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if tm.(model).showHelp {
			t.Error("ctrl+g did not dismiss the help overlay")
		}
	})

	t.Run("cancels active filter", func(t *testing.T) {
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		tm, _ = tm.Update(dataMsg{issues: []list.Item{
			item{number: 1, title: "alpha", type_: "issue"},
			item{number: 2, title: "beta", type_: "issue"},
		}})
		// Enter filter mode and type a query.
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		tm = typeRunes(tm, "beta")
		if tm.(model).issueList.FilterState() == list.Unfiltered {
			t.Fatal("setup: filter not active")
		}
		// ctrl+g cancels the filter, mirroring esc.
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if got := tm.(model).issueList.FilterState(); got != list.Unfiltered {
			t.Errorf("ctrl+g did not cancel filter: state = %v", got)
		}
	})

	t.Run("quits from list", func(t *testing.T) {
		tm := listModel(t)
		_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if !quits(cmd) {
			t.Error("ctrl+g in list view did not dispatch quit")
		}
	})
}
