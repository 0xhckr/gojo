package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// seg is a styled run of text used to compose a single terminal line.
// A nil fg or bg means "use the terminal default".
type seg struct {
	text      string
	fg, bg    lipgloss.TerminalColor
	bold      bool
	underline bool
	faint     bool
}

// renderSegs renders a sequence of styled segments into one ANSI string.
// Each segment carries its own background so resets between segments never
// leave a visible gap in a filled row.
func renderSegs(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		st := lipgloss.NewStyle()
		if s.fg != nil {
			st = st.Foreground(s.fg)
		}
		if s.bg != nil {
			st = st.Background(s.bg)
		}
		if s.bold {
			st = st.Bold(true)
		}
		if s.underline {
			st = st.Underline(true)
		}
		if s.faint {
			st = st.Faint(true)
		}
		b.WriteString(st.Render(s.text))
	}
	return b.String()
}

// clip truncates a (possibly styled) line to width columns, preserving ANSI.
func clip(s string, width int) string {
	if width < 0 {
		width = 0
	}
	return ansi.Truncate(s, width, "")
}

// plainRow renders segments and clips to width (no background fill).
func plainRow(width int, segs ...seg) string {
	return clip(renderSegs(segs), width)
}

// bgRow renders segments over a full-width background, padding then clipping.
// Segments without an explicit background inherit bg.
func bgRow(width int, bg lipgloss.TerminalColor, segs ...seg) string {
	for i := range segs {
		if segs[i].bg == nil {
			segs[i].bg = bg
		}
	}
	rendered := renderSegs(segs)
	w := lipgloss.Width(rendered)
	if w < width {
		pad := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", width-w))
		rendered += pad
	}
	return clip(rendered, width)
}

// blankRow returns a width-wide row filled with bg (or empty if bg == nil).
func blankRow(width int, bg lipgloss.TerminalColor) string {
	if bg == nil {
		return ""
	}
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", width))
}

// runeWidth returns the display cell width of a rune (0 for combining marks).
func runeWidth(r rune) int { return runewidth.RuneWidth(r) }

// wrapSegs greedily wraps a sequence of styled segments into lines no wider
// than `width` cells, splitting segments mid-text as needed. Styling is
// preserved per emitted rune and adjacent same-style runes are merged back into
// a single segment so the output stays compact. Always returns at least one
// line (possibly empty). Used to render the visible diff rows only.
func wrapSegs(segs []seg, width int) [][]seg {
	if width < 1 {
		width = 1
	}
	var lines [][]seg
	var cur []seg
	w := 0
	// push appends rune r (carrying style st) to the current line, merging with
	// the previous segment when the style matches.
	push := func(r rune, st seg) {
		if n := len(cur); n > 0 {
			last := &cur[n-1]
			if last.fg == st.fg && last.bg == st.bg && last.bold == st.bold && last.underline == st.underline && last.faint == st.faint {
				last.text += string(r)
				w += runeWidth(r)
				return
			}
		}
		cur = append(cur, seg{text: string(r), fg: st.fg, bg: st.bg, bold: st.bold, underline: st.underline, faint: st.faint})
		w += runeWidth(r)
	}
	for _, s := range segs {
		if s.text == "" {
			continue
		}
		for _, r := range s.text {
			rw := runeWidth(r)
			if w+rw > width && w > 0 {
				lines = append(lines, cur)
				cur = nil
				w = 0
			}
			push(r, s)
		}
	}
	lines = append(lines, cur) // flush final line (empty → one empty line)
	return lines
}

// textWrapCount is the number of terminal lines `s` occupies when hard-wrapped
// to `width` cells. Cheap (no allocation); used to size the diff layout for
// every row so the scroll window and scrollbar stay accurate.
func textWrapCount(s string, width int) int {
	if width < 1 {
		width = 1
	}
	lines, w := 1, 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > width && w > 0 {
			lines++
			w = 0
		}
		w += rw
	}
	return lines
}

// spansWrapCount is like textWrapCount but iterates styled spans without
// concatenating their text, so it is allocation-free.
func spansWrapCount(spans []span, width int) int {
	if width < 1 {
		width = 1
	}
	lines, w := 1, 0
	for _, sp := range spans {
		for _, r := range sp.text {
			rw := runeWidth(r)
			if w+rw > width && w > 0 {
				lines++
				w = 0
			}
			w += rw
		}
	}
	return lines
}
