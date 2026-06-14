package main

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// Palette is a complete set of semantic-role colors for the TUI. Each role is an
// AdaptiveColor holding a Light and Dark value resolved at render time against
// the terminal background (lipgloss.HasDarkBackground), so dark/light switching
// works per palette with no plumbing through the model. Name is the display
// label shown in the settings menu.
type Palette struct {
	Name            string
	Accent          lipgloss.AdaptiveColor // active tabs, titles, number prefix, key hints, diff meta lines.
	Muted           lipgloss.AdaptiveColor // inactive tabs, status bar, borders, diff context.
	DiffAdd         lipgloss.AdaptiveColor // added lines in diffs.
	DiffDel         lipgloss.AdaptiveColor // removed lines in diffs.
	DiffPath        lipgloss.AdaptiveColor // file paths in diff headers.
	Highlight       lipgloss.AdaptiveColor // active diff file, active search match background.
	Text            lipgloss.AdaptiveColor // help/description text, search match text.
	MatchBg         lipgloss.AdaptiveColor // search match background.
	MatchActiveText lipgloss.AdaptiveColor // active search match text on the highlight background.
}

// paletteMu guards the package-level palette color vars and the derived styles
// (diffAddStyle … numberStyle) below. applyPalette mutates them from Update
// (palette-picker cursor move / enter / esc); the same globals/styles are read
// on the render path. bubbletea serializes Update→View on its event loop and
// glamour renders synchronously, so the two have not been observed to contend —
// but under the Go memory model an unsynchronized read/write across goroutines
// is a data race (markdown.go already notes View may run off the Update
// goroutine). The write path (applyPalette → rebuildThemeStyles) takes the write
// lock; the read path takes the read lock once at the View boundary, which is
// the only render entry point that can run concurrently with a palette switch.
// Every other reader (refreshDiffView, colorizeDiff, etc.) is reached either
// from Update — the same goroutine as applyPalette — or transitively from View
// under that same RLock, so none of them re-lock (a nested RLock could deadlock
// against a waiting writer). RLock is held across the whole render rather than
// snapshotting each global: the reads are scattered across many call sites,
// holding the read lock is cheap, and writes only originate from the event loop
// so an RLock-held render never actually blocks a writer in practice. Issue #115.
var paletteMu sync.RWMutex

// Package-level active colors. These are referenced directly across the render
// code (detail.go, fetch.go, diff.go, help.go, statusbar.go, row.go, view.go,
// tabs.go). applyPalette reassigns them so a theme switch happens in one place
// without re-plumbing every call site. Reads/writes are guarded by paletteMu
// (see above). The initial values are the Tokyo Night palette (the historical
// default), preserved exactly so the default dark appearance does not drift.
var (
	accentColor          = tokyoNight.Accent
	mutedColor           = tokyoNight.Muted
	diffAddColor         = tokyoNight.DiffAdd
	diffDelColor         = tokyoNight.DiffDel
	diffPathColor        = tokyoNight.DiffPath
	highlightColor       = tokyoNight.Highlight
	textColor            = tokyoNight.Text
	matchBgColor         = tokyoNight.MatchBg
	matchActiveTextColor = tokyoNight.MatchActiveText
)

// applyPalette makes p the active palette by reassigning the package-level color
// vars and rebuilding the pre-computed styles that captured them. Render
// functions read those globals/styles, so this is a true live switch.
func applyPalette(p Palette) {
	paletteMu.Lock()
	defer paletteMu.Unlock()
	accentColor = p.Accent
	mutedColor = p.Muted
	diffAddColor = p.DiffAdd
	diffDelColor = p.DiffDel
	diffPathColor = p.DiffPath
	highlightColor = p.Highlight
	textColor = p.Text
	matchBgColor = p.MatchBg
	matchActiveTextColor = p.MatchActiveText
	rebuildThemeStyles()
}

// rebuildThemeStyles regenerates the package-level lipgloss styles that bake in
// color values at construction time (diff styles in diff.go, detail search
// styles in detail.go, the list number-prefix style in row.go). These are not
// AdaptiveColor-only — they're full styles — so a palette switch must rebuild
// them; otherwise the diff view, detail search highlight, and list number
// prefix would keep rendering in the palette active at program start.
//
// Callers must hold paletteMu for writing (applyPalette does); the sole
// exception is init, which runs before any goroutine could read the styles.
func rebuildThemeStyles() {
	diffAddStyle = lipgloss.NewStyle().Foreground(diffAddColor)
	diffDelStyle = lipgloss.NewStyle().Foreground(diffDelColor)
	diffMetaStyle = lipgloss.NewStyle().Foreground(accentColor)
	diffPathStyle = lipgloss.NewStyle().Bold(true).Foreground(diffPathColor)
	diffMutedStyle = lipgloss.NewStyle().Foreground(mutedColor)
	diffActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(highlightColor)

	detailMatchStyle = lipgloss.NewStyle().Background(matchBgColor).Foreground(textColor)
	detailActiveMatchStyle = lipgloss.NewStyle().Background(highlightColor).Foreground(matchActiveTextColor).Bold(true)

	numberStyle = lipgloss.NewStyle().Foreground(accentColor).Inline(true)
}

// init ensures the derived styles are populated for the default palette before
// any render runs, even on code paths that don't go through applyPalette (e.g.
// tests constructing styles directly).
func init() {
	rebuildThemeStyles()
}

