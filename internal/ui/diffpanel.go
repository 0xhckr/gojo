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
	return " " + padNum(r.oldNum, digits) + " " + padNum(r.newNum, digits) + " "
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

// diffLayout maps each diff body row (or raw line) to its wrapped terminal-line
// range within the body (the region below the description/status head). It is
// width-dependent: narrower terminals wrap more, so every row may occupy more
// than one terminal line. starts[i] is the first terminal-line index of row i
// (0-based within the body); counts[i] is how many terminal lines that row
// spans (≥1). total is the grand total. scrollW is the effective content width
// (terminal width minus the scrollbar reservation, when one is needed).
//
// The same pure computation drives both the scroll/offset math in the Model
// (navigation) and the visible-window rendering in renderDiffPanel, so wrapped
// rows never get misaligned between where the cursor thinks a line is and where
// it is actually drawn.
type diffLayout struct {
	starts  []int
	counts  []int
	total   int
	scrollW int
}

// rowAt maps a 0-based body terminal-line index to the (row index, sub-line)
// that occupies it. Sub-line 0 is the row's first wrapped line.
func (l diffLayout) rowAt(bodyLine int) (rowIdx, sub int) {
	if bodyLine < 0 {
		return 0, 0
	}
	lo, hi := 0, len(l.starts)
	for lo < hi {
		mid := (lo + hi) / 2
		if l.starts[mid] <= bodyLine {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0, 0
	}
	idx := lo - 1
	return idx, bodyLine - l.starts[idx]
}

// collapsedRowSet returns the set of diff row indices that belong to collapsed
// files (everything between a collapsed file header and the next file header).
// Returns nil when no files are collapsed.
func collapsedRowSet(rows []diffRow, collapsed map[string]bool) map[int]bool {
	if len(collapsed) == 0 {
		return nil
	}
	hidden := map[int]bool{}
	for i, r := range rows {
		if r.kind == rowFileHeader && collapsed[r.path] {
			for j := i + 1; j < len(rows); j++ {
				if rows[j].kind == rowFileHeader {
					break
				}
				hidden[j] = true
			}
		}
	}
	return hidden
}

// diffFileHeaderForRow returns the index of the file header row that contains
// the given row index, or -1 if none is found (e.g. rowIdx is before the first
// file header).
func diffFileHeaderForRow(rows []diffRow, rowIdx int) int {
	for i := rowIdx; i >= 0; i-- {
		if rows[i].kind == rowFileHeader {
			return i
		}
	}
	return -1
}

// diffRowWrapCount is the number of terminal lines a diff row occupies at
// scrollW columns. File/hunk headers wrap their label; content lines wrap the
// syntax-highlighted body after the line-number gutter. fileCollapsed only
// affects file header rows, adding a ▼/▶ indicator to the label. When
// splitActive is true, a 3-char indicator slot ([x]/[ ]/[~]) replaces the
// single-char prefix, widening the prefix by 2 columns. When fileMode is true,
// content lines use a single line-number column (no old/new split, no sign
// column), so the prefix is narrower.
func diffRowWrapCount(scrollW, digits int, r diffRow, fileCollapsed bool, splitActive bool, fileMode bool) int {
	switch r.kind {
	case rowFileHeader:
		indicator := "▼ "
		if fileCollapsed {
			indicator = "▶ "
		}
		label := indicator + r.path + "  (" + r.changeType + ")"
		if r.prevPath != "" {
			label = indicator + r.prevPath + " → " + r.path + "  (" + r.changeType + ")"
		}
		prefix := 2
		if splitActive {
			prefix = 4
		}
		return textWrapCount(label, max(1, scrollW-prefix))
	case rowHunkHeader:
		return textWrapCount(r.hunkText, max(1, scrollW-2))
	default:
		if fileMode {
			return spansWrapCount(r.spans, max(1, scrollW-(digits+3)))
		}
		gutter := 2*digits + 5
		if splitActive {
			gutter = 2*digits + 7
		}
		return spansWrapCount(r.spans, max(1, scrollW-gutter))
	}
}

// computeDiffLayoutPure builds the wrapped-line layout for the diff body. The
// scrollbar reservation depends on whether the body overflows the viewport,
// which in turn depends on the wrap width, so it is resolved in two passes:
// first at the full width, then (if overflow) at width minus the scrollbar.
func computeDiffLayoutPure(width, contentH, headLen int, rows []diffRow, raw string, digits int, collapsed map[string]bool, splitActive bool, fileMode bool) diffLayout {
	scrollW := width
	if scrollW < 1 {
		scrollW = 1
	}
	var rawLines []string
	if len(rows) == 0 && raw != "" {
		rawLines = strings.Split(raw, "\n")
	}
	var counts []int
	for pass := 0; pass < 2; pass++ {
		if rawLines != nil {
			counts = make([]int, len(rawLines))
			for i, l := range rawLines {
				counts[i] = textWrapCount(l, max(1, scrollW-1))
			}
		} else {
			hidden := collapsedRowSet(rows, collapsed)
			counts = make([]int, len(rows))
			for i, r := range rows {
				if hidden != nil && hidden[i] {
					counts[i] = 0
					continue
				}
				isCollapsed := r.kind == rowFileHeader && collapsed != nil && collapsed[r.path]
				counts[i] = diffRowWrapCount(scrollW, digits, r, isCollapsed, splitActive, fileMode)
			}
		}
		total := 0
		for _, c := range counts {
			total += c
		}
		if total <= contentH || pass == 1 {
			break
		}
		scrollW = width - scrollbarWidth
		if scrollW < 1 {
			scrollW = 1
		}
	}
	starts := make([]int, len(counts))
	acc := 0
	for i, c := range counts {
		starts[i] = acc
		acc += c
	}
	return diffLayout{starts: starts, counts: counts, total: acc, scrollW: scrollW}
}

// renderDiffPanel produces exactly height lines for the diff panel. Only the
// visible window of rows is styled, so scroll cost is independent of diff size.
// Long diff lines wrap onto additional terminal lines; the wrapped-line layout
// (computeDiffLayoutPure) keeps the scroll window, scrollbar and chunk cursor
// aligned with the actual on-screen row positions.
//
// cursorBodyRow is the terminal body-line index of the focused change line
// (-1 if none); chunkRows is the set of body-row indices (headLen + rowIdx)
// that belong to the focused chunk. A thin left-edge bar is drawn for those
// rows: bright for the cursor line, dim for the rest of the chunk.
func renderDiffPanel(width, height int, rev string, revPrefixLen int, loading bool, aiLoading bool, spinnerFrame int, desc string, showDesc bool, rows []diffRow, digits int, status []jj.StatusEntry, rawContent string, scrollY int, cursorBodyRow int, chunkRows map[int]bool, collapsed map[string]bool, sv splitView, fileMode bool, fileHead []string) []string {
	// Title bar — the only sticky chrome; description + status + separator +
	// diff all scroll together below it as one body. The revision ID uses the
	// same two-tone highlighting as the log view: the shortest-unique prefix
	// in magenta, the rest in purple. In file mode the title shows the file
	// path in bold text with a "back" hint instead of the diff close hint.
	var titleSegs []seg
	titleSegs = append(titleSegs, seg{text: " ", fg: colText, bg: colPanel})
	if fileMode {
		titleSegs = append(titleSegs, seg{text: rev, fg: colText, bold: true, bg: colPanel})
		if loading {
			titleSegs = append(titleSegs, seg{text: "  loading…", fg: colText, bg: colPanel})
		}
		titleSegs = append(titleSegs, seg{text: "  (esc/q to back) ", fg: colTextMuted, bg: colPanel})
	} else {
		if revPrefixLen > 0 && revPrefixLen < len(rev) {
			titleSegs = append(titleSegs, seg{text: rev[:revPrefixLen], fg: colMagenta, bold: true, bg: colPanel})
			titleSegs = append(titleSegs, seg{text: rev[revPrefixLen:], fg: colTextMuted, bg: colPanel})
		} else {
			titleSegs = append(titleSegs, seg{text: rev, fg: colMagenta, bold: true, bg: colPanel})
		}
		if loading {
			titleSegs = append(titleSegs, seg{text: "  loading…", fg: colText, bg: colPanel})
		}
		titleSegs = append(titleSegs, seg{text: "  (enter/q to close) ", fg: colText, bg: colPanel})
	}
	out := []string{bgRow(width, colPanel, titleSegs...)}

	// In file mode the blame header is sticky chrome (always visible), not
	// part of the scrollable body, so the user always knows which commit owns
	// the cursor line.
	if fileMode && len(fileHead) > 0 {
		out = append(out, fileHead...)
	}

	contentH := height - len(out)
	if contentH < 0 {
		contentH = 0
	}

	// The scrollable body is: description header + status header + status
	// items + separators + diff. The head lines themselves do not wrap; only
	// the diff/raw body wraps. In file mode the body is just the file's lines
	// (the blame header is rendered as sticky chrome above).
	var head []string
	if !fileMode {
		if showDesc {
			head = append(head, buildDescHead(width, desc, aiLoading, spinnerFrame)...)
		}
		head = append(head, buildStatusHead(width, status)...)
		head = append(head, buildChangesHead(width)...)
	}
	headLen := len(head)

	layout := computeDiffLayoutPure(width, contentH, headLen, rows, rawContent, digits, collapsed, sv.active, fileMode)
	bodyTotal := headLen + layout.total

	start, end := visibleRange(scrollY, contentH, bodyTotal)

	// Scrollbar: the layout already reserved columns when content overflows.
	scrollW := layout.scrollW
	thumbStart, thumbEnd := scrollbarThumb(bodyTotal, start, end-start, contentH)
	hasBar := thumbStart >= 0

	// Resolve the focused logical row index from the cursor's terminal line,
	// so the cursor/chunk bar can be coloured per visible row.
	cursorRowIdx := -1
	if cursorBodyRow >= headLen {
		if bl := cursorBodyRow - headLen; bl >= 0 && bl < layout.total {
			cursorRowIdx, _ = layout.rowAt(bl)
		}
	}

	var rawLines []string
	if len(rows) == 0 && rawContent != "" {
		rawLines = strings.Split(rawContent, "\n")
	}

	var content []string
	for i := start; i < end; i++ {
		rowLine := i - start // 0-based index within the visible window
		if i < headLen {
			content = append(content, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, rowLine, thumbStart, thumbEnd, head[i]))
			continue
		}
		bodyLine := i - headLen
		if rawLines != nil {
			ri, sub := layout.rowAt(bodyLine)
			str := renderRawSubLine(scrollW, rawLines[ri], sub)
			content = append(content, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, rowLine, thumbStart, thumbEnd, str))
		} else {
			ri, sub := layout.rowAt(bodyLine)
			r := rows[ri]
			isCursor := ri == cursorRowIdx
			inChunk := chunkRows != nil && chunkRows[headLen+ri]
			isCollapsed := r.kind == rowFileHeader && collapsed != nil && collapsed[r.path]
			splitInd := splitIndicatorForRow(rows, ri, sv)
			var barColor lipgloss.TerminalColor
			if fileMode {
				p := r.sectionParity
				if isCursor {
					barColor = fileSectionBarBright[p%len(fileSectionBarBright)]
				} else if inChunk {
					barColor = fileSectionBarDim[p%len(fileSectionBarDim)]
				}
			} else {
				barColor = cursorBar(r, isCursor, inChunk)
			}
			str := renderDiffRowSubLine(scrollW, digits, r, sub, barColor, isCollapsed, isCursor, splitInd, sv.active, fileMode)
			var rowBg lipgloss.TerminalColor
			if fileMode {
				rowBg = fileRowBg(r)
			} else {
				rowBg = diffRowBg(r)
				if r.kind == rowFileHeader && isCursor {
					rowBg = diffFileHeaderFg
				}
			}
			content = append(content, renderRowWithBarFromString(scrollW, width, rowBg, hasBar, rowLine, thumbStart, thumbEnd, str))
		}
	}

	content = padLines(content, contentH)
	out = append(out, content...)
	return padLines(out, height)
}

