package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The registry must contain the four seeded palettes, in order, each with a
// non-empty Name and every semantic role defining both Light and Dark.
func TestPaletteRegistry(t *testing.T) {
	wantNames := []string{"Tokyo Night", "Catppuccin Mocha", "Dracula", "Synthwave"}
	if len(palettes) != len(wantNames) {
		t.Fatalf("registry has %d palettes, want %d", len(palettes), len(wantNames))
	}
	for i, name := range wantNames {
		if palettes[i].Name != name {
			t.Errorf("palettes[%d].Name = %q, want %q", i, palettes[i].Name, name)
		}
	}

	for _, p := range palettes {
		if p.Name == "" {
			t.Error("palette with empty Name")
		}
		roles := map[string][2]string{
			"Accent":          {p.Accent.Light, p.Accent.Dark},
			"Number":          {p.Number.Light, p.Number.Dark},
			"Muted":           {p.Muted.Light, p.Muted.Dark},
			"DiffAdd":         {p.DiffAdd.Light, p.DiffAdd.Dark},
			"DiffDel":         {p.DiffDel.Light, p.DiffDel.Dark},
			"DiffPath":        {p.DiffPath.Light, p.DiffPath.Dark},
			"Highlight":       {p.Highlight.Light, p.Highlight.Dark},
			"Text":            {p.Text.Light, p.Text.Dark},
			"MatchBg":         {p.MatchBg.Light, p.MatchBg.Dark},
			"MatchActiveText": {p.MatchActiveText.Light, p.MatchActiveText.Dark},
		}
		for role, lv := range roles {
			if lv[0] == "" {
				t.Errorf("%s: %s missing Light value", p.Name, role)
			}
			if lv[1] == "" {
				t.Errorf("%s: %s missing Dark value", p.Name, role)
			}
		}
	}
}

// The default palette is Tokyo Night and its dark values match the historical
// hardcoded literals, so the refactor doesn't drift the default appearance.
func TestDefaultPaletteIsTokyoNight(t *testing.T) {
	if defaultPalette.Name != "Tokyo Night" {
		t.Fatalf("defaultPalette = %q, want Tokyo Night", defaultPalette.Name)
	}
	if palettes[0].Name != "Tokyo Night" {
		t.Errorf("first registry palette = %q, want Tokyo Night", palettes[0].Name)
	}
	want := map[string]string{
		"accent":          "#7aa2f7",
		"muted":           "#565f89",
		"diffAdd":         "#9ece6a",
		"diffDel":         "#f7768e",
		"diffPath":        "#bb9af7",
		"highlight":       "#e0af68",
		"text":            "#c0caf5",
		"matchBg":         "#3b4261",
		"matchActiveText": "#1a1b26",
	}
	got := map[string]string{
		"accent":          tokyoNight.Accent.Dark,
		"muted":           tokyoNight.Muted.Dark,
		"diffAdd":         tokyoNight.DiffAdd.Dark,
		"diffDel":         tokyoNight.DiffDel.Dark,
		"diffPath":        tokyoNight.DiffPath.Dark,
		"highlight":       tokyoNight.Highlight.Dark,
		"text":            tokyoNight.Text.Dark,
		"matchBg":         tokyoNight.MatchBg.Dark,
		"matchActiveText": tokyoNight.MatchActiveText.Dark,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("Tokyo Night %s dark = %q, want %q", k, got[k], w)
		}
	}
}

