package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// listModel returns a sized model in list mode with one issue and one PR loaded.
func listModel(t *testing.T) tea.Model {
	t.Helper()
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "a", type_: "issue"}},
		prs:    []list.Item{item{number: 2, title: "b", type_: "pr"}},
	})
	return tm
}

// Pressing `?` in list mode opens the help overlay; the view switches to render
// it, listing the list-mode shortcuts with descriptions.
func TestHelpOverlayOpensFromList(t *testing.T) {
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})

	mm := tm.(model)
	if !mm.showHelp {
		t.Fatal("? did not open the help overlay")
	}
	view := mm.View()
	for _, want := range []string{"Keyboard shortcuts", "quit", "filter", "open"} {
		if !strings.Contains(view, want) {
			t.Errorf("help overlay missing %q: %q", want, view)
		}
	}
}

// `?` toggles the overlay closed again.
func TestHelpOverlayToggleClosesWithQuestionMark(t *testing.T) {
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !tm.(model).showHelp {
		t.Fatal("setup: overlay should be open")
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if tm.(model).showHelp {
		t.Error("second ? should close the overlay")
	}
}

// esc dismisses the overlay.
func TestHelpOverlayClosesWithEsc(t *testing.T) {
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tm.(model).showHelp {
		t.Error("esc should close the overlay")
	}
}

// While the overlay is open it swallows other keys so they don't act on the
// obscured view (e.g. tab must not switch tabs underneath).
func TestHelpOverlaySwallowsKeys(t *testing.T) {
	tm := listModel(t)
	before := tm.(model).activeTab
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm := tm.(model)
	if !mm.showHelp {
		t.Error("overlay should stay open after an unrelated key")
	}
	if mm.activeTab != before {
		t.Error("tab should not switch tabs while the overlay is open")
	}
}

// The overlay reflects the current view: opened from a PR detail it lists the
// detail shortcuts (scroll, the diff toggle), not the list-mode ones.
func TestHelpOverlayReflectsDetailMode(t *testing.T) {
	m := newModel()
	m.prList.SetItems([]list.Item{item{number: 7, title: "p", body: "b", type_: "pr"}})
	m.activeTab = tabPRs
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !tm.(model).detailOpen {
		t.Fatal("setup: detail did not open")
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})

	view := tm.(model).View()
	for _, want := range []string{"back", "scroll", "show diff"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail help overlay missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "switch tab") {
		t.Errorf("detail help overlay leaked list-mode shortcut: %q", view)
	}
}

// While filtering, `?` is a literal filter character and must not open the
// overlay (the inline filter hint stays usable).
func TestHelpOverlayNotOpenedWhileFiltering(t *testing.T) {
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	mm := tm.(model)
	if !mm.currentList().SettingFilter() {
		t.Fatal("setup: not in filter mode")
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if tm.(model).showHelp {
		t.Error("? while filtering must not open the overlay")
	}
}
