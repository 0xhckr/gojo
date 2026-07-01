// Command gojo is a fullscreen terminal UI for jj (Jujutsu VCS).
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/ui"
)

// version is set at build time via ldflags (e.g. by goreleaser). It defaults
// to "dev" for `go run` / `go build` without flags.
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("gojo", version)
		return
	}

	// Detect the terminal background once, before entering the alt screen, so
	// the detection query doesn't corrupt the TUI. AdaptiveColor and the diff
	// syntax-highlighting theme then read this cached value.
	_ = lipgloss.HasDarkBackground()

	p := tea.NewProgram(
		ui.NewModel(),
		tea.WithAltScreen(),
		tea.WithReportFocus(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
