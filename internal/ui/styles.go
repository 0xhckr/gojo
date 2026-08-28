package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type terminalColor = color.Color

var hasDarkBackground = true

func adaptiveColor(light, dark string) terminalColor {
	if hasDarkBackground {
		return lipgloss.Color(dark)
	}
	return lipgloss.Color(light)
}

// The palette uses truecolor hex values with adaptive light/dark pairs, so
// gojo renders with its own refined colour scheme inspired by modern TUI
// design. A three-tier surface system (background → panel → element) provides
// visual hierarchy. Accent colours are truecolor for consistency across
// terminals. nil still means "terminal default" for body text.
//
// These initializers are the "gojo" default theme; at boot (and when the
// user picks another theme) applyTheme rewrites every var from the selected
// theme's palette — see themes.go.

var (
	// ── Surface tiers (background → panel → element) ───────────────
	colBackground terminalColor = adaptiveColor("#f6f6f8", "#0d0d12")
	colPanel      terminalColor = adaptiveColor("#ffffff", "#14141c")
	colElement    terminalColor = adaptiveColor("#ececf0", "#1c1c26")
	colHover      terminalColor = adaptiveColor("#f0f0f5", "#181824")

	// ── Border hierarchy ───────────────────────────────────────────
	colBorder       terminalColor = adaptiveColor("#c4c4cc", "#343440")
	colBorderActive terminalColor = adaptiveColor("#9898a4", "#545462")
	colBorderSubtle terminalColor = adaptiveColor("#dadde0", "#24242e")
	colGraph        terminalColor = adaptiveColor("#9494a4", "#5a5a72") // jj commit-graph edges/symbols

	// ── Text ───────────────────────────────────────────────────────
	colText      terminalColor = adaptiveColor("#1a1a22", "#e2e2ec")
	colTextMuted terminalColor = adaptiveColor("#787884", "#787886")

	// ── Accents ────────────────────────────────────────────────────
	colPurple     terminalColor = adaptiveColor("#6B50FF", "#9d7cd8") // change IDs, primary accent
	colMagenta    terminalColor = adaptiveColor("#7b3fb5", "#c487f0") // change ID prefix
	colBlue       terminalColor = adaptiveColor("#2563eb", "#5c9cf5") // author names
	colGreen      terminalColor = adaptiveColor("#3d9a57", "#7fd88f") // bookmarks, additions
	colRed        terminalColor = adaptiveColor("#d1383d", "#e06c75") // errors, deletions
	colYellow     terminalColor = adaptiveColor("#b0851f", "#f5a742") // working copy, cursor
	colCyan       terminalColor = adaptiveColor("#318795", "#56b6c2") // bookmark mode, hunk headers
	colOrange     terminalColor = adaptiveColor("#d68c27", "#f5a742") // git mode
	colDarkOrange terminalColor = adaptiveColor("#a06b1a", "#b08030") // git mode hint
	colPink       terminalColor = adaptiveColor("#c44b8a", "#ff7eb6") // remote mode
	colDarkPink   terminalColor = adaptiveColor("#9a3868", "#b85a90") // remote mode hint
	colTeal       terminalColor = adaptiveColor("#00897B", "#4DB6AC") // tag mode, tags
	colDarkTeal   terminalColor = adaptiveColor("#00695C", "#2E7D72") // tag mode hint

	// ── Legacy aliases (map old names to new palette) ─────────────
	colWhite     = colText
	colGray      = colTextMuted
	colDarkGray  = colBorder
	colMutedGray = colBorderSubtle

	// Background bands — remapped to the surface tiers.
	colDarkPurple = colElement // selection / top bar
	colDarkerGray = colPanel   // status / help bars
)

// File-view section colours — alternating per blame hunk. Section A is a
// blue-purple, section B a pink-purple. Each section has its own bar colours:
// a subdued shade for the rest of the hunk and an intense shade for the
// cursor line, so the ┃ bar always tints toward the section it belongs to.
var (
	fileSectionBg = []terminalColor{
		adaptiveColor("#eae6f6", "#1a1a2e"), // blue-purple
		adaptiveColor("#f6e8f0", "#241a26"), // pink-purple
	}
	fileSectionBarDim = []terminalColor{
		adaptiveColor("#c4bbe0", "#2e2e48"), // dim blue-purple
		adaptiveColor("#e0bcd0", "#3e2840"), // dim pink-purple
	}
	fileSectionBarBright = []terminalColor{
		adaptiveColor("#6B50FF", "#8a8cf5"), // intense blue-purple
		adaptiveColor("#c44b8a", "#e08ad8"), // intense pink-purple
	}
)

// Diff panel colors — subtle tinted backgrounds, refined foregrounds.
var (
	diffAddedSign    terminalColor = adaptiveColor("#3d9a57", "#7fd88f")
	diffRemovedSign  terminalColor = adaptiveColor("#d1383d", "#e06c75")
	diffContextFg    terminalColor = colText
	diffHunkHeaderFg terminalColor = adaptiveColor("#7086b5", "#828bb8")
	diffFileHeaderFg terminalColor = adaptiveColor("#b0851f", "#f5a742")
	diffLineNumber   terminalColor = adaptiveColor("#595959", "#8f8f8f")

	diffAddedBg      terminalColor = adaptiveColor("#d8edd8", "#1a2a22")
	diffRemovedBg    terminalColor = adaptiveColor("#f0d8dc", "#2a1a22")
	diffHunkHeaderBg terminalColor = adaptiveColor("#d8e4ec", "#1a2230")
	diffFileHeaderBg terminalColor = adaptiveColor("#eee4cc", "#24221a")

	// Gutter backgrounds — a dimmer blend toward the panel surface, so the
	// coloured tint between the ┃ bars is less opaque than the content area.
	diffAddedGutterBg   terminalColor = adaptiveColor("#ebf6eb", "#171f1f")
	diffRemovedGutterBg terminalColor = adaptiveColor("#f7ebed", "#1f171f")

	// Chunk cursor — ┃ bar marking the focused change chunk.
	diffCursorAddBright terminalColor = adaptiveColor("#3d9a57", "#7fd88f")
	diffCursorDelBright terminalColor = adaptiveColor("#d1383d", "#e06c75")
	diffCursorAddDim    terminalColor = adaptiveColor("#a8d8a8", "#2e4a2e")
	diffCursorDelDim    terminalColor = adaptiveColor("#d8a8a8", "#4a2e2e")

	// Split mode — indicators for marked/unmarked/partial selection.
	splitMarked   terminalColor = adaptiveColor("#3d9a57", "#7fd88f")
	splitPartial  terminalColor = adaptiveColor("#b0851f", "#f5a742")
	splitUnmarked terminalColor = adaptiveColor("#999999", "#555560")

	// Conflict view — side-by-side pane tints (blue = side 1, green = side 2).
	confLeftBg       terminalColor = adaptiveColor("#dde5f4", "#192230")
	confLeftFocusBg  terminalColor = adaptiveColor("#ccdaf2", "#213144")
	confRightBg      terminalColor = adaptiveColor("#dcf0dc", "#1a291d")
	confRightFocusBg terminalColor = adaptiveColor("#cbeacc", "#233a28")
	confLoserBg      terminalColor = adaptiveColor("#eaeaef", "#17171d")
)

// spinnerFrames cycles a braille spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
