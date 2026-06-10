package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	forceColorProfile()

	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func forceColorProfile() {
	termenv := os.Getenv("TERM_PROGRAM")
	switch termenv {
	case "iTerm.app", "Apple_Terminal", "ghostty", "Hyper", "vscode":
		os.Setenv("LIPGLOSS_PROFILE", "truecolor")
	default:
		if os.Getenv("COLORTERM") == "truecolor" {
			os.Setenv("LIPGLOSS_PROFILE", "truecolor")
		} else {
			os.Setenv("LIPGLOSS_PROFILE", "256")
		}
	}
}