// applyPalette reassigns the package-level color globals to the given palette's
// values, and activePaletteName reports the applied palette. Restore the
// default afterwards so other tests aren't affected by the global mutation.
func TestApplyPaletteSetsGlobals(t *testing.T) {
	t.Cleanup(func() { applyPalette(defaultPalette) })

	applyPalette(dracula)
	if accentColor != dracula.Accent {
		t.Errorf("accentColor = %v, want %v", accentColor, dracula.Accent)
	}
	if matchActiveTextColor != dracula.MatchActiveText {
		t.Errorf("matchActiveTextColor = %v, want %v", matchActiveTextColor, dracula.MatchActiveText)
	}
	if got := activePaletteName(); got != "Dracula" {
		t.Errorf("activePaletteName() = %q, want Dracula", got)
	}

	applyPalette(tokyoNight)
	if got := activePaletteName(); got != "Tokyo Night" {
		t.Errorf("activePaletteName() = %q, want Tokyo Night", got)
	}
}

// applyPalette must also rebuild the pre-computed styles that bake in colors
// (diff/detail/number styles), so a live theme switch actually changes the diff
// view, detail search highlight, and list number prefix — not just the
// AdaptiveColor globals.
func TestApplyPaletteRebuildsStyles(t *testing.T) {
	t.Cleanup(func() { applyPalette(defaultPalette) })

	applyPalette(tokyoNight)
	if diffAddStyle.GetForeground() != lipgloss.AdaptiveColor(tokyoNight.DiffAdd) {
		t.Errorf("diffAddStyle fg = %v, want %v", diffAddStyle.GetForeground(), tokyoNight.DiffAdd)
	}

	applyPalette(dracula)
	if diffAddStyle.GetForeground() != lipgloss.AdaptiveColor(dracula.DiffAdd) {
		t.Errorf("diffAddStyle not rebuilt: fg = %v, want %v", diffAddStyle.GetForeground(), dracula.DiffAdd)
	}
	if numberStyle.GetForeground() != lipgloss.AdaptiveColor(dracula.Accent) {
		t.Errorf("numberStyle not rebuilt: fg = %v, want %v", numberStyle.GetForeground(), dracula.Accent)
	}
	if detailNumberStyle.GetForeground() != lipgloss.AdaptiveColor(dracula.Number) {
		t.Errorf("detailNumberStyle not rebuilt: fg = %v, want %v", detailNumberStyle.GetForeground(), dracula.Number)
	}
	if detailActiveMatchStyle.GetBackground() != lipgloss.AdaptiveColor(dracula.Highlight) {
		t.Errorf("detailActiveMatchStyle not rebuilt: bg = %v, want %v", detailActiveMatchStyle.GetBackground(), dracula.Highlight)
	}
}

// activePaletteName falls back to the default palette's name when the live
// globals don't match any registered palette (e.g. colors set outside the
// registry). This exercises the registry-miss branch the other tests skip by
// only ever applying registered palettes.
func TestActivePaletteNameRegistryMissFallsBack(t *testing.T) {
	t.Cleanup(func() { applyPalette(defaultPalette) })

	// Set the accent to a value no registered palette uses, leaving the globals
	// in a state that matches no palette in the registry. Guard the write with
	// paletteMu, honoring the same contract production writers (applyPalette) use,
	// so the assignment stays race-free even if a future test parallelizes.
	paletteMu.Lock()
	accentColor = lipgloss.AdaptiveColor{Light: "#010203", Dark: "#040506"}
	paletteMu.Unlock()
	if got := activePaletteName(); got != defaultPalette.Name {
		t.Errorf("activePaletteName() with unregistered globals = %q, want default %q", got, defaultPalette.Name)
	}
}

func TestPaletteByNameAndIndex(t *testing.T) {
	if p, ok := paletteByName("Dracula"); !ok || p.Name != "Dracula" {
		t.Errorf("paletteByName(Dracula) = %v, %v", p, ok)
	}
	if _, ok := paletteByName("Nope"); ok {
		t.Error("paletteByName(Nope) should not be found")
	}
	if i := paletteIndex("Dracula"); i != 2 {
		t.Errorf("paletteIndex(Dracula) = %d, want 2", i)
	}
	if i := paletteIndex("Nope"); i != 0 {
		t.Errorf("paletteIndex(Nope) = %d, want 0 (default)", i)
	}
}