// diffRowBg is the background colour for a diff row's terminal lines.
func diffRowBg(r diffRow) lipgloss.TerminalColor {
	switch {
	case r.kind == rowFileHeader:
		return diffFileHeaderBg
	case r.kind == rowHunkHeader:
		return diffHunkHeaderBg
	case r.lineKind == "addition":
		return diffAddedBg
	case r.lineKind == "deletion":
		return diffRemovedBg
	default:
		return colPanel
	}
}

// fileRowBg is the background colour for a file-view row: the row's
// alternating section tint (falling back to colPanel when unset).
func fileRowBg(r diffRow) lipgloss.TerminalColor {
	if r.sectionBg != nil {
		return r.sectionBg
	}
	return colPanel
}

// renderRawSubLine renders sub-line `sub` of a raw (list-output) line, wrapped
// at scrollW-1 columns with a leading space.
func renderRawSubLine(scrollW int, line string, sub int) string {
	bodyW := max(1, scrollW-1)
	wrapped := wrapSegs([]seg{{text: line, fg: colText}}, bodyW)
	var body []seg
	if sub >= 0 && sub < len(wrapped) {
		body = wrapped[sub]
	}
	segs := append([]seg{{text: " ", fg: nil}}, body...)
	return bgRow(scrollW, colPanel, segs...)
}

