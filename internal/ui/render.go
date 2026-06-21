package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
