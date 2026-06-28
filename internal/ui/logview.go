package ui

import (
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

func commitLines(e jj.LogEntry) int { return 2 + len(e.EdgeLines) }

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

// renderLog produces up to height lines for the commit log.
func renderLog(width, height int, entries []jj.LogEntry, cursor, offset int, aiLoading map[string]bool, spinnerFrame int, rb rebaseView, sq squashView) []string {
	if len(entries) == 0 {
		return padLines([]string{plainRow(width, seg{text: "  no revisions found", fg: colGray})}, height)
	}

	// In rebase / squash mode the destination indicator is the focused row that
	// drives scrolling and carries the cursor highlight.
	focus := cursor
	if rb.active {
		focus = rb.dest
	}
	if sq.active {
		focus = sq.dest
	}

	availableLines := height - 1 // top padding
	off, end := logWindow(entries, focus, offset, availableLines)

	var lines []string
	lines = append(lines, "") // top padding

	for i := off; i < end; i++ {
		e := entries[i]
		highlighted := i == focus
		var bg lipgloss.TerminalColor
		if highlighted {
			bg = colDarkPurple
		}

		// Header line.
		var hs []seg
		hs = append(hs, seg{text: e.HeaderPrefix, fg: colDarkGray})
		hs = append(hs, seg{text: " "})
		if e.ChangeIDPrefixLen > 0 && e.ChangeIDPrefixLen < len(e.ChangeID) {
			hs = append(hs, seg{text: e.ChangeID[:e.ChangeIDPrefixLen], fg: colMagenta, bold: true})
			hs = append(hs, seg{text: e.ChangeID[e.ChangeIDPrefixLen:], fg: colPurple, bold: true})
		} else {
			hs = append(hs, seg{text: e.ChangeID, fg: colPurple, bold: true})
		}
		hs = append(hs, seg{text: " "})
		hs = append(hs, seg{text: e.Authors, fg: colBlue})
		hs = append(hs, seg{text: " "})
		hs = append(hs, seg{text: e.Date, fg: colGray})
		hs = append(hs, seg{text: " "})
		hs = append(hs, seg{text: e.CommitID, fg: colGray})
		for _, bm := range e.Bookmarks {
			hs = append(hs, seg{text: " "})
			hs = append(hs, seg{text: bm, fg: colGreen, bold: true})
		}
		// Rebase-mode markers: source (the picked-up commit) and destination
		// (where it will land, labelled with the current placement).
		if rb.active && i == rb.source {
			tag := "  ● moving"
			if rb.subtree {
				tag = "  ● moving +descendants"
			}
			hs = append(hs, seg{text: tag, fg: colMagenta, bold: true})
		}
		if rb.active && i == rb.dest {
			hs = append(hs, seg{text: "  ◀ " + rebasePlaceLabels[rb.place], fg: colYellow, bold: true})
		}
		// Squash-mode markers: source (the commit being folded in) and
		// destination (the commit it gets squashed into).
		if sq.active && i == sq.source {
			hs = append(hs, seg{text: "  ● squashing", fg: colMagenta, bold: true})
		}
		if sq.active && i == sq.dest {
			hs = append(hs, seg{text: "  ◀ into", fg: colYellow, bold: true})
		}
		lines = append(lines, renderRow(width, bg, hs))

		// Body line.
		var bs []seg
		bs = append(bs, seg{text: e.BodyPrefix, fg: colDarkGray})
		bs = append(bs, seg{text: " "})
		if aiLoading[e.ChangeID] {
			frame := spinnerFrames[spinnerFrame%len(spinnerFrames)]
			bs = append(bs, seg{text: frame + " generating…", fg: colMagenta, bold: true})
		} else {
			subject := e.Subject
			if subject == "" {
				subject = "(no description set)"
			}
			switch {
			case e.IsWorkingCopy:
				bs = append(bs, seg{text: subject, fg: colYellow, bold: true})
			case e.IsImmutable:
				bs = append(bs, seg{text: subject, faint: true})
			default:
				bs = append(bs, seg{text: subject, fg: colWhite})
			}
		}
		lines = append(lines, renderRow(width, bg, bs))

		// Graph-only edge lines (merge connectors, elided "~" rows) trail the
		// commit they belong to — jj always draws them below a node's text.
		for _, edge := range e.EdgeLines {
			lines = append(lines, plainRow(width, seg{text: edge, fg: colDarkGray}))
		}
	}

	return padLines(lines, height)
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
