// ── Color Palette (CharmTone / Charmbracelet Crush) ───────────────────────
export const colors = {
	purple: "#6B50FF",      // Charple — primary, change IDs
	darkPurple: "#3A3350", // Charple-tinted dark — selection background
	blue: "#00A4FF",       // Malibu — info, author names
	green: "#00FFB2",      // Julep — success, bookmarks
	red: "#EB4268",        // Sriracha — errors
	yellow: "#F5EF34",     // Mustard — working copy, cursor highlights
	magenta: "#FF60FF",    // Dolly — secondary, change ID prefix
	cyan: "#10B1AE",       // Zinc — bookmark mode
	gray: "#858392",       // Squid — dates, commit IDs, help text
	darkGray: "#3A3943",   // Char — graph edges, separators
	darkerGray: "#201F26",  // Pepper — status bar background
	white: "#ECEBF0",      // Sash — subject lines, body text
	orange: "#FF985A",     // Tang — git mode
	darkOrange: "#BF976F", // Cumin — git mode hint
	mutedGray: "#605F6B",  // Oyster — node chars, immutable
}

// ── Status strings ────────────────────────────────────────────────────────
export const statusSymbols = {
	added: "A",
	modified: "M",
	removed: "D",
	conflicted: "C",
} as const

// ── Spinner frames ────────────────────────────────────────────────────────
export const spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"]
