// Package diff computes and renders styled unified diffs using gotextdiff.
package diff

import (
	"fmt"
	"strings"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
)

// FileDiff holds the computed diff for a single file.
type FileDiff struct {
	Path    string
	From    string
	To      string
	Unified gotextdiff.Unified
}

// ComputeFile computes a diff between before and after content for a single file.
func ComputeFile(path, before, after string) FileDiff {
	edits := myers.ComputeEdits(span.URIFromPath(path), before, after)
	u := gotextdiff.ToUnified("a/"+path, "b/"+path, before, edits)
	return FileDiff{Path: path, From: "a/" + path, To: "b/" + path, Unified: u}
}

// ANSI style constants.
const (
	reset = "\x1b[0m"

	boldBlue   = "\x1b[1;38;5;69m"
	boldRed    = "\x1b[1;38;5;167m"
	boldGreen  = "\x1b[1;38;5;78m"
	boldCyan   = "\x1b[1;38;5;73m"
	boldYellow = "\x1b[1;38;5;179m"
	fgGreen    = "\x1b[38;5;78m"
	fgRed      = "\x1b[38;5;167m"
	fgGray     = "\x1b[38;5;245m"
	fgWhite    = "\x1b[38;5;252m"
	fgCyan     = "\x1b[38;5;73m"
	fgDimGreen = "\x1b[38;5;28m"
	fgDimRed   = "\x1b[38;5;124m"

	bgFileHeader = "\x1b[48;5;17m"
	bgHunkHeader = "\x1b[48;5;235m"
	bgAdd        = "\x1b[48;5;22m"
	bgRemove     = "\x1b[48;5;52m"
)

const lineNumWidth = 4

// RenderFiles renders multiple FileDiffs into a single styled ANSI string.
func RenderFiles(files []FileDiff, width int) string {
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		renderFile(&b, f, width)
	}
	return b.String()
}

// renderFile renders one FileDiff as styled ANSI.
func renderFile(b *strings.Builder, f FileDiff, width int) {
	// File header
	b.WriteString(bgFileHeader)
	b.WriteString(boldBlue)
	b.WriteString(" diff --git ")
	b.WriteString(f.From)
	b.WriteString(" ")
	b.WriteString(f.To)
	padLine(b, "diff --git "+f.From+" "+f.To, 1, width)
	b.WriteString(reset)
	b.WriteString("\n")

	// --- / +++ headers
	b.WriteString(boldRed)
	b.WriteString("--- ")
	b.WriteString(f.From)
	b.WriteString(reset)
	b.WriteString("\n")
	b.WriteString(boldGreen)
	b.WriteString("+++ ")
	b.WriteString(f.To)
	b.WriteString(reset)
	b.WriteString("\n")

	// Hunks
	oldLine := 0
	newLine := 0
	for _, hunk := range f.Unified.Hunks {
		oldLine = hunk.FromLine
		newLine = hunk.ToLine

		// Hunk header
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.FromLine, hunk.ToLine, hunk.FromLine, hunk.ToLine)
		// Count lines by kind to compute proper ranges
		oldCount, newCount := countHunkLines(hunk)
		header = fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.FromLine, oldCount, hunk.ToLine, newCount)

		b.WriteString(bgHunkHeader)
		b.WriteString(fgCyan)
		b.WriteString(" ")
		b.WriteString(header)
		padLine(b, header, 1, width)
		b.WriteString(reset)
		b.WriteString("\n")

		for _, line := range hunk.Lines {
			switch line.Kind {
			case gotextdiff.Delete:
				content := strings.TrimRight(line.Content, "\n")
				b.WriteString(bgRemove)
				b.WriteString(formatLineNums(oldLine, 0, fgDimRed))
				b.WriteString(fgRed)
				b.WriteString("-")
				b.WriteString(content)
				padLine(b, content, 10, width)
				b.WriteString(reset)
				b.WriteString("\n")
				oldLine++

			case gotextdiff.Insert:
				content := strings.TrimRight(line.Content, "\n")
				b.WriteString(bgAdd)
				b.WriteString(formatLineNums(0, newLine, fgDimGreen))
				b.WriteString(fgGreen)
				b.WriteString("+")
				b.WriteString(content)
				padLine(b, content, 10, width)
				b.WriteString(reset)
				b.WriteString("\n")
				newLine++

			case gotextdiff.Equal:
				content := strings.TrimRight(line.Content, "\n")
				b.WriteString(formatLineNums(oldLine, newLine, fgGray))
				b.WriteString(fgWhite)
				b.WriteString(" ")
				b.WriteString(content)
				b.WriteString(reset)
				b.WriteString("\n")
				oldLine++
				newLine++
			}
		}
	}
}

// countHunkLines returns the old and new line counts in a hunk.
func countHunkLines(hunk *gotextdiff.Hunk) (old, new int) {
	for _, l := range hunk.Lines {
		switch l.Kind {
		case gotextdiff.Delete:
			old++
		case gotextdiff.Insert:
			new++
		case gotextdiff.Equal:
			old++
			new++
		}
	}
	return
}

// formatLineNum formats a single line number column (right-aligned, 4 chars).
// Does NOT emit reset — caller must reset.
func formatLineNum(n int, color string) string {
	if n <= 0 {
		return color + strings.Repeat(" ", lineNumWidth)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) > lineNumWidth {
		s = s[len(s)-lineNumWidth:]
	}
	return color + strings.Repeat(" ", lineNumWidth-len(s)) + s
}

// formatLineNums formats a pair of line numbers: "  12  34 ".
// Does NOT emit reset — caller must reset.
func formatLineNums(oldN, newN int, color string) string {
	return formatLineNum(oldN, color) + " " + formatLineNum(newN, color) + " "
}

// padLine pads the current line to fill the terminal width.
// extra is the number of visible chars already written outside Content.
func padLine(b *strings.Builder, content string, extra int, width int) {
	visible := visibleWidth(content) + extra
	if visible < width {
		b.WriteString(strings.Repeat(" ", width-visible))
	}
}

// visibleWidth returns the visible width of a string, ignoring ANSI escapes.
func visibleWidth(s string) int {
	w := 0
	inEscape := false
	for _, ch := range s {
		if ch == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				inEscape = false
			}
			continue
		}
		w++
	}
	return w
}
