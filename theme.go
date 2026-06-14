package main

import "github.com/charmbracelet/lipgloss"

// Central color palette for the TUI. Each entry is a semantic role, not a raw
// hex literal, so a theme tweak happens in one place. lipgloss.AdaptiveColor
// holds both a Light and a Dark value and resolves at render time against the
// terminal's detected background (lipgloss.HasDarkBackground), giving runtime
// dark/light switching with no plumbing through the model.
//
// Dark values are the existing Tokyo Night palette (preserved exactly so the
// dark appearance does not drift). Light values are the Tokyo Night Day
// counterparts.
var (
	// accent: active tabs, titles, number prefix, key hints, diff meta lines.
	accentColor = lipgloss.AdaptiveColor{Light: "#2e7de9", Dark: "#7aa2f7"}
	// muted: inactive tabs, status bar, borders, diff context.
	mutedColor = lipgloss.AdaptiveColor{Light: "#8990b3", Dark: "#565f89"}
	// diffAdd: added lines in diffs.
	diffAddColor = lipgloss.AdaptiveColor{Light: "#587539", Dark: "#9ece6a"}
	// diffDel: removed lines in diffs.
	diffDelColor = lipgloss.AdaptiveColor{Light: "#f52a65", Dark: "#f7768e"}
	// diffPath: file paths in diff headers.
	diffPathColor = lipgloss.AdaptiveColor{Light: "#9854f1", Dark: "#bb9af7"}
	// highlight: active diff file, active search match background.
	highlightColor = lipgloss.AdaptiveColor{Light: "#8c6c3e", Dark: "#e0af68"}
	// text: help/description text, search match text.
	textColor = lipgloss.AdaptiveColor{Light: "#3760bf", Dark: "#c0caf5"}
	// matchBg: search match background.
	matchBgColor = lipgloss.AdaptiveColor{Light: "#b7c1e3", Dark: "#3b4261"}
	// matchActiveText: active search match text on the highlight background.
	matchActiveTextColor = lipgloss.AdaptiveColor{Light: "#e1e2e7", Dark: "#1a1b26"}
)
