// Command gojo is a fullscreen terminal UI for jj (Jujutsu VCS).
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

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

	// Ask the terminal to report OS dark/light scheme changes as DSR replies
	// (xterm mode 2031; supported by kitty, Ghostty, VTE, Contour, …). The UI
	// handles Bubble Tea's parsed scheme events and re-themes (see
	// internal/ui/darkmode.go). Terminals without
	// support ignore the mode silently.
	fmt.Fprint(os.Stdout, "\x1b[?2031h")
	defer fmt.Fprint(os.Stdout, "\x1b[?2031l")

	p := tea.NewProgram(
		ui.NewModel(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
