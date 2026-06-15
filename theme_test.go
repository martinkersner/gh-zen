package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Every active semantic-color GLOBAL must define a Light value distinct from its
// Dark value, so the runtime dark/light switch (AdaptiveColor resolving against
// the terminal background) actually has something to switch to — identical
// Light/Dark would make a theme switch a silent no-op.
//
// This is the unique assertion theme_test contributes: the Light/Dark presence
// and the Tokyo-Night dark-hex literals are already pinned by palette_test.go
// (TestPaletteRegistry + TestDefaultPaletteIsTokyoNight), so only the Light!=Dark
// identity check on the applied globals is kept here to avoid duplication.
func TestPaletteLightDistinctFromDark(t *testing.T) {
	cases := []struct {
		name string
		c    lipgloss.AdaptiveColor
	}{
		{"accent", accentColor},
		{"muted", mutedColor},
		{"diffAdd", diffAddColor},
		{"diffDel", diffDelColor},
		{"diffPath", diffPathColor},
		{"highlight", highlightColor},
		{"text", textColor},
		{"matchBg", matchBgColor},
		{"matchActiveText", matchActiveTextColor},
	}
	for _, tc := range cases {
		if tc.c.Light == "" || tc.c.Dark == "" {
			t.Errorf("%s: missing Light/Dark value (Light=%q Dark=%q)", tc.name, tc.c.Light, tc.c.Dark)
		}
		if tc.c.Light == tc.c.Dark {
			t.Errorf("%s: Light and Dark are identical (%q); a theme switch would be a no-op", tc.name, tc.c.Light)
		}
	}
}
