package ui

import "github.com/charmbracelet/lipgloss"

// The palette uses truecolor hex values with adaptive light/dark pairs, so
// gojo renders with its own refined colour scheme inspired by modern TUI
// design. A three-tier surface system (background → panel → element) provides
// visual hierarchy. Accent colours are truecolor for consistency across
// terminals. nil still means "terminal default" for body text.

var (
	// ── Surface tiers (background → panel → element) ───────────────
	colBackground lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#f6f6f8", Dark: "#0d0d12"}
	colPanel      lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#14141c"}
	colElement    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#ececf0", Dark: "#1c1c26"}

	// ── Border hierarchy ───────────────────────────────────────────
	colBorder       lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#c4c4cc", Dark: "#343440"}
	colBorderActive lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#9898a4", Dark: "#545462"}
	colBorderSubtle lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#dadde0", Dark: "#24242e"}

	// ── Text ───────────────────────────────────────────────────────
	colText      lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#1a1a22", Dark: "#e2e2ec"}
	colTextMuted lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#787884", Dark: "#787886"}

	// ── Accents ────────────────────────────────────────────────────
	colPurple     lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#6B50FF", Dark: "#9d7cd8"}  // change IDs, primary accent
	colMagenta    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#7b3fb5", Dark: "#c487f0"}  // change ID prefix
	colBlue       lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#5c9cf5"}  // author names
	colGreen      lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#3d9a57", Dark: "#7fd88f"}  // bookmarks, additions
	colRed        lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d1383d", Dark: "#e06c75"}  // errors, deletions
	colYellow     lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#b0851f", Dark: "#f5a742"}  // working copy, cursor
	colCyan       lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#318795", Dark: "#56b6c2"}  // bookmark mode, hunk headers
	colOrange     lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d68c27", Dark: "#f5a742"}  // git mode
	colDarkOrange lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#a06b1a", Dark: "#b08030"}  // git mode hint
	colPink       lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#c44b8a", Dark: "#ff7eb6"}  // remote mode
	colDarkPink   lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#9a3868", Dark: "#b85a90"}  // remote mode hint

	// ── Legacy aliases (map old names to new palette) ─────────────
	colWhite     = colText
	colGray      = colTextMuted
	colDarkGray  = colBorder
	colMutedGray = colBorderSubtle

	// Background bands — remapped to the surface tiers.
	colDarkPurple = colElement    // selection / top bar
	colDarkerGray = colPanel      // status / help bars
)

// Blame-view section backgrounds — alternating tints per blame hunk so
// contiguous runs of the same commit stand out.
var (
	blameSectionBgA lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#f2f2f6", Dark: "#181820"}
	blameSectionBgB lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#e8e8ee", Dark: "#1e1e28"}
)

// Diff panel colors — subtle tinted backgrounds, refined foregrounds.
var (
	diffAddedSign    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#3d9a57", Dark: "#7fd88f"}
	diffRemovedSign  lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d1383d", Dark: "#e06c75"}
	diffContextFg    lipgloss.TerminalColor = colText
	diffHunkHeaderFg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#7086b5", Dark: "#828bb8"}
	diffFileHeaderFg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#b0851f", Dark: "#f5a742"}
	diffLineNumber   lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#595959", Dark: "#8f8f8f"}
	diffBorder       lipgloss.TerminalColor = colBorder

	diffAddedBg      lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d8edd8", Dark: "#1a2a22"}
	diffRemovedBg    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#f0d8dc", Dark: "#2a1a22"}
	diffHunkHeaderBg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d8e4ec", Dark: "#1a2230"}
	diffFileHeaderBg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#eee4cc", Dark: "#24221a"}

	// Chunk cursor — ┃ bar marking the focused change chunk.
	diffCursorAddBright lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#3d9a57", Dark: "#7fd88f"}
	diffCursorDelBright lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d1383d", Dark: "#e06c75"}
	diffCursorAddDim    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#a8d8a8", Dark: "#2e4a2e"}
	diffCursorDelDim    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d8a8a8", Dark: "#4a2e2e"}
)

// spinnerFrames cycles a braille spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