// renderDiffRowSubLine renders a single wrapped sub-line (sub ≥ 0) of a diff
// row at scrollW columns. Sub-line 0 carries the real line-number gutter and
// sign; continuation sub-lines blank the gutter/sign so the wrapped content
// aligns under the original content while the gutter columns keep the row's
// background. The left cursor bar (┃) is drawn on every sub-line so a focused
// wrapped line stays visually marked end-to-end.
func renderDiffRowSubLine(scrollW, digits int, r diffRow, sub int, barColor lipgloss.TerminalColor, fileCollapsed bool, isCursor bool, splitIndicator string, splitActive bool, fileMode bool) string {
	switch r.kind {
	case rowFileHeader:
		indicator := "▼ "
		if fileCollapsed {
			indicator = "▶ "
		}
		label := indicator + r.path + "  (" + r.changeType + ")"
		if r.prevPath != "" {
			label = indicator + r.prevPath + " → " + r.path + "  (" + r.changeType + ")"
		}
		labelFg, labelBg := diffFileHeaderFg, diffFileHeaderBg
		if isCursor {
			labelFg, labelBg = diffFileHeaderBg, diffFileHeaderFg
		}
		prefix := 2
		if splitActive {
			prefix = 4
		}
		bodyW := max(1, scrollW-prefix)
		wrapped := wrapSegs([]seg{{text: label, fg: labelFg, bold: true, bg: labelBg}}, bodyW)
		var segs []seg
		if sub == 0 {
			barFg := diffFileHeaderFg
			if barColor != nil {
				barFg = barColor
			}
			if splitActive {
				indFg := splitIndicatorColor(splitIndicator)
				if indFg == nil {
					indFg = labelFg
				}
				indText := splitIndicator
				if indText == "" {
					indText = "   "
				}
				segs = []seg{
					{text: "┃", fg: barFg, bold: true, bg: colPanel},
					{text: indText, fg: indFg, bold: true, bg: labelBg},
				}
			} else {
				segs = []seg{
					{text: "┃", fg: barFg, bold: true, bg: colPanel},
					{text: " ", fg: labelFg, bold: true, bg: labelBg},
				}
			}
		} else {
			if splitActive {
				segs = []seg{
					{text: " ", bg: colPanel},
					{text: "   ", bg: labelBg},
				}
			} else {
				segs = []seg{
					{text: " ", bg: colPanel},
					{text: " ", bg: labelBg},
				}
			}
		}
		if sub >= 0 && sub < len(wrapped) {
			segs = append(segs, wrapped[sub]...)
		}
		return bgRow(scrollW, labelBg, segs...)

	case rowHunkHeader:
		bodyW := max(1, scrollW-2)
		wrapped := wrapSegs([]seg{{text: r.hunkText, fg: diffHunkHeaderFg, bg: diffHunkHeaderBg}}, bodyW)
		var segs []seg
		if sub == 0 {
			segs = []seg{
				{text: "┃", fg: diffHunkHeaderFg, bg: colPanel},
				{text: " ", fg: diffHunkHeaderFg, bg: diffHunkHeaderBg},
			}
		} else {
			segs = []seg{
				{text: " ", bg: colPanel},
				{text: " ", bg: diffHunkHeaderBg},
			}
		}
		if sub >= 0 && sub < len(wrapped) {
			segs = append(segs, wrapped[sub]...)
		}
		return bgRow(scrollW, diffHunkHeaderBg, segs...)

	default:
		if fileMode {
			// File viewer mode: single line number, no sign column.
			// Background alternates per blame section (r.sectionBg). The left
			// ┃ bar highlights the entire section, bright on the cursor line.
			bg := fileRowBg(r)
			lineFg := diffContextFg
			prefixW := digits + 3 // leftBar(1) + gutter(1+digits+1)
			bodyW := max(1, scrollW-prefixW)

			var bodySegs []seg
			for _, s := range r.spans {
				var fg lipgloss.TerminalColor = lineFg
				if s.fg != "" {
					fg = lipgloss.Color(s.fg)
				}
				bodySegs = append(bodySegs, seg{text: s.text, fg: fg, bg: bg})
			}
			wrapped := wrapSegs(bodySegs, bodyW)

			leftBar := seg{text: "┃", fg: bg, bg: bg}
			if barColor != nil {
				leftBar = seg{text: "┃", fg: barColor, bg: bg}
			}

			var gutterSegs []seg
			if sub == 0 {
				gutterSegs = []seg{
					{text: " " + padNum(r.newNum, digits) + " ", fg: diffLineNumber, bg: bg},
				}
			} else {
				gutterSegs = []seg{
					{text: strings.Repeat(" ", digits+2), bg: bg},
				}
			}

			segs := []seg{leftBar}
			segs = append(segs, gutterSegs...)
			if sub >= 0 && sub < len(wrapped) {
				segs = append(segs, wrapped[sub]...)
			}
			return bgRow(scrollW, bg, segs...)
		}

		var lineFg, lineBg lipgloss.TerminalColor
		switch r.lineKind {
		case "addition":
			lineFg, lineBg = diffAddedSign, diffAddedBg
		case "deletion":
			lineFg, lineBg = diffRemovedSign, diffRemovedBg
		default:
			lineFg = diffContextFg
		}

		prefixW := 2*digits + 8 // leftBar + gutter(incl leading space) + gap + sign + 2 trailing spaces
		if splitActive {
			prefixW = 2*digits + 10 // indicator slot is 3 chars instead of 1
		}
		bodyW := max(1, scrollW-prefixW)

		// Build the wrapping body: the syntax-highlighted spans, all carrying
		// the line background so continuation lines stay colour-continuous.
		// Spans with a per-token word-diff bg override use that instead.
		var bodySegs []seg
		for _, s := range r.spans {
			var fg lipgloss.TerminalColor = lineFg
			if s.fg != "" {
				fg = lipgloss.Color(s.fg)
			}
			bg := lineBg
			if s.bg != "" {
				bg = lipgloss.Color(s.bg)
			}
			bodySegs = append(bodySegs, seg{text: s.text, fg: fg, bg: bg})
		}
		wrapped := wrapSegs(bodySegs, bodyW)

		// Left cursor bar: ┃ before line numbers, on every sub-line. Bright on
		// cursor, dim on chunk, panel-coloured (invisible) elsewhere.
		leftBar := seg{text: "┃", fg: colPanel, bg: colPanel}
		if barColor != nil {
			leftBar = seg{text: "┃", fg: barColor, bg: colPanel}
		}

		// Gutter: real line numbers on sub-line 0, blank thereafter. Uses a
		// dimmer tint than the content area so the gutter is less opaque.
		// In split mode, the leading space is replaced by a 3-char indicator
		// slot ([x]/[ ]/[~] for selectable lines, 3 spaces for context lines).
		var gutterBg lipgloss.TerminalColor
		switch r.lineKind {
		case "addition":
			gutterBg = diffAddedGutterBg
		case "deletion":
			gutterBg = diffRemovedGutterBg
		}
		gutterBlankW := 2*digits + 3
		if splitActive {
			gutterBlankW = 2*digits + 5
		}
		var gutterSegs []seg
		if sub == 0 {
			if splitActive {
				indFg := splitIndicatorColor(splitIndicator)
				if indFg == nil {
					indFg = diffLineNumber
				}
				indText := splitIndicator
				if indText == "" {
					indText = "   "
				}
				gutterSegs = []seg{
					{text: indText, fg: indFg, bold: true, bg: gutterBg},
					{text: padNum(r.oldNum, digits) + " " + padNum(r.newNum, digits) + " ", fg: diffLineNumber, bg: gutterBg},
				}
			} else {
				gutterSegs = []seg{
					{text: lineNumText(r, digits), fg: diffLineNumber, bg: gutterBg},
				}
			}
		} else {
			gutterSegs = []seg{
				{text: strings.Repeat(" ", gutterBlankW), bg: gutterBg},
			}
		}

		// Sign: real sign on sub-line 0, blank after. A one-space gap separates
		// the sign column from the line numbers; two spaces follow the sign.
		var signSeg seg
		if sub == 0 {
			signSeg = seg{text: " " + r.sign + "  ", fg: lineFg, bg: lineBg}
		} else {
			signSeg = seg{text: "    ", bg: lineBg}
		}

		segs := []seg{leftBar}
		segs = append(segs, gutterSegs...)
		segs = append(segs, signSeg)
		if sub >= 0 && sub < len(wrapped) {
			segs = append(segs, wrapped[sub]...)
		}
		if lineBg == nil {
			return bgRow(scrollW, colPanel, segs...)
		}
		return bgRow(scrollW, lineBg, segs...)
	}
}

