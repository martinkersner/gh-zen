package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// shortcut is a single keyboard shortcut: the key(s) that trigger it and a short
// description. The set of shortcuts valid in the current view/mode is the single
// source of truth for both the status bar's `? help` affordance and the help
// overlay (see currentShortcuts).
type shortcut struct {
	keys string
	desc string
}

// helpHint is the compact affordance shown in the status bar in place of the
// full inline shortcut list. Pressing `?` opens the overlay (see renderHelp).
const helpHint = "? help"

// currentShortcuts returns the shortcuts valid in the current view/mode, in the
// order they should be displayed. It is the single source of truth consumed by
// the help overlay so the listed shortcuts always match the active mode.
//
// Filtering (list) and in-detail search are deliberately excluded: in those
// modes `?` is a literal input character, so the overlay can't be opened. Their
// inline hints stay in renderStatusBar.
func (m model) currentShortcuts() []shortcut {
	if m.detailOpen {
		s := []shortcut{
			{"q/esc", "back"},
			{"ctrl+n/ctrl+p", "scroll"},
		}
		// PRs gain a key to toggle between the body and the diff view.
		if m.detailItem != nil && m.detailItem.type_ == "pr" {
			verb := "show diff"
			if m.detailShowDiff {
				verb = "show body"
			}
			s = append(s, shortcut{"d", verb})
		}
		s = append(s,
			shortcut{"/", "search"},
			shortcut{"r", "refresh"},
			shortcut{"?", "toggle help"},
		)
		return s
	}

	return []shortcut{
		{"q/esc", "quit"},
		{"tab", "switch tab"},
		{"/", "filter"},
		{"enter", "open"},
		{"r", "refresh"},
		{"?", "toggle help"},
	}
}

// renderHelp renders the shortcuts overlay: a bordered box listing every
// shortcut valid in the current view/mode with its description. It is shown
// centered over the screen while m.showHelp is set; `?` or esc dismisses it.
func (m model) renderHelp() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))

	shortcuts := m.currentShortcuts()

	// Width the key column to the longest key so descriptions align.
	keyWidth := 0
	for _, s := range shortcuts {
		if w := lipgloss.Width(s.keys); w > keyWidth {
			keyWidth = w
		}
	}

	var rows []string
	rows = append(rows, titleStyle.Render("Keyboard shortcuts"), "")
	for _, s := range shortcuts {
		key := keyStyle.Width(keyWidth).Render(s.keys)
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, key, "  ", descStyle.Render(s.desc)))
	}
	rows = append(rows, "", descStyle.Render("? or esc to close"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#565f89")).
		Padding(0, 2).
		Render(strings.Join(rows, "\n"))

	// Center the box over the available area when the terminal size is known.
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}
