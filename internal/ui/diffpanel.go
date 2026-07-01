package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

func maxLineDigits(rows []diffRow) int {
	m := 1
	for _, r := range rows {
		if r.kind != rowLine {
			continue
		}
		if r.oldNum > 0 {
			if d := len(strconv.Itoa(r.oldNum)); d > m {
				m = d
			}
		}
		if r.newNum > 0 {
			if d := len(strconv.Itoa(r.newNum)); d > m {
				m = d
			}
		}
	}
	return m
}

func padNum(n, digits int) string {
	if n <= 0 {
		return strings.Repeat(" ", digits)
	}
	s := strconv.Itoa(n)
	if len(s) < digits {
		s = strings.Repeat(" ", digits-len(s)) + s
	}
	return s
}

func lineNumText(r diffRow, digits int) string {
	return padNum(r.oldNum, digits) + " " + padNum(r.newNum, digits) + " "
}

var statusColors = map[jj.StatusKind]lipgloss.TerminalColor{
	jj.StatusAdded:      colGreen,
	jj.StatusModified:   colYellow,
	jj.StatusRemoved:    colRed,
	jj.StatusConflicted: colMagenta,
}

func statusSym(k jj.StatusKind) string {
	switch k {
	case jj.StatusAdded:
		return "A"
	case jj.StatusModified:
		return "M"
	case jj.StatusRemoved:
		return "D"
	default:
		return "C"
	}
}

// renderDiffPanel produces exactly height lines for the diff panel. Only the
// visible window of rows is styled, so scroll cost is independent of diff size.
//
// cursorBodyRow is the body-row index of the focused change line (-1 if none);
// chunkRows is the set of body-row indices that belong to the focused chunk.
// A thin left-edge bar is drawn for those rows: bright for the cursor line,
// dim for the rest of the chunk.
func renderDiffPanel(width, height int, rev string, loading bool, rows []diffRow, digits int, status []jj.StatusEntry, rawContent string, scrollY int, cursorBodyRow int, chunkRows map[int]bool) []string {
	// Title bar — the only sticky chrome; status + separator + diff all scroll
	// together below it as one body.
	titleLine := " " + rev
	if loading {
		titleLine += "  loading…"
	}
	titleLine += "  (enter/q to close) "
	out := []string{bgRow(width, colElement, seg{text: titleLine, fg: colText})}

	contentH := height - len(out)
	if contentH < 0 {
		contentH = 0
	}

	// The scrollable body is: status header + status items + separator + diff.
	head := buildStatusHead(width, status)
	bodyTotal := len(head) + diffBodyLen(rows, rawContent)

	start, end := visibleRange(scrollY, contentH, bodyTotal)

	// Scrollbar: reserve 1 column on the right when content overflows.
	scrollW := width
	thumbStart, thumbEnd := scrollbarThumb(bodyTotal, start, end-start, contentH)
	hasBar := thumbStart >= 0
	if hasBar {
		scrollW--
	}

	gutterWidth := digits*2 + 4
	var rawLines []string
	if len(rows) == 0 && rawContent != "" {
		rawLines = strings.Split(rawContent, "\n")
	}

	var content []string
	for i := start; i < end; i++ {
		rowLine := i - start // 0-based index within the visible window
		if i < len(head) {
			content = append(content, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, rowLine, thumbStart, thumbEnd, head[i]))
			continue
		}
		idx := i - len(head)
		if rawLines != nil {
			rowStr := plainRow(width, seg{text: " ", fg: nil}, seg{text: rawLines[idx], fg: colText})
			content = append(content, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, rowLine, thumbStart, thumbEnd, rowStr))
		} else {
			r := rows[idx]
			var rowBg lipgloss.TerminalColor = colPanel
			switch {
			case r.kind == rowFileHeader:
				rowBg = diffFileHeaderBg
			case r.kind == rowHunkHeader:
				rowBg = diffHunkHeaderBg
			case r.lineKind == "addition":
				rowBg = diffAddedBg
			case r.lineKind == "deletion":
				rowBg = diffRemovedBg
			}
			diffRowStr := renderDiffRow(width, gutterWidth, digits, r, cursorBar(r, i, cursorBodyRow, chunkRows))
			content = append(content, renderRowWithBarFromString(scrollW, width, rowBg, hasBar, rowLine, thumbStart, thumbEnd, diffRowStr))
		}
	}

	content = padLines(content, contentH)
	out = append(out, content...)
	return padLines(out, height)
}

