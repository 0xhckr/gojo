package ui

// Theme system. Theme data lives in TOML files — one file per theme:
//
//	themes/*.toml                      built-in set (this repo; installed to
//	                                   share/gojo/themes next to the binary)
//	~/.config/gojo/themes/*.toml       user themes / overrides (same filename
//	                                   id shadows a built-in)
//
// A Theme carries one or two variants (dark and/or light) of a paletteDef —
// the full set of color slots the UI uses. Slots left empty in a variant are
// filled by deterministic derivation from the core slots (see resolve): tint
// backgrounds are color mixes toward the background, "dark_*" hint colors are
// dimmed accent copies, and accent chains fall back (magenta → purple, pink →
// magenta, teal → cyan, …). A slot value is either a "#rrggbb" hex color, an
// ANSI lipgloss color name (e.g. "1", "12" — how the "terminal" theme follows
// the terminal's own scheme), or empty. mix() only blends hex inputs —
// otherwise it yields "" which becomes nil ("terminal default") at apply time.
//
// The "gojo" default and the "terminal" followers are compiled in (they must
// exist even when no theme directory is installed); everything else comes
// from files. The active theme is chosen with theme = "<id>" in gojo.toml or
// via the in-app picker (key T in the log view), which writes the same key.

import (
	"reflect"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// paletteDef holds every color slot the UI consumes. All fields are color
// strings: "" means "derive from the core slots" (or terminal-nil when no
// derivation is possible), "#rrggbb" is a truecolor value, anything else is
// handed to lipgloss verbatim (ANSI palette indices like "1"…"15" work).
type paletteDef struct {
	// Surfaces, borders, text.
	Background   string `toml:"background"`
	Panel        string `toml:"panel"`
	Element      string `toml:"element"`
	Hover        string `toml:"hover"`
	Border       string `toml:"border"`
	BorderActive string `toml:"border_active"`
	BorderSubtle string `toml:"border_subtle"`
	Graph        string `toml:"graph"`
	Text         string `toml:"text"`
	TextMuted    string `toml:"text_muted"`

	// Core accents.
	Purple     string `toml:"purple"`
	Magenta    string `toml:"magenta"`
	Blue       string `toml:"blue"`
	Green      string `toml:"green"`
	Red        string `toml:"red"`
	Yellow     string `toml:"yellow"`
	Cyan       string `toml:"cyan"`
	Orange     string `toml:"orange"`
	DarkOrange string `toml:"dark_orange"`
	Pink       string `toml:"pink"`
	DarkPink   string `toml:"dark_pink"`
	Teal       string `toml:"teal"`
	DarkTeal   string `toml:"dark_teal"`

	// Diff panel (derived from the mix recipe when empty).
	DiffAddedSign       string `toml:"diff_added_sign"`
	DiffRemovedSign     string `toml:"diff_removed_sign"`
	DiffContextFg       string `toml:"diff_context_fg"`
	DiffHunkHeaderFg    string `toml:"diff_hunk_header_fg"`
	DiffFileHeaderFg    string `toml:"diff_file_header_fg"`
	DiffLineNumber      string `toml:"diff_line_number"`
	DiffAddedBg         string `toml:"diff_added_bg"`
	DiffRemovedBg       string `toml:"diff_removed_bg"`
	DiffHunkHeaderBg    string `toml:"diff_hunk_header_bg"`
	DiffFileHeaderBg    string `toml:"diff_file_header_bg"`
	DiffAddedGutterBg   string `toml:"diff_added_gutter_bg"`
	DiffRemovedGutterBg string `toml:"diff_removed_gutter_bg"`
	DiffCursorAddBright string `toml:"diff_cursor_add_bright"`
	DiffCursorDelBright string `toml:"diff_cursor_del_bright"`
	DiffCursorAddDim    string `toml:"diff_cursor_add_dim"`
	DiffCursorDelDim    string `toml:"diff_cursor_del_dim"`

	// Split mode indicators.
	SplitMarked   string `toml:"split_marked"`
	SplitPartial  string `toml:"split_partial"`
	SplitUnmarked string `toml:"split_unmarked"`

	// Conflict panes.
	ConfLeftBg       string `toml:"conf_left_bg"`
	ConfLeftFocusBg  string `toml:"conf_left_focus_bg"`
	ConfRightBg      string `toml:"conf_right_bg"`
	ConfRightFocusBg string `toml:"conf_right_focus_bg"`
	ConfLoserBg      string `toml:"conf_loser_bg"`

	// File-view section bands (blame hunk alternation).
	SectionBgA        string `toml:"section_bg_a"`
	SectionBgB        string `toml:"section_bg_b"`
	SectionBarDimA    string `toml:"section_bar_dim_a"`
	SectionBarDimB    string `toml:"section_bar_dim_b"`
	SectionBarBrightA string `toml:"section_bar_bright_a"`
	SectionBarBrightB string `toml:"section_bar_bright_b"`
}

// tomlPaletteKeys maps TOML key names to palette fields via the toml tags.
var tomlPaletteKeys = func() map[string]int {
	m := map[string]int{}
	t := reflect.TypeOf(paletteDef{})
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("toml"); tag != "" && tag != "-" {
			m[tag] = i
		}
	}
	return m
}()

