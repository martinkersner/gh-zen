package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderSettings renders the palette-picker overlay: a bordered box listing the
// registered palettes (the highlighted one marked) beside a live preview of
// sample text rendered with the currently-active palette. Because moving the
// selection calls applyPalette, the preview re-renders in the chosen palette's
// colors, so the user sees the real appearance before committing. Mirrors the
// help overlay's centered full-screen pattern (see renderHelp).
func (m model) renderSettings() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	descStyle := lipgloss.NewStyle().Foreground(textColor)
	mutedStyle := lipgloss.NewStyle().Foreground(mutedColor)

	var rows []string
	rows = append(rows, titleStyle.Render("Theme"), "")

	for i, p := range palettes {
		marker := "  "
		name := p.Name
		if i == m.settingsCursor {
			marker = "> "
			name = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(name)
		} else {
			name = descStyle.Render(name)
		}
		rows = append(rows, mutedStyle.Render(marker)+name)
	}

	rows = append(rows, "", titleStyle.Render("Preview"), "")
	rows = append(rows, m.settingsPreview())
	rows = append(rows, "", mutedStyle.Render("↑/↓ move · enter select · esc cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(mutedColor).
		Padding(0, 2).
		Render(strings.Join(rows, "\n"))

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}

// settingsPreview renders sample text exercising every semantic role using the
// active (previewed) palette's colors, so the picker shows the actual
// appearance: active/inactive tabs, diff add/del lines, a diff file path, a
// search match, an accent title, and muted status text.
func (m model) settingsPreview() string {
	activeTab := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("Issues (12)")
	inactiveTab := lipgloss.NewStyle().Foreground(mutedColor).Render("PRs (3)")
	tabs := lipgloss.JoinHorizontal(lipgloss.Left, activeTab, "  ", inactiveTab)

	title := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("#42 Sample issue title")
	path := lipgloss.NewStyle().Foreground(diffPathColor).Render("internal/theme.go")
	add := lipgloss.NewStyle().Foreground(diffAddColor).Render("+ added line")
	del := lipgloss.NewStyle().Foreground(diffDelColor).Render("- removed line")

	match := lipgloss.NewStyle().
		Background(matchBgColor).
		Foreground(matchActiveTextColor).
		Render(" search match ")
	matchLine := lipgloss.NewStyle().Foreground(textColor).Render("a ") +
		match + lipgloss.NewStyle().Foreground(textColor).Render(" here")

	status := lipgloss.NewStyle().Foreground(mutedColor).Render("loading… · ? help")

	return strings.Join([]string{tabs, title, path, add, del, matchLine, status}, "\n")
}
