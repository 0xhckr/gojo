// Command gojo is a fullscreen terminal UI for jj (Jujutsu VCS).
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"gojo/internal/ui"
)

// version is set at build time via ldflags (e.g. by goreleaser). It defaults
// to "dev" for `go run` / `go build` without flags.
var version = "dev"

//go:embed CHANGELOG.md
var changelog string

func main() {
	handled, err := handleFlags(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return
		}
		os.Exit(2)
	}
	if handled {
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

func handleFlags(args []string, stdout, stderr io.Writer) (bool, error) {
	flags := flag.NewFlagSet("gojo", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var showVersion bool
	flags.BoolVar(&showVersion, "version", false, "print version and exit")
	flags.BoolVar(&showVersion, "v", false, "print version and exit")
	showChangelog := flags.Bool("changelog", false, "print changelog and exit")
	if err := flags.Parse(args); err != nil {
		return true, err
	}

	if showVersion {
		fmt.Fprintln(stdout, "gojo", version)
		return true, nil
	}
	if *showChangelog {
		fmt.Fprint(stdout, changelog)
		return true, nil
	}
	return false, nil
}