// Theme is one selectable color scheme.
type Theme struct {
	ID     string      // selection id — matched by theme = "<id>" in config
	Title  string      // display name in the picker
	Custom bool        // loaded from ~/.config/gojo/themes
	Dark   *paletteDef // nil when the theme has no dark variant
	Light  *paletteDef // nil when the theme has no light variant

	// SyntaxDark/SyntaxLight optionally override the chroma style used for
	// diff syntax highlighting on dark/light terminal backgrounds. Empty
	// keeps the adaptive default (github-dark / github).
	SyntaxDark  string
	SyntaxLight string
}

// variantLabel is the picker's badge for a theme: "adaptive" when it follows
// the terminal's background, otherwise the single variant it pins.
func (t Theme) variantLabel() string {
	switch {
	case t.Dark != nil && t.Light != nil:
		return "adaptive"
	case t.Light != nil:
		return "light"
	default:
		return "dark"
	}
}

// variant returns the resolved palette for the terminal's background class.
// When the theme has only one variant it is used for both. Palettes are
// resolved once at load/parse time (see resolve), so this is a cheap copy
// — it runs on every picker row of every frame and on every theme switch.
func (t Theme) variant(dark bool) paletteDef {
	p := t.Dark
	if !dark {
		p = t.Light
	}
	if p == nil {
		p = t.Dark
	}
	if p == nil {
		p = t.Light
	}
	if p == nil {
		return paletteDef{}
	}
	return *p
}

// previewColors returns the theme's core accent colors for the picker's
// color swatch, resolved for the terminal's background class.
func (t Theme) previewColors(dark bool) []lipgloss.TerminalColor {
	p := t.variant(dark)
	cols := []string{p.Red, p.Orange, p.Yellow, p.Green, p.Cyan, p.Blue, p.Purple, p.Pink}
	out := make([]lipgloss.TerminalColor, 0, len(cols))
	for _, c := range cols {
		if c != "" {
			out = append(out, lipgloss.Color(c))
		}
	}
	return out
}

// ── Color math ──────────────────────────────────────────────────────────────

// parseHex parses "#rrggbb" into RGB components. ok=false for anything else
// (empty, ANSI indices, names).
func parseHex(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(n >> 16 & 0xff), int(n >> 8 & 0xff), int(n & 0xff), true
}

// hex formats RGB components as "#rrggbb", clamped to [0,255].
func hex(r, g, b int) string {
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	two := func(v int) string {
		const digits = "0123456789abcdef"
		return string([]byte{digits[v>>4], digits[v&15]})
	}
	return "#" + two(clamp(r)) + two(clamp(g)) + two(clamp(b))
}

// isLightHex reports whether a hex background reads as light (for picking
// which half of a derivation recipe applies).
func isLightHex(bg string) bool {
	r, g, b, ok := parseHex(bg)
	if !ok {
		return false
	}
	// Relative luminance (Rec. 601, close enough for palette decisions).
	return (299*r+587*g+114*b)/1000 > 140
}

