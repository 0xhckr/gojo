// Command gojo is a fullscreen terminal UI for jj (Jujutsu VCS).
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/ui"
)

func main() {
	// Detect the terminal background once, before entering the alt screen, so
	// the detection query doesn't corrupt the TUI. AdaptiveColor and the diff
	// syntax-highlighting theme then read this cached value.
	_ = lipgloss.HasDarkBackground()

	p := tea.NewProgram(
		ui.NewModel(),
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
