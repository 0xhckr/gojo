package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// scrollbarWidth is the number of columns reserved for the scrollbar track.
// Two columns makes the thumb easy to grab with the mouse for click-and-drag
// scrolling.
const scrollbarWidth = 2

func commitLines(e jj.LogEntry) int { return 2 + len(e.EdgeLines) }

// entryAtLine returns the index of the entry whose content lines contain the
// given absolute line number (0-based). Used to map a scrollbar drag position
// back to a commit index for cursor-driven views like the log.
func entryAtLine(entries []jj.LogEntry, line int) int {
	if len(entries) == 0 {
		return 0
	}
	cum := 0
	for i := range entries {
		cl := commitLines(entries[i])
		if cum+cl > line {
			return i
		}
		cum += cl
	}
	return len(entries) - 1
}

// rebaseView carries the live rebase-mode selection into log rendering so the
// source (picked-up) and destination (drop target) commits can be marked.
type rebaseView struct {
	active  bool
	source  int // index into entries of the picked-up commit
	dest    int // index into entries of the drop target
	subtree bool
	place   int // index into rebasePlaceLabels
}

// squashView carries the live squash-mode selection into log rendering so the
// source (the commit being squashed) and destination (the target it folds into)
// can be marked.
type squashView struct {
	active bool
	source int // index into entries of the commit being squashed
	dest   int // index into entries of the squash target
}

// logWindow computes the visible [off, end) range of commits for the given
// cursor, prior offset, and available line budget (variable-height commits).
func logWindow(entries []jj.LogEntry, cursor, offset, availableLines int) (int, int) {
	off := offset
	if cursor < off {
		off = cursor
	}

	end := off
	used := 0
	for end < len(entries) {
		h := commitLines(entries[end])
		if used+h > availableLines && end > off {
			break
		}
		used += h
		end++
	}

	if cursor >= end {
		off = cursor
		end = cursor + 1
		used = commitLines(entries[cursor])
		for off > 0 {
			h := commitLines(entries[off-1])
			if used+h > availableLines {
				break
			}
			used += h
			off--
		}
	}
	return off, end
}

// scrollbarThumb computes the [start, end) range of the scrollbar thumb within
// a track of `trackH` lines, given the total content lines, the first visible
// line offset, and the number of visible lines. Returns (-1, -1) when no
// scrollbar is needed (everything fits).
func scrollbarThumb(total, firstVis, visLines, trackH int) (int, int) {
	if total <= visLines || total <= 0 || trackH <= 0 {
		return -1, -1
	}
	thumb := trackH * visLines / total
	if thumb < 1 {
		thumb = 1
	}
	maxStart := trackH - thumb
	start := maxStart * firstVis / max(1, total-visLines)
	return start, start + thumb
}

// renderLog produces up to height lines for the commit log. The content area
// gets a subtle panel background, and a scrollbar indicator on the right edge
// shows position when the log overflows.
func renderLog(width, height int, entries []jj.LogEntry, cursor, offset int, aiLoading map[string]bool, spinnerFrame int, rb rebaseView, sq squashView) []string {
	if len(entries) == 0 {
		return padLines([]string{bgRow(width, colPanel, seg{text: "  no revisions found", fg: colTextMuted})}, height)
	}

	focus := cursor
	if rb.active {
		focus = rb.dest
	}
	if sq.active {
		focus = sq.dest
	}

	availableLines := height - 1 // top padding
	off, end := logWindow(entries, focus, offset, availableLines)

	// Compute total and visible line counts for scrollbar proportioning
	// (commits have variable height — 2 + edge lines each). Also compute
	// the line offset of the first visible entry so the thumb position
	// reflects scroll position accurately.
	var totalLines, visLines, firstVisLine int
	for i := range entries {
		cl := commitLines(entries[i])
		if i < off {
			firstVisLine += cl
		}
		if i >= off && i < end {
			visLines += cl
		}
		totalLines += cl
	}

	// Scrollbar: reserve columns on the right when content overflows.
	scrollW := width
	thumbStart, thumbEnd := scrollbarThumb(totalLines, firstVisLine, visLines, availableLines)
	hasBar := thumbStart >= 0
	if hasBar {
		scrollW -= scrollbarWidth
	}

	var lines []string
	lines = append(lines, blankRow(width, colPanel)) // top padding

	contentLine := 0 // 0-based line index within the content area (below top padding)

	for i := off; i < end; i++ {
		e := entries[i]
		highlighted := i == focus
		var bg lipgloss.TerminalColor = colPanel
		if highlighted {
			bg = colElement
		}

		// Header line.
		var hs []seg
		hs = append(hs, seg{text: " ", bg: bg})
		hs = append(hs, seg{text: e.HeaderPrefix, fg: colBorderSubtle, bg: bg})
		hs = append(hs, seg{text: " ", bg: bg})
		if e.ChangeIDPrefixLen > 0 && e.ChangeIDPrefixLen < len(e.ChangeID) {
			hs = append(hs, seg{text: e.ChangeID[:e.ChangeIDPrefixLen], fg: colMagenta, bold: true, bg: bg})
			hs = append(hs, seg{text: e.ChangeID[e.ChangeIDPrefixLen:], fg: colPurple, bold: true, bg: bg})
		} else {
			hs = append(hs, seg{text: e.ChangeID, fg: colPurple, bold: true, bg: bg})
		}
		hs = append(hs, seg{text: " ", bg: bg})
		hs = append(hs, seg{text: e.Authors, fg: colBlue, bg: bg})
		hs = append(hs, seg{text: " ", bg: bg})
		hs = append(hs, seg{text: e.Date, fg: colTextMuted, bg: bg})
		hs = append(hs, seg{text: " ", bg: bg})
		hs = append(hs, seg{text: e.CommitID, fg: colTextMuted, bg: bg})
		for _, bm := range e.Bookmarks {
			hs = append(hs, seg{text: " ", bg: bg})
			hs = append(hs, seg{text: bm, fg: colGreen, bold: true, bg: bg})
		}
		if rb.active && i == rb.source {
			tag := "  ● moving"
			if rb.subtree {
				tag = "  ● moving +descendants"
			}
			hs = append(hs, seg{text: tag, fg: colMagenta, bold: true, bg: bg})
		}
		if rb.active && i == rb.dest {
			hs = append(hs, seg{text: "  ◀ " + rebasePlaceLabels[rb.place], fg: colYellow, bold: true, bg: bg})
		}
		if sq.active && i == sq.source {
			hs = append(hs, seg{text: "  ● squashing", fg: colMagenta, bold: true, bg: bg})
		}
		if sq.active && i == sq.dest {
			hs = append(hs, seg{text: "  ◀ into", fg: colYellow, bold: true, bg: bg})
		}
		lines = append(lines, renderRowWithBar(scrollW, width, bg, hasBar, contentLine, thumbStart, thumbEnd, hs))
		contentLine++

		// Body line.
		var bs []seg
		bs = append(bs, seg{text: " ", bg: bg})
		bs = append(bs, seg{text: e.BodyPrefix, fg: colBorderSubtle, bg: bg})
		bs = append(bs, seg{text: " ", bg: bg})
		if aiLoading[e.ChangeID] {
			frame := spinnerFrames[spinnerFrame%len(spinnerFrames)]
			bs = append(bs, seg{text: frame + " generating…", fg: colMagenta, bold: true, bg: bg})
		} else {
			subject := e.Subject
			if subject == "" {
				subject = "(no description set)"
			}
			switch {
			case e.IsWorkingCopy:
				bs = append(bs, seg{text: subject, fg: colYellow, bold: true, bg: bg})
			case e.IsImmutable:
				bs = append(bs, seg{text: subject, fg: colTextMuted, faint: true, bg: bg})
			default:
				bs = append(bs, seg{text: subject, fg: colText, bg: bg})
			}
		}
		lines = append(lines, renderRowWithBar(scrollW, width, bg, hasBar, contentLine, thumbStart, thumbEnd, bs))
		contentLine++

		// Graph-only edge lines (merge connectors, elided "~" rows) always use
		// the panel background, not the selection highlight.
		edgeBg := colPanel
		for _, edge := range e.EdgeLines {
			lines = append(lines, renderRowWithBar(scrollW, width, edgeBg, hasBar, contentLine, thumbStart, thumbEnd, []seg{{text: " ", bg: edgeBg}, {text: edge, fg: colBorderSubtle, bg: edgeBg}}))
			contentLine++
		}
	}

	return padLines(lines, height)
}