// mix blends fg over bg with weight t (0 = bg, 1 = fg), linear per channel.
// Non-hex inputs yield "" (derivation impossible → terminal default).
func mix(fg, bg string, t float64) string {
	fr, fg2, fb, ok := parseHex(fg)
	if !ok {
		return ""
	}
	br, bg2, bb, ok := parseHex(bg)
	if !ok {
		return ""
	}
	f := func(a, c int) int { return int(float64(a)*t + float64(c)*(1-t) + 0.5) }
	return hex(f(fr, br), f(fg2, bg2), f(fb, bb))
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// dim produces a subdued copy of c that stays readable on the background:
// on dark backgrounds blend toward the background, on light ones toward the
// text color. Non-hex inputs fall back to c verbatim.
func dim(c, bg, text string, t float64) string {
	if _, _, _, ok := parseHex(c); !ok {
		return c
	}
	if isLightHex(bg) {
		if out := mix(c, text, t); out != "" {
			return out
		}
		return c
	}
	if out := mix(c, bg, t); out != "" {
		return out
	}
	return c
}

// resolve fills every empty slot of p using the derivation recipe.
func resolve(p paletteDef) paletteDef {
	bg, text, muted := p.Background, p.Text, p.TextMuted

	// Surfaces — hierarchy is "more text mixed in" = "closer to the eye".
	if p.Panel == "" {
		p.Panel = mix(text, bg, 0.045)
	}
	if p.Element == "" {
		p.Element = mix(text, bg, 0.11)
	}
	if p.Hover == "" {
		p.Hover = mix(text, bg, 0.075)
	}

	// Borders/graph — muted text at increasing strength over the background.
	if p.BorderSubtle == "" {
		p.BorderSubtle = mix(muted, bg, 0.20)
	}
	if p.Border == "" {
		p.Border = mix(muted, bg, 0.36)
	}
	if p.BorderActive == "" {
		p.BorderActive = mix(muted, bg, 0.62)
	}
	if p.Graph == "" {
		p.Graph = mix(muted, bg, 0.65)
	}

	// Accent chains: every accent ends up set when at least one of the
	// family anchors is set.
	p.Purple = firstNonEmpty(p.Purple, p.Blue, p.Cyan)
	p.Blue = firstNonEmpty(p.Blue, p.Cyan, p.Purple)
	p.Cyan = firstNonEmpty(p.Cyan, p.Blue, p.Purple)
	p.Magenta = firstNonEmpty(p.Magenta, mix(p.Purple, text, 0.25), p.Purple)
	p.Pink = firstNonEmpty(p.Pink, p.Magenta)
	p.Teal = firstNonEmpty(p.Teal, p.Cyan)
	p.Green = firstNonEmpty(p.Green, p.Teal)
	p.Orange = firstNonEmpty(p.Orange, mix(p.Red, p.Yellow, 0.5), p.Yellow)
	p.Yellow = firstNonEmpty(p.Yellow, p.Orange)
	p.Red = firstNonEmpty(p.Red, p.Pink)

	if p.DarkOrange == "" {
		p.DarkOrange = dim(p.Orange, bg, text, 0.30)
	}
	if p.DarkPink == "" {
		p.DarkPink = dim(p.Pink, bg, text, 0.30)
	}
	if p.DarkTeal == "" {
		p.DarkTeal = dim(p.Teal, bg, text, 0.30)
	}

	// Diff foregrounds.
	if p.DiffAddedSign == "" {
		p.DiffAddedSign = p.Green
	}
	if p.DiffRemovedSign == "" {
		p.DiffRemovedSign = p.Red
	}
	if p.DiffContextFg == "" {
		p.DiffContextFg = p.Text
	}
	if p.DiffHunkHeaderFg == "" {
		p.DiffHunkHeaderFg = p.Blue
	}
	if p.DiffFileHeaderFg == "" {
		p.DiffFileHeaderFg = p.Yellow
	}
	if p.DiffLineNumber == "" {
		p.DiffLineNumber = p.TextMuted
	}

	// Diff tints: accent mixed a little over the background.
	if p.DiffAddedBg == "" {
		p.DiffAddedBg = mix(p.Green, bg, 0.13)
	}
	if p.DiffRemovedBg == "" {
		p.DiffRemovedBg = mix(p.Red, bg, 0.13)
	}
	if p.DiffHunkHeaderBg == "" {
		p.DiffHunkHeaderBg = mix(p.Blue, bg, 0.13)
	}
	if p.DiffFileHeaderBg == "" {
		p.DiffFileHeaderBg = mix(p.Yellow, bg, 0.12)
	}
	if p.DiffAddedGutterBg == "" {
		p.DiffAddedGutterBg = mix(p.Green, bg, 0.06)
	}
	if p.DiffRemovedGutterBg == "" {
		p.DiffRemovedGutterBg = mix(p.Red, bg, 0.06)
	}
	if p.DiffCursorAddBright == "" {
		p.DiffCursorAddBright = p.Green
	}
	if p.DiffCursorDelBright == "" {
		p.DiffCursorDelBright = p.Red
	}
	if p.DiffCursorAddDim == "" {
		p.DiffCursorAddDim = mix(p.Green, bg, 0.32)
	}
	if p.DiffCursorDelDim == "" {
		p.DiffCursorDelDim = mix(p.Red, bg, 0.32)
	}

	// Split indicators.
	if p.SplitMarked == "" {
		p.SplitMarked = p.Green
	}
	if p.SplitPartial == "" {
		p.SplitPartial = p.Yellow
	}
	if p.SplitUnmarked == "" {
		p.SplitUnmarked = mix(muted, bg, 0.70)
	}

	// Conflict panes: blue = side 1, green = side 2.
	if p.ConfLeftBg == "" {
		p.ConfLeftBg = mix(p.Blue, bg, 0.15)
	}
	if p.ConfLeftFocusBg == "" {
		p.ConfLeftFocusBg = mix(p.Blue, bg, 0.24)
	}
	if p.ConfRightBg == "" {
		p.ConfRightBg = mix(p.Green, bg, 0.13)
	}
	if p.ConfRightFocusBg == "" {
		p.ConfRightFocusBg = mix(p.Green, bg, 0.21)
	}
	if p.ConfLoserBg == "" {
		p.ConfLoserBg = mix(muted, bg, 0.09)
	}

	// File-view section bands: purple vs pink families.
	if p.SectionBgA == "" {
		p.SectionBgA = mix(p.Purple, bg, 0.10)
	}
	if p.SectionBgB == "" {
		p.SectionBgB = mix(p.Pink, bg, 0.10)
	}
	if p.SectionBarDimA == "" {
		p.SectionBarDimA = mix(p.Purple, bg, 0.30)
	}
	if p.SectionBarDimB == "" {
		p.SectionBarDimB = mix(p.Pink, bg, 0.30)
	}
	if p.SectionBarBrightA == "" {
		p.SectionBarBrightA = p.Purple
	}
	if p.SectionBarBrightB == "" {
		p.SectionBarBrightB = p.Pink
	}
	return p
}

// themeColor combines the light and dark values of one slot into a lipgloss
// color: adaptive when the variants differ, a plain color when both are the
// same (single-variant themes pinned to both backgrounds), nil ("terminal
// default") when both are empty.
func themeColor(light, dark string) lipgloss.TerminalColor {
	if light != "" && dark != "" {
		if light == dark {
			return lipgloss.Color(dark)
		}
		return lipgloss.AdaptiveColor{Light: light, Dark: dark}
	}
	if dark != "" {
		return lipgloss.Color(dark)
	}
	if light != "" {
		return lipgloss.Color(light)
	}
	return nil
}

// applyTheme swaps the whole UI palette to t. Package-level color vars are
// read fresh every frame and the style/blank caches key on the color values,
// so the repaint after a switch is automatic.
func applyTheme(t Theme) {
	l := t.variant(false)
	d := t.variant(true)

	colBackground = themeColor(l.Background, d.Background)
	colPanel = themeColor(l.Panel, d.Panel)
	colElement = themeColor(l.Element, d.Element)
	colHover = themeColor(l.Hover, d.Hover)
	colBorder = themeColor(l.Border, d.Border)
	colBorderActive = themeColor(l.BorderActive, d.BorderActive)
	colBorderSubtle = themeColor(l.BorderSubtle, d.BorderSubtle)
	colGraph = themeColor(l.Graph, d.Graph)
	colText = themeColor(l.Text, d.Text)
	colTextMuted = themeColor(l.TextMuted, d.TextMuted)

	colPurple = themeColor(l.Purple, d.Purple)
	colMagenta = themeColor(l.Magenta, d.Magenta)
	colBlue = themeColor(l.Blue, d.Blue)
	colGreen = themeColor(l.Green, d.Green)
	colRed = themeColor(l.Red, d.Red)
	colYellow = themeColor(l.Yellow, d.Yellow)
	colCyan = themeColor(l.Cyan, d.Cyan)
	colOrange = themeColor(l.Orange, d.Orange)
	colDarkOrange = themeColor(l.DarkOrange, d.DarkOrange)
	colPink = themeColor(l.Pink, d.Pink)
	colDarkPink = themeColor(l.DarkPink, d.DarkPink)
	colTeal = themeColor(l.Teal, d.Teal)
	colDarkTeal = themeColor(l.DarkTeal, d.DarkTeal)

	// Legacy aliases.
	colWhite = colText
	colGray = colTextMuted
	colDarkGray = colBorder
	colMutedGray = colBorderSubtle
	colDarkPurple = colElement
	colDarkerGray = colPanel

	fileSectionBg = []lipgloss.TerminalColor{
		themeColor(l.SectionBgA, d.SectionBgA),
		themeColor(l.SectionBgB, d.SectionBgB),
	}
	fileSectionBarDim = []lipgloss.TerminalColor{
		themeColor(l.SectionBarDimA, d.SectionBarDimA),
		themeColor(l.SectionBarDimB, d.SectionBarDimB),
	}
	fileSectionBarBright = []lipgloss.TerminalColor{
		themeColor(l.SectionBarBrightA, d.SectionBarBrightA),
		themeColor(l.SectionBarBrightB, d.SectionBarBrightB),
	}

	diffAddedSign = themeColor(l.DiffAddedSign, d.DiffAddedSign)
	diffRemovedSign = themeColor(l.DiffRemovedSign, d.DiffRemovedSign)
	diffContextFg = themeColor(l.DiffContextFg, d.DiffContextFg)
	diffHunkHeaderFg = themeColor(l.DiffHunkHeaderFg, d.DiffHunkHeaderFg)
	diffFileHeaderFg = themeColor(l.DiffFileHeaderFg, d.DiffFileHeaderFg)
	diffLineNumber = themeColor(l.DiffLineNumber, d.DiffLineNumber)
	diffAddedBg = themeColor(l.DiffAddedBg, d.DiffAddedBg)
	diffRemovedBg = themeColor(l.DiffRemovedBg, d.DiffRemovedBg)
	diffHunkHeaderBg = themeColor(l.DiffHunkHeaderBg, d.DiffHunkHeaderBg)
	diffFileHeaderBg = themeColor(l.DiffFileHeaderBg, d.DiffFileHeaderBg)
	diffAddedGutterBg = themeColor(l.DiffAddedGutterBg, d.DiffAddedGutterBg)
	diffRemovedGutterBg = themeColor(l.DiffRemovedGutterBg, d.DiffRemovedGutterBg)
	diffCursorAddBright = themeColor(l.DiffCursorAddBright, d.DiffCursorAddBright)
	diffCursorDelBright = themeColor(l.DiffCursorDelBright, d.DiffCursorDelBright)
	diffCursorAddDim = themeColor(l.DiffCursorAddDim, d.DiffCursorAddDim)
	diffCursorDelDim = themeColor(l.DiffCursorDelDim, d.DiffCursorDelDim)

	splitMarked = themeColor(l.SplitMarked, d.SplitMarked)
	splitPartial = themeColor(l.SplitPartial, d.SplitPartial)
	splitUnmarked = themeColor(l.SplitUnmarked, d.SplitUnmarked)

	confLeftBg = themeColor(l.ConfLeftBg, d.ConfLeftBg)
	confLeftFocusBg = themeColor(l.ConfLeftFocusBg, d.ConfLeftFocusBg)
	confRightBg = themeColor(l.ConfRightBg, d.ConfRightBg)
	confRightFocusBg = themeColor(l.ConfRightFocusBg, d.ConfRightFocusBg)
	confLoserBg = themeColor(l.ConfLoserBg, d.ConfLoserBg)

	setChromaStyleOverride(t.SyntaxDark, t.SyntaxLight)
}
