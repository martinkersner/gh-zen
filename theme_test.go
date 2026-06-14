package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Every semantic palette color must define both a Light and a Dark value so the
// runtime dark/light switch (AdaptiveColor resolving against the terminal
// background) actually has something to switch to. A missing value would
// silently render as an empty color in one mode.
func TestPaletteHasLightAndDark(t *testing.T) {
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
		if tc.c.Light == "" {
			t.Errorf("%s: missing Light value", tc.name)
		}
		if tc.c.Dark == "" {
			t.Errorf("%s: missing Dark value", tc.name)
		}
		if tc.c.Light == tc.c.Dark {
			t.Errorf("%s: Light and Dark are identical (%q); a theme switch would be a no-op", tc.name, tc.c.Light)
		}
	}
}

// The Dark values must equal the original Tokyo Night literals so this
// refactor does not drift the existing (dark) appearance. This pins each role's
// dark hex; changing one here is a deliberate signal it changed in the palette.
func TestPaletteDarkMatchesTokyoNight(t *testing.T) {
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
		"accent":          string(accentColor.Dark),
		"muted":           string(mutedColor.Dark),
		"diffAdd":         string(diffAddColor.Dark),
		"diffDel":         string(diffDelColor.Dark),
		"diffPath":        string(diffPathColor.Dark),
		"highlight":       string(highlightColor.Dark),
		"text":            string(textColor.Dark),
		"matchBg":         string(matchBgColor.Dark),
		"matchActiveText": string(matchActiveTextColor.Dark),
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s dark value = %q, want %q", name, got[name], w)
		}
	}
}