// renderRowWithBar renders a content row to scrollW columns, then appends a
// scrollbar track (scrollbarWidth columns) to fill the full width. lineIdx is
// the 0-based index within the content area (excluding top padding), used to
// determine thumb position.
func renderRowWithBar(scrollW, fullW int, bg lipgloss.TerminalColor, hasBar bool, lineIdx, thumbStart, thumbEnd int, segs []seg) string {
	row := renderRow(scrollW, bg, segs)
	if !hasBar {
		return bgRow(fullW, bg, segs...)
	}
	// Scrollbar columns: a 1-column gap + the bar glyph.
	var sbSegs []seg
	if lineIdx >= thumbStart && lineIdx < thumbEnd {
		sbSegs = []seg{{text: " ", bg: bg}, {text: "┃", fg: colBorderActive, bg: bg}}
	} else {
		sbSegs = []seg{{text: " ", bg: bg}, {text: "│", fg: colBorderSubtle, bg: bg}}
	}
	scrollbar := renderSegs(sbSegs)
	// Pad row to scrollW if needed.
	rw := lipgloss.Width(row)
	if rw < scrollW {
		if bg != nil {
			row += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", scrollW-rw))
		} else {
			row += strings.Repeat(" ", scrollW-rw)
		}
	}
	result := row + scrollbar
	return clip(result, fullW)
}

// renderRowWithBarFromString is like renderRowWithBar but takes a pre-rendered
// string instead of []seg. Used by the diff panel and help view where rows are
// already styled with their own backgrounds.
func renderRowWithBarFromString(scrollW, fullW int, bg lipgloss.TerminalColor, hasBar bool, lineIdx, thumbStart, thumbEnd int, row string) string {
	if !hasBar {
		return clip(row, fullW)
	}
	row = clip(row, scrollW)
	var sbSegs []seg
	if lineIdx >= thumbStart && lineIdx < thumbEnd {
		sbSegs = []seg{{text: " ", bg: bg}, {text: "┃", fg: colBorderActive, bg: bg}}
	} else {
		sbSegs = []seg{{text: " ", bg: bg}, {text: "│", fg: colBorderSubtle, bg: bg}}
	}
	scrollbar := renderSegs(sbSegs)
	rw := lipgloss.Width(row)
	if rw < scrollW {
		if bg != nil {
			row += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", scrollW-rw))
		} else {
			row += strings.Repeat(" ", scrollW-rw)
		}
	}
	return clip(row+scrollbar, fullW)
}

// renderRow renders a row with an optional background fill.
func renderRow(width int, bg lipgloss.TerminalColor, segs []seg) string {
	if bg == nil {
		return plainRow(width, segs...)
	}
	return bgRow(width, bg, segs...)
}

// padLines pads (or truncates) a slice to exactly n lines.
func padLines(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}
