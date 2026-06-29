package ui

import "github.com/charmbracelet/lipgloss"

// The palette follows the terminal's own theme. Foregrounds use the ANSI
// 16-color palette (indices "0".."15"), so they inherit whatever colors the
// user's terminal defines — readable in both light and dark themes. Primary
// body text uses no color at all (the terminal default foreground). Only the
// few background bands are fixed tints, and those are AdaptiveColor pairs so
// they flip between light and dark backgrounds.
//
// nil means "no color" → the terminal default foreground/background.
var (
	colPurple     lipgloss.TerminalColor = lipgloss.Color("5")  // magenta — primary accent, change IDs
	colMagenta    lipgloss.TerminalColor = lipgloss.Color("13") // bright magenta — change ID prefix
	colBlue       lipgloss.TerminalColor = lipgloss.Color("4")  // author names
	colGreen      lipgloss.TerminalColor = lipgloss.Color("2")  // bookmarks, additions
	colRed        lipgloss.TerminalColor = lipgloss.Color("1")  // errors, deletions
	colYellow     lipgloss.TerminalColor = lipgloss.Color("3")  // working copy, cursor, git mode
	colCyan       lipgloss.TerminalColor = lipgloss.Color("6")  // bookmark mode, hunk headers
	colGray       lipgloss.TerminalColor = lipgloss.Color("8")  // dates, commit IDs, help text
	colDarkGray   lipgloss.TerminalColor = lipgloss.Color("8")  // graph edges, separators
	colWhite      lipgloss.TerminalColor                        // nil — subjects/body use terminal default
	colOrange     lipgloss.TerminalColor = lipgloss.Color("3")  // git mode
	colDarkOrange lipgloss.TerminalColor = lipgloss.Color("3")  // git mode hint
	colPink       lipgloss.TerminalColor = lipgloss.Color("5")  // remote mode
	colDarkPink   lipgloss.TerminalColor = lipgloss.Color("5")  // remote mode hint
	colMutedGray  lipgloss.TerminalColor = lipgloss.Color("8")  // node chars, immutable

	// Background bands — adaptive so they read on light and dark terminals.
	colDarkPurple lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#DAD5F2", Dark: "#3A3350"} // selection / top bar
	colDarkerGray lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#E6E4EC", Dark: "#201F26"} // status / help bars
)

// Diff panel colors.
var (
	diffAddedSign    lipgloss.TerminalColor = lipgloss.Color("2")
	diffRemovedSign  lipgloss.TerminalColor = lipgloss.Color("1")
	diffContextFg    lipgloss.TerminalColor // nil — terminal default
	diffHunkHeaderFg lipgloss.TerminalColor = lipgloss.Color("6")
	diffFileHeaderFg lipgloss.TerminalColor = lipgloss.Color("3")
	diffLineNumber   lipgloss.TerminalColor = lipgloss.Color("8")
	diffBorder       lipgloss.TerminalColor = lipgloss.Color("8")

	diffAddedBg      lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#D6F0D6", Dark: "#1a2e1a"}
	diffRemovedBg    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#F2DADA", Dark: "#2e1a1a"}
	diffHunkHeaderBg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#D4E8E8", Dark: "#1a2a2a"}
	diffFileHeaderBg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#ECE8CE", Dark: "#2a2a1a"}

	// Chunk cursor — a thin left-edge bar marking the focused change chunk.
	// The current line pops in a bright ANSI color; the rest of the chunk is
	// tinted so its extent stays visible while navigating line by line.
	diffCursorAddBright lipgloss.TerminalColor = lipgloss.Color("10") // bright green
	diffCursorDelBright lipgloss.TerminalColor = lipgloss.Color("9")  // bright red
	diffCursorAddDim    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#A8D8A8", Dark: "#2e4a2e"}
	diffCursorDelDim    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#D8A8A8", Dark: "#4a2e2e"}
)

// spinnerFrames cycles a braille spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