// buildStatusHead renders the status header, items, and separator — the small
// fixed-size top of the scrollable body.
func buildStatusHead(width int, status []jj.StatusEntry) []string {
	head := []string{bgRow(width, colPanel, seg{text: "┃ ", fg: colCyan, bold: true, bg: colPanel}, seg{text: "status", fg: colTextMuted, bg: colPanel})}
	if len(status) == 0 {
		head = append(head, bgRow(width, colPanel, seg{text: "  (no changes)", fg: colTextMuted, bg: colPanel}))
	} else {
		for _, e := range status {
			color := statusColors[e.Status]
			if color == nil {
				color = colTextMuted
			}
			head = append(head, bgRow(width, colPanel,
				seg{text: "┃ ", fg: color, bg: colPanel},
				seg{text: statusSym(e.Status) + " ", fg: color, bg: colPanel},
				seg{text: e.Path, fg: color, bg: colPanel},
			))
		}
	}
	head = append(head, bgRow(width, colPanel, seg{text: strings.Repeat("─", width), fg: colBorder, bg: colPanel}))
	return head
}

// diffBodyLen is the number of diff (or raw) lines below the status head.
func diffBodyLen(rows []diffRow, rawContent string) int {
	if len(rows) == 0 && rawContent != "" {
		return strings.Count(rawContent, "\n") + 1
	}
	return len(rows)
}

// visibleRange clamps a scroll offset to a [start, end) window of at most
// count lines within a list of total items.
func visibleRange(scrollY, count, total int) (int, int) {
	start := scrollY
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + count
	if end > total {
		end = total
	}
	return start, end
}

func renderDiffRow(width, gutterWidth, digits int, r diffRow, barColor lipgloss.TerminalColor) string {
	switch r.kind {
	case rowFileHeader:
		label := r.path + "  (" + r.changeType + ")"
		if r.prevPath != "" {
			label = r.prevPath + " → " + r.path + "  (" + r.changeType + ")"
		}
		return bgRow(width, diffFileHeaderBg,
			seg{text: "┃", fg: diffFileHeaderFg, bold: true, bg: colPanel},
			seg{text: " ", fg: diffFileHeaderFg, bold: true, bg: diffFileHeaderBg},
			seg{text: label, fg: diffFileHeaderFg, bold: true, bg: diffFileHeaderBg})

	case rowHunkHeader:
		return bgRow(width, diffHunkHeaderBg,
			seg{text: "┃", fg: diffHunkHeaderFg, bg: colPanel},
			seg{text: " ", fg: diffHunkHeaderFg, bg: diffHunkHeaderBg},
			seg{text: r.hunkText, fg: diffHunkHeaderFg, bg: diffHunkHeaderBg})

	default:
		var lineFg, lineBg lipgloss.TerminalColor
		switch r.lineKind {
		case "addition":
			lineFg, lineBg = diffAddedSign, diffAddedBg
		case "deletion":
			lineFg, lineBg = diffRemovedSign, diffRemovedBg
		default:
			lineFg = diffContextFg
		}

		// Line numbers.
		gutter := lineNumText(r, digits)

		// Left cursor gutter: ┃ before line numbers. Bright on cursor, dim
		// on chunk, panel-coloured (invisible) elsewhere.
		leftBar := seg{text: "┃", fg: colPanel, bg: colPanel}
		if barColor != nil {
			leftBar = seg{text: "┃", fg: barColor, bg: colPanel}
		}

		// Right cursor gutter: ┃ after line numbers, always panel-coloured.
		rightBar := seg{text: "┃", fg: colPanel, bg: lineBg}

		segs := []seg{leftBar, {text: gutter, fg: diffLineNumber, bg: lineBg}, rightBar, {text: r.sign, fg: lineFg, bg: lineBg}}
		for _, s := range r.spans {
			// Syntax-highlight colors from chroma are truecolor hex; fall back
			// to the line's kind color when a token has no color.
			var fg lipgloss.TerminalColor = lineFg
			if s.fg != "" {
				fg = lipgloss.Color(s.fg)
			}
			segs = append(segs, seg{text: s.text, fg: fg, bg: lineBg})
		}
		_ = gutterWidth
		if lineBg == nil {
			return bgRow(width, colPanel, segs...)
		}
		return bgRow(width, lineBg, segs...)
	}
}

// cursorBar picks the foreground color for the ┃ cursor glyph on a given
// content line. The focused line gets a bright color; other lines in the same
// chunk get a dim tint; everything else gets nothing.
func cursorBar(r diffRow, bodyRow, cursorBodyRow int, chunkRows map[int]bool) lipgloss.TerminalColor {
	if r.kind != rowLine {
		return nil
	}
	isCursor := bodyRow == cursorBodyRow
	inChunk := chunkRows != nil && chunkRows[bodyRow]
	if !isCursor && !inChunk {
		return nil
	}
	switch r.lineKind {
	case "addition":
		if isCursor {
			return diffCursorAddBright
		}
		return diffCursorAddDim
	case "deletion":
		if isCursor {
			return diffCursorDelBright
		}
		return diffCursorDelDim
	}
	return nil
}
