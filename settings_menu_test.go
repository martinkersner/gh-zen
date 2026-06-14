package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// restorePalette returns the globals to the default after a test mutates them
// via the live preview, so other tests aren't affected.
func restorePalette(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { applyPalette(defaultPalette) })
}

// Pressing `t` in list mode opens the palette picker, seeded on the active
// palette, and the view renders the menu plus a preview.
func TestSettingsOpensFromList(t *testing.T) {
	tempConfigDir(t)
	restorePalette(t)
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})

	mm := tm.(model)
	if !mm.showSettings {
		t.Fatal("t did not open the settings overlay")
	}
	if mm.settingsCursor != paletteIndex(defaultPalette.Name) {
		t.Errorf("cursor = %d, want default palette index", mm.settingsCursor)
	}
	view := mm.View()
	for _, want := range []string{"Theme", "Preview", "Tokyo Night", "Dracula"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings overlay missing %q", want)
		}
	}
}

// Moving the cursor live-previews the highlighted palette by applying it to the
// globals.
func TestSettingsCursorLivePreviews(t *testing.T) {
	tempConfigDir(t)
	restorePalette(t)
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown})

	mm := tm.(model)
	if mm.settingsCursor != 1 {
		t.Fatalf("cursor = %d, want 1", mm.settingsCursor)
	}
	if accentColor != palettes[1].Accent {
		t.Errorf("preview did not apply palettes[1]: accentColor = %v", accentColor)
	}
}

// Esc cancels: it restores the palette active when the menu opened and closes.
func TestSettingsEscRestores(t *testing.T) {
	tempConfigDir(t)
	restorePalette(t)
	tm := listModel(t)
	before := accentColor

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown}) // preview palettes[1]
	if accentColor == before {
		t.Fatal("setup: preview should have changed the accent")
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})

	mm := tm.(model)
	if mm.showSettings {
		t.Error("esc should close the overlay")
	}
	if accentColor != before {
		t.Errorf("esc did not restore the original palette: accentColor = %v, want %v", accentColor, before)
	}
}

// Enter keeps the previewed palette and persists it.
func TestSettingsEnterPersists(t *testing.T) {
	restorePalette(t)
	tempConfigDir(t)
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown}) // select palettes[1]
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	mm := tm.(model)
	if mm.showSettings {
		t.Error("enter should close the overlay")
	}
	if accentColor != palettes[1].Accent {
		t.Error("enter should keep the previewed palette")
	}
	if p := loadPalette(); p.Name != palettes[1].Name {
		t.Errorf("persisted palette = %q, want %q", p.Name, palettes[1].Name)
	}
}

// The overlay swallows unrelated keys so they don't act on the obscured list.
func TestSettingsSwallowsKeys(t *testing.T) {
	restorePalette(t)
	tm := listModel(t)
	before := tm.(model).activeTab

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})

	mm := tm.(model)
	if !mm.showSettings {
		t.Error("overlay should stay open after an unrelated key")
	}
	if mm.activeTab != before {
		t.Error("tab should not switch tabs while the overlay is open")
	}
}

// While filtering, `t` is a literal filter character and must not open the menu.
func TestSettingsNotOpenedWhileFiltering(t *testing.T) {
	restorePalette(t)
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	fm := tm.(model)
	if !fm.currentList().SettingFilter() {
		t.Fatal("setup: not in filter mode")
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if tm.(model).showSettings {
		t.Error("t while filtering must not open the settings overlay")
	}
}
