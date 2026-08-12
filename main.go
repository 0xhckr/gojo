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

	// Ask the terminal to report OS dark/light scheme changes as DSR replies
	// (xterm mode 2031; supported by kitty, Ghostty, VTE, Contour, …). The UI
	// picks the CSI ? 997 ; 1|2 n replies out of bubbletea's unrecognized-CSI
	// messages and re-themes (see internal/ui/darkmode.go). Terminals without
	// support ignore the mode silently.
	fmt.Fprint(os.Stdout, "\x1b[?2031h")
	defer fmt.Fprint(os.Stdout, "\x1b[?2031l")

	p := tea.NewProgram(
		ui.NewModel(),
		tea.WithAltScreen(),
		tea.WithReportFocus(),
		tea.WithMouseCellMotion(),
		tea.WithMouseAllMotion(),
		// Deduplicate redundant SGR/cursor sequences in each repainted frame.
		// Scrolling changes every row, so each frame is a full-screen rewrite;
		// compressed frames are substantially smaller, which keeps slow
		// terminals (macOS Terminal.app, iTerm2 under load) from falling
		// behind into a visible top-to-bottom repaint crawl.
		tea.WithANSICompressor(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