// tokyoNight is the historical default. Dark values are the original Tokyo Night
// literals (preserved exactly); Light values are the Tokyo Night Day
// counterparts.
var tokyoNight = Palette{
	Name:            "Tokyo Night",
	Accent:          lipgloss.AdaptiveColor{Light: "#2e7de9", Dark: "#7aa2f7"},
	Muted:           lipgloss.AdaptiveColor{Light: "#8990b3", Dark: "#565f89"},
	DiffAdd:         lipgloss.AdaptiveColor{Light: "#587539", Dark: "#9ece6a"},
	DiffDel:         lipgloss.AdaptiveColor{Light: "#f52a65", Dark: "#f7768e"},
	DiffPath:        lipgloss.AdaptiveColor{Light: "#9854f1", Dark: "#bb9af7"},
	Highlight:       lipgloss.AdaptiveColor{Light: "#8c6c3e", Dark: "#e0af68"},
	Text:            lipgloss.AdaptiveColor{Light: "#3760bf", Dark: "#c0caf5"},
	MatchBg:         lipgloss.AdaptiveColor{Light: "#b7c1e3", Dark: "#3b4261"},
	MatchActiveText: lipgloss.AdaptiveColor{Light: "#e1e2e7", Dark: "#1a1b26"},
}

// The three palettes below are dark-first themes. The issue specifies their
// canonical (dark) values; the Light values are pragmatic fallbacks chosen so
// each role stays legible in a light terminal (reusing Tokyo Night Day's light
// values, which already cover every role), not official light variants of these
// themes.
var tokyoNightLightFallback = tokyoNight // alias for documenting the fallback source

// catppuccinMocha — canonical Mocha dark palette.
// base #1e1e2e, text #cdd6f4, blue #89b4fa, green #a6e3a1, red #f38ba8,
// mauve #cba6f7, yellow #f9e2af, surface2 #585b70, surface1 #45475a.
var catppuccinMocha = Palette{
	Name:            "Catppuccin Mocha",
	Accent:          lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Accent.Light, Dark: "#89b4fa"},
	Muted:           lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Muted.Light, Dark: "#585b70"},
	DiffAdd:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffAdd.Light, Dark: "#a6e3a1"},
	DiffDel:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffDel.Light, Dark: "#f38ba8"},
	DiffPath:        lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffPath.Light, Dark: "#cba6f7"},
	Highlight:       lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Highlight.Light, Dark: "#f9e2af"},
	Text:            lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Text.Light, Dark: "#cdd6f4"},
	MatchBg:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.MatchBg.Light, Dark: "#45475a"},
	MatchActiveText: lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.MatchActiveText.Light, Dark: "#1e1e2e"},
}

// dracula — canonical Dracula dark palette.
// bg #282a36, fg #f8f8f2, purple #bd93f9, green #50fa7b, red #ff5555,
// pink #ff79c6, yellow #f1fa8c, comment #6272a4, currentLine #44475a.
var dracula = Palette{
	Name:            "Dracula",
	Accent:          lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Accent.Light, Dark: "#bd93f9"},
	Muted:           lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Muted.Light, Dark: "#6272a4"},
	DiffAdd:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffAdd.Light, Dark: "#50fa7b"},
	DiffDel:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffDel.Light, Dark: "#ff5555"},
	DiffPath:        lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffPath.Light, Dark: "#ff79c6"},
	Highlight:       lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Highlight.Light, Dark: "#f1fa8c"},
	Text:            lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Text.Light, Dark: "#f8f8f2"},
	MatchBg:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.MatchBg.Light, Dark: "#44475a"},
	MatchActiveText: lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.MatchActiveText.Light, Dark: "#282a36"},
}

// synthwave — an approximation of the synthwave/neon aesthetic (neon on deep
// purple). Not an official standard; hexes chosen to read as neon-on-dark.
// bg #2b213a, neon-pink #ff7edb, green #72f1b8, red/magenta #fe4450,
// purple #c792ea, yellow #fede5d, text #f0eff1, muted #848bbd, matchBg #463465.
var synthwave = Palette{
	Name:            "Synthwave",
	Accent:          lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Accent.Light, Dark: "#ff7edb"},
	Muted:           lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Muted.Light, Dark: "#848bbd"},
	DiffAdd:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffAdd.Light, Dark: "#72f1b8"},
	DiffDel:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffDel.Light, Dark: "#fe4450"},
	DiffPath:        lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.DiffPath.Light, Dark: "#c792ea"},
	Highlight:       lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Highlight.Light, Dark: "#fede5d"},
	Text:            lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.Text.Light, Dark: "#f0eff1"},
	MatchBg:         lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.MatchBg.Light, Dark: "#463465"},
	MatchActiveText: lipgloss.AdaptiveColor{Light: tokyoNightLightFallback.MatchActiveText.Light, Dark: "#2b213a"},
}

// palettes is the registry in stable display order for the settings menu. The
// first entry (Tokyo Night) is the default.
var palettes = []Palette{tokyoNight, catppuccinMocha, dracula, synthwave}

// defaultPalette is the active palette when no valid persisted choice exists.
var defaultPalette = tokyoNight

// paletteByName returns the registered palette with the given display name and
// whether it was found.
func paletteByName(name string) (Palette, bool) {
	for _, p := range palettes {
		if p.Name == name {
			return p, true
		}
	}
	return Palette{}, false
}

// paletteIndex returns the index of the palette with the given name in the
// registry, or 0 (the default) if not found.
func paletteIndex(name string) int {
	for i, p := range palettes {
		if p.Name == name {
			return i
		}
	}
	return 0
}

// activePaletteName returns the display name of the registered palette whose
// colors are currently applied to the globals, or the default palette's name if
// none match (e.g. the globals were set outside the registry). Used to seed the
// settings cursor on the live palette.
func activePaletteName() string {
	paletteMu.RLock()
	defer paletteMu.RUnlock()
	for _, p := range palettes {
		if p.Accent == accentColor && p.Text == textColor && p.DiffAdd == diffAddColor {
			return p.Name
		}
	}
	return defaultPalette.Name
}
