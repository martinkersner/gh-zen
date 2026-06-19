package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// closeChoice is one selectable option in the close-issue dialog (the `c` key).
// label is the user-facing text; reason is the GitHub state reason passed to
// `gh issue close --reason` (empty for the Cancel option, which just dismisses).
type closeChoice struct {
	label  string
	reason string
}

// closeChoices is the ordered set of options shown in the close dialog: close as
// completed, close as not planned, or cancel (no change). The cursor moves over
// these; selecting one with a non-empty reason fires the close mutation, Cancel
// (the last entry) just dismisses. It is the single source of truth for the
// dialog's rows and the index-to-reason mapping.
var closeChoices = []closeChoice{
	{label: "Completed", reason: closeReasonCompleted},
	{label: "Not planned", reason: closeReasonNotPlanned},
	{label: "Cancel", reason: ""},
}

// cmdCloseIssue closes the given issue via the GitHub API with the supplied state
// reason (see ghCloseIssue), returning a closeIssueResultMsg with the outcome. It
// is the mutation counterpart to the read-path cmds (cmdFetchBody etc.) and runs
// inside a tea.Cmd off the main loop. number is carried back on the result so the
// handler can reflect the closed state on the matching item.
func cmdCloseIssue(number int, reason string) tea.Cmd {
	return func() tea.Msg {
		err := ghCloseIssue(number, reason)
		return closeIssueResultMsg{number: number, err: err}
	}
}

// renderCloseDialog renders the close-issue confirmation dialog: a centered,
// bordered box asking how to close the focused issue, with the highlighted choice
// marked. It mirrors the help / settings overlays' centered full-screen pattern
// (see renderHelp / renderSettings) so it reads as a focused modal rather than
// text bleeding through the obscured view.
func (m model) renderCloseDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	descStyle := lipgloss.NewStyle().Foreground(textColor)
	mutedStyle := lipgloss.NewStyle().Foreground(mutedColor)

	var rows []string
	rows = append(rows, titleStyle.Render("Close issue"), "")
	if m.closeDialogItem != nil {
		rows = append(rows, mutedStyle.Render(m.closeDialogItem.Title()), "")
	}

	for i, c := range closeChoices {
		marker := "  "
		name := descStyle.Render(c.label)
		if i == m.closeCursor {
			marker = "> "
			name = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(c.label)
		}
		rows = append(rows, mutedStyle.Render(marker)+name)
	}

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