// buildDescHead renders the description label, the description text (one row
// buildDescHead renders the description label, the description text (one row
// per line), and a horizontal divider — shown above the status section. When
// the description is empty, a "(no description set)" placeholder is shown.
// When aiLoading is true, a spinner replaces the description text.
func buildDescHead(width int, desc string, aiLoading bool, spinnerFrame int) []string {
	head := []string{bgRow(width, colPanel, seg{text: "┃ ", fg: colCyan, bold: true, bg: colPanel}, seg{text: "description", fg: colTextMuted, bg: colPanel})}
	if aiLoading {
		frame := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		head = append(head, bgRow(width, colPanel, seg{text: "  " + frame + " generating…", fg: colMagenta, bold: true, bg: colPanel}))
	} else {
		text := desc
		if text == "" {
			text = "(no description set)"
		}
		for _, line := range strings.Split(text, "\n") {
			head = append(head, bgRow(width, colPanel, seg{text: "  " + line, fg: colText, bg: colPanel}))
		}
	}
	head = append(head, bgRow(width, colPanel, seg{text: strings.Repeat("─", width), fg: colBorder, bg: colPanel}))
	return head
}

// descHeadLen is the number of body rows a description header occupies: the
// label, one row per description line (at least one), and the divider.
func descHeadLen(desc string) int {
	lines := 1
	if desc != "" {
		lines = strings.Count(desc, "\n") + 1
	}
	return 1 + lines + 1
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

// buildChangesHead renders the "changes" label that precedes the diff body.
func buildChangesHead(width int) []string {
	return []string{bgRow(width, colPanel, seg{text: "┃ ", fg: colCyan, bold: true, bg: colPanel}, seg{text: "changes", fg: colTextMuted, bg: colPanel})}
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

// cursorBar picks the foreground color for the ┃ cursor glyph on a content
// row. The focused row gets a bright color; other rows in the same chunk get a
// dim tint; everything else gets nothing. File header rows get a yellow cursor
// bar when focused. The bar is drawn on every wrapped sub-line of the row, so
// it is evaluated per logical row (not per terminal line).
func cursorBar(r diffRow, isCursor, inChunk bool) lipgloss.TerminalColor {
	if r.kind == rowFileHeader {
		if isCursor {
			return colYellow
		}
		return nil
	}
	if r.kind != rowLine {
		return nil
	}
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

// splitIndicatorColor returns the foreground color for a split-mode indicator
// glyph: green for marked ([x]), yellow for partial ([~]), gray for unmarked
// ([ ]). Returns nil for unknown indicators.
func splitIndicatorColor(indicator string) lipgloss.TerminalColor {
	switch indicator {
	case "[x]":
		return splitMarked
	case "[~]":
		return splitPartial
	case "[ ]":
		return splitUnmarked
	default:
		return nil
	}
}
