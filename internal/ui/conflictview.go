package ui

// conflictview.go — the side-by-side conflict resolution view.
//
// Opened with `c` on a commit that has conflicts (or from the diff panel).
// The left pane shows side #1, the right pane side #2 of each conflicted
// file; clean regions stream down both panes already merged. The user walks
// conflict hunks one by one (j/k, wheel) and picks a side per hunk —
// l/←/h = left, r/→ = right, b = both, u = undo. When every hunk in a file
// has a resolution, ⏎ applies it through `jj resolve` with a throwaway merge
// tool that copies the composed content into jj's $output (see
// jj.Runner.FetchConflictSides and jj.Runner.Resolve).

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// conflictState is the root state of the conflict resolution view.
type conflictState struct {
	rev       string // revision the conflicts belong to (change id)
	revPrefix int    // shortest-unique-prefix length for display
	files     []conflictFile
	cur       int // current file tab
	cursor    int // index into the current file's conflicts list
	scrollY   int // first visible body row (absolute row index)
}

// conflictFile is one conflicted path plus its merged block structure.
type conflictFile struct {
	path string
	// note, when non-empty, marks the file unresolvable here (e.g. a
	// >2-sided conflict or a kind jj won't feed to a merge tool); the body
	// renders the note instead of hunks.
	note      string
	blocks    []mergeBlock
	conflicts []int // indexes into blocks that are mergeConflict
	done      bool  // resolution applied via jj resolve

	// Rows are the visible content lines, 1:1 with terminal rows (no
	// wrapping; long lines clip). Choices never reshuffle them, so they are
	// cached at load.
	rows      []conflictRow
	rowStarts []int // per-block starting row
}

// conflictRowKind tags each visible row.
type conflictRowKind int

const (
	cRowContext     conflictRowKind = iota // unchanged everywhere
	cRowAuto                               // auto-merged (one side changed)
	cConflictHeader                        // full-width "conflict k/n" header
	cConflictLine                          // left/right candidate lines (blank pad on a side)
)

// conflictRow is one visible row of the conflict view.
type conflictRow struct {
	kind  conflictRowKind
	block int    // index into the file's blocks
	l, r  string // pane texts ("" on a padded side)
	hasL  bool   // l belongs to the block (false = padded blank)
	hasR  bool
}

// deriveRows flattens the blocks into display rows and records row geometry.
func (f *conflictFile) deriveRows() {
	f.rows = nil
	f.rowStarts = make([]int, len(f.blocks))
	trim := func(s string) string { return strings.TrimSuffix(s, "\n") }
	for i, b := range f.blocks {
		f.rowStarts[i] = len(f.rows)
		switch b.kind {
		case mergeContext:
			for _, l := range b.base {
				t := trim(l)
				f.rows = append(f.rows, conflictRow{kind: cRowContext, block: i, l: t, r: t, hasL: true, hasR: true})
			}
		case mergeAuto:
			for _, l := range b.auto {
				t := trim(l)
				f.rows = append(f.rows, conflictRow{kind: cRowAuto, block: i, l: t, r: t, hasL: true, hasR: true})
			}
		case mergeConflict:
			f.rows = append(f.rows, conflictRow{kind: cConflictHeader, block: i})
			n := max(len(b.left), len(b.right))
			for k := 0; k < n; k++ {
				row := conflictRow{kind: cConflictLine, block: i}
				if k < len(b.left) {
					row.l, row.hasL = trim(b.left[k]), true
				}
				if k < len(b.right) {
					row.r, row.hasR = trim(b.right[k]), true
				}
				f.rows = append(f.rows, row)
			}
		}
	}
}

// totalRows is the number of content rows for the current file.
func (f conflictFile) totalRows() int { return len(f.rows) }

// unresolved returns the number of conflict blocks without a choice.
func (f conflictFile) unresolved() int {
	n := 0
	for _, bi := range f.conflicts {
		if f.blocks[bi].choice == resolutionUnset {
			n++
		}
	}
	return n
}

// resolvable reports whether this file can be resolved in the view.
func (f conflictFile) resolvable() bool { return f.note == "" }

// curFile returns the active file, or nil when there are none.
func (m Model) curConflictFile() *conflictFile {
	if len(m.conflict.files) == 0 {
		return nil
	}
	return &m.conflict.files[m.conflict.cur]
}

// conflictMaxScroll returns the maximum scrollY for the current file.
func (m Model) conflictMaxScroll() int {
	f := m.curConflictFile()
	if f == nil {
		return 0
	}
	return max(0, f.totalRows()-m.conflictBodyHeight())
}

// conflictBodyHeight is the number of content rows visible below the tab bar
// and pane header.
func (m Model) conflictBodyHeight() int {
	return max(0, m.contentHeight()-2)
}

// conflictClampScroll clamps scrollY into [0, conflictMaxScroll].
func (m *Model) conflictClampScroll() {
	m.conflict.scrollY = max(0, min(m.conflict.scrollY, m.conflictMaxScroll()))
}

// conflictFocusBlock returns the focused conflict's block index (-1 when the
// file has no conflicts).
func (m Model) conflictFocusBlock() int {
	f := m.curConflictFile()
	if f == nil || len(f.conflicts) == 0 {
		return -1
	}
	cur := max(0, min(m.conflict.cursor, len(f.conflicts)-1))
	return f.conflicts[cur]
}

// conflictBlockHeight is the number of rows a block occupies.
func conflictBlockHeight(b mergeBlock) int {
	switch b.kind {
	case mergeContext:
		return len(b.base)
	case mergeAuto:
		return len(b.auto)
	case mergeConflict:
		return 1 + max(len(b.left), len(b.right))
	}
	return 0
}

// conflictFollowCursor ensures the focused conflict block is fully visible
// (blocks taller than the viewport are aligned to their top).
func (m *Model) conflictFollowCursor() {
	f := m.curConflictFile()
	b := m.conflictFocusBlock()
	if f == nil || b < 0 {
		return
	}
	bodyH := m.conflictBodyHeight()
	start, h := f.rowStarts[b], conflictBlockHeight(f.blocks[b])
	if start < m.conflict.scrollY {
		m.conflict.scrollY = start
	} else if h > bodyH {
		m.conflict.scrollY = start
	} else if start+h > m.conflict.scrollY+bodyH {
		m.conflict.scrollY = start + h - bodyH
	}
	m.conflictClampScroll()
}

// conflictSetChoice resolves the focused conflict block and, when the choice
// is a resolution (not an undo), advances to the next unresolved conflict.
func (m *Model) conflictSetChoice(r resolution) {
	f := m.curConflictFile()
	if f == nil || f.done || !f.resolvable() {
		return
	}
	cur := max(0, min(m.conflict.cursor, len(f.conflicts)-1))
	if len(f.conflicts) == 0 {
		return
	}
	f.blocks[f.conflicts[cur]].choice = r
	if r == resolutionUnset {
		return
	}
	m.errMsg = ""
	n := len(f.conflicts)
	for k := 1; k <= n; k++ {
		idx := f.conflicts[(cur+k)%n]
		if f.blocks[idx].choice == resolutionUnset {
			m.conflict.cursor = (cur + k) % n
			m.conflictFollowCursor()
			return
		}
	}
	// All resolved: stay put so the user can review and hit ⏎.
}

// conflictSwitchFile moves the tab by delta (±1, wrapping), resetting the
// per-file cursor to its first unresolved (or first) conflict.
func (m *Model) conflictSwitchFile(delta int) {
	n := len(m.conflict.files)
	if n == 0 {
		return
	}
	m.conflict.cur = ((m.conflict.cur+delta)%n + n) % n
	m.conflict.scrollY = 0
	m.conflict.cursor = 0
	f := m.curConflictFile()
	if f != nil && len(f.conflicts) > 0 {
		for i, bi := range f.conflicts {
			if f.blocks[bi].choice == resolutionUnset {
				m.conflict.cursor = i
				break
			}
		}
		m.conflictFollowCursor()
	}
}

// ── Messages ────────────────────────────────────────────────────────────────

type conflictLoadedMsg struct {
	rev       string
	revPrefix int
	files     []conflictFile
	err       error
}

// resolveFinishedMsg is returned after `jj resolve` for one conflicted path.
type resolveFinishedMsg struct {
	rev  string
	path string
	err  error
	elev *elevReq
}

// ── Commands ────────────────────────────────────────────────────────────────

// openConflictCmd lists the conflicts at rev and materializes each 2-sided
// one (fetching the three full file versions through a probe merge tool) so
// the blocks can be computed up front. Files with kinds jj won't merge are
// carried with an explanatory note.
func (m Model) openConflictCmd(rev string, revPrefix int) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		entries, err := r.Conflicts(rev)
		if err != nil {
			return conflictLoadedMsg{rev: rev, err: err}
		}
		var files []conflictFile
		for _, ce := range entries {
			cf := conflictFile{path: ce.Path}
			if ce.Sides != 2 {
				cf.note = fmt.Sprintf("%d-sided conflicts can't be resolved with a merge tool", ce.Sides)
				files = append(files, cf)
				continue
			}
			sides, ferr := r.FetchConflictSides(rev, ce.Path)
			if ferr != nil {
				cf.note = "this conflict can't be resolved with a 3-way merge tool"
				files = append(files, cf)
				continue
			}
			cf.blocks = merge3Strings(sides.Base, sides.Left, sides.Right)
			for i := range cf.blocks {
				if cf.blocks[i].kind == mergeConflict {
					cf.conflicts = append(cf.conflicts, i)
				}
			}
			cf.deriveRows()
			files = append(files, cf)
		}
		return conflictLoadedMsg{rev: rev, revPrefix: revPrefix, files: files}
	}
}

// applyConflictCmd composes the resolved content for file and applies it via
// `jj resolve` with a throwaway merge tool that copies the composed content
// into jj's $output. extra carries elevation flags on retries.
func (m Model) applyConflictCmd(f conflictFile, extra ...string) tea.Cmd {
	r := m.runner
	rev := m.conflict.rev
	path := f.path
	resolved, err := composeResolved(f.blocks)
	if err != nil {
		return func() tea.Msg {
			return resolveFinishedMsg{rev: rev, path: path, err: err}
		}
	}
	return func() tea.Msg {
		err := r.Resolve(rev, path, resolved, extra...)
		if err != nil {
			if len(extra) == 0 {
				if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
					return resolveFinishedMsg{rev: rev, path: path, err: err, elev: &elevReq{
						flag:   flag,
						reason: reason,
						retry:  func() tea.Cmd { return m.applyConflictCmd(f, flag) },
					}}
				}
			}
			return resolveFinishedMsg{rev: rev, path: path, err: err}
		}
		return resolveFinishedMsg{rev: rev, path: path}
	}
}

// execConflictApply kicks off resolution of the current file (busy spinner),
// or reports why it can't run yet.
func (m Model) execConflictApply() (tea.Model, tea.Cmd) {
	f := m.curConflictFile()
	if f == nil {
		return m, nil
	}
	if f.done {
		m.message = f.path + " already resolved"
		return m, nil
	}
	if !f.resolvable() {
		m.errMsg = f.note
		return m, nil
	}
	if n := f.unresolved(); n > 0 {
		word := "hunks"
		if n == 1 {
			word = "hunk"
		}
		m.errMsg = fmt.Sprintf("%d conflict %s unresolved in %s — pick a side for each hunk first (l/r/b, u to undo)", n, word, f.path)
		return m, nil
	}
	m, tick := m.startBusy("resolving " + f.path + "…")
	return m, tea.Batch(tick, m.applyConflictCmd(*f))
}

// ── Keys ────────────────────────────────────────────────────────────────────

func (m Model) handleConflictKey(k string) (tea.Model, tea.Cmd) {
	f := m.curConflictFile()
	n := 0
	if f != nil {
		n = len(f.conflicts)
	}
	bodyH := m.conflictBodyHeight()

	switch k {
	case "q", "esc":
		m.conflictOpen = false
		return m, nil
	case "up", "k":
		if n > 0 {
			m.conflict.cursor = (m.conflict.cursor - 1 + n) % n
			m.conflictFollowCursor()
		}
	case "down", "j":
		if n > 0 {
			m.conflict.cursor = (m.conflict.cursor + 1) % n
			m.conflictFollowCursor()
		}
	case "home", "g":
		if n > 0 {
			m.conflict.cursor = 0
			m.conflictFollowCursor()
		}
	case "end", "G":
		if n > 0 {
			m.conflict.cursor = n - 1
			m.conflictFollowCursor()
		}
	case "pgup", "ctrl+u":
		m.conflict.scrollY -= max(1, bodyH/2)
		m.conflictClampScroll()
	case "pgdn", "ctrl+d":
		m.conflict.scrollY += max(1, bodyH/2)
		m.conflictClampScroll()
	case "[":
		m.conflictSwitchFile(-1)
	case "]":
		m.conflictSwitchFile(1)
	case "l", "left", "h":
		m.conflictSetChoice(resolutionLeft)
	case "r", "right":
		m.conflictSetChoice(resolutionRight)
	case "b":
		m.conflictSetChoice(resolutionBoth)
	case "u":
		m.conflictSetChoice(resolutionUnset)
	case "enter":
		return m.execConflictApply()
	}
	return m, nil
}

// ── Mouse ───────────────────────────────────────────────────────────────────

// conflictTabBar builds the tab-bar segments and the [x1, x2) pane spans of
// each file tab chip; the same layout backs click hit-testing.
func (m Model) conflictTabBar() ([]seg, [][2]int) {
	c := &m.conflict
	segs := []seg{
		{text: " ⚡ ", fg: colYellow, bold: true, bg: colElement},
		{text: "conflicts ", fg: colText, bg: colElement},
		{text: c.rev, fg: colMagenta, bold: true, bg: colElement},
		{text: "  ", bg: colElement},
	}
	var spans [][2]int
	x := segTextWidth(" ⚡ conflicts ") + segTextWidth(c.rev) + 2
	for i, f := range c.files {
		label := conflictTabLabel(f)
		segs = append(segs, seg{text: " ", bg: colElement})
		x++
		chipBg := lipgloss.TerminalColor(colPanel)
		fg := colTextMuted
		bold := false
		if i == c.cur {
			chipBg = colBorderSubtle
			fg = colText
			bold = true
		}
		segs = append(segs, seg{text: label, fg: fg, bg: chipBg, bold: bold})
		spans = append(spans, [2]int{x, x + segTextWidth(label)})
		x += segTextWidth(label)
	}
	return segs, spans
}

// conflictTabLabel renders one tab chip's text: the basename plus a state
// marker (✓ fully resolved/applied, ✗ unresolvable, or the open-hunk count).
func conflictTabLabel(f conflictFile) string {
	name := f.path
	if i := strings.LastIndexByte(name, '/'); i >= 0 && i+1 < len(name) {
		name = name[i+1:]
	}
	switch {
	case f.done:
		return name + " ✓"
	case !f.resolvable():
		return name + " ✗"
	default:
		if n := f.unresolved(); n > 0 {
			return fmt.Sprintf("%s %d", name, n)
		}
		return name + " ✓"
	}
}

// conflictPaneAtX reports which pane a terminal X falls in: 0 = left,
// 1 = right, -1 = the divider column.
func (m Model) conflictPaneAtX(x int) int {
	scrollW := m.conflictScrollW()
	paneW := max(1, (scrollW-1)/2)
	switch {
	case x >= 0 && x < paneW:
		return 0
	case x >= paneW+1 && x < paneW+1+m.conflictRightPaneW():
		return 1
	default:
		return -1
	}
}

// conflictScrollW returns the width available to content (minus the scrollbar
// when it is shown).
func (m Model) conflictScrollW() int {
	w := m.width
	f := m.curConflictFile()
	if f != nil && f.totalRows() > m.conflictBodyHeight() {
		w -= scrollbarWidth
	}
	return max(1, w)
}

// conflictRightPaneW is the right pane's width (the right pane takes the odd
// remainder column).
func (m Model) conflictRightPaneW() int {
	scrollW := m.conflictScrollW()
	return max(1, scrollW-1-(scrollW-1)/2)
}

// handleConflictClick handles a left press inside the conflict view: a click
// in a conflict-line row picks that pane's side (left pane = side 1, right
// pane = side 2); headers just focus; the tab bar switches files.
func (m Model) handleConflictClick(x, mouseY int) (tea.Model, tea.Cmd) {
	f := m.curConflictFile()
	if f == nil {
		return m, nil
	}

	// Tab bar row.
	if mouseY == contentTopBarHeight {
		_, spans := m.conflictTabBar()
		for i, sp := range spans {
			if x >= sp[0] && x < sp[1] {
				if i != m.conflict.cur {
					m.conflict.cur = i
					m.conflict.scrollY = 0
					m.conflict.cursor = 0
					if f2 := m.curConflictFile(); f2 != nil && len(f2.conflicts) > 0 {
						for ci, bi := range f2.conflicts {
							if f2.blocks[bi].choice == resolutionUnset {
								m.conflict.cursor = ci
								break
							}
						}
						m.conflictFollowCursor()
					}
				}
				return m, nil
			}
		}
		return m, nil
	}

	// Body rows sit below the tab bar and the pane header.
	row := mouseY - contentTopBarHeight - 2 + m.conflict.scrollY
	if row < 0 || row >= f.totalRows() || !f.resolvable() || f.done {
		return m, nil
	}
	cr := f.rows[row]
	if cr.kind != cConflictLine && cr.kind != cConflictHeader {
		return m, nil
	}
	// Focus the block under the click.
	for ci, b := range f.conflicts {
		if b == cr.block {
			m.conflict.cursor = ci
			break
		}
	}
	if cr.kind == cConflictHeader {
		m.conflictFollowCursor()
		return m, nil
	}
	switch m.conflictPaneAtX(x) {
	case 0:
		m.conflictSetChoice(resolutionLeft)
	case 1:
		m.conflictSetChoice(resolutionRight)
	default:
		m.conflictFollowCursor()
	}
	return m, nil
}

// conflictHover updates the hover state for the conflict view.
func (m *Model) conflictHover(x, y int) {
	m.hover.conflictBlock = -1
	m.hover.conflictLeft = false
	f := m.curConflictFile()
	if f == nil || !f.resolvable() || f.done {
		return
	}
	row := y - contentTopBarHeight - 2 + m.conflict.scrollY
	if row < 0 || row >= f.totalRows() {
		return
	}
	cr := f.rows[row]
	if cr.kind != cConflictLine {
		return
	}
	m.hover.conflictBlock = cr.block
	m.hover.conflictLeft = m.conflictPaneAtX(x) == 0
}

// ── Rendering ───────────────────────────────────────────────────────────────

// paneCell renders one pane's content to exactly w cells: the (tab-expanded,
// clipped) text over a background fill.
func paneCell(text string, w int, fg, bg lipgloss.TerminalColor, underline, faint bool, hasContent bool) string {
	text = expandTabs(text)
	var b strings.Builder
	if hasContent {
		styleFor(seg{text: text, fg: fg, bg: bg, underline: underline, faint: faint}).apply(&b, text)
	}
	tw := segTextWidth(text)
	if !hasContent {
		tw = 0
	}
	if tw < w {
		if bg != nil {
			bgStyler(bg).apply(&b, strings.Repeat(" ", w-tw))
		} else {
			b.WriteString(strings.Repeat(" ", w-tw))
		}
		return b.String()
	}
	return clip(b.String(), w)
}

// renderConflictView produces exactly height lines: the tab bar, the pane
// header, then the scrollable side-by-side body with a scrollbar when the
// content overflows.
func (m Model) renderConflictView(width, height int) []string {
	c := &m.conflict
	f := m.curConflictFile()
	bodyH := max(0, height-2)

	total := 0
	if f != nil {
		total = f.totalRows()
	}
	thumbStart, thumbEnd := scrollbarThumb(total, c.scrollY, min(bodyH, total), bodyH)
	hasBar := thumbStart >= 0
	scrollW := width
	if hasBar {
		scrollW -= scrollbarWidth
	}
	paneW := max(1, (scrollW-1)/2)
	rightW := max(1, scrollW-1-paneW)

	var out []string

	// Tab bar: ⚡ conflicts <rev> + one chip per file.
	tabSegs, _ := m.conflictTabBar()
	out = append(out, bgRow(width, colElement, tabSegs...))

	// Pane header.
	leftTitle, rightTitle := " side 1 (l)", " side 2 (r) "
	hdr := paneCell(leftTitle, paneW, colBlue, colPanel, false, false, true) +
		styleFor(seg{text: "│", fg: colBorderSubtle, bg: colPanel}).render("│") +
		paneCell(rightTitle, rightW, colGreen, colPanel, false, false, true)
	out = append(out, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, -1, thumbStart, thumbEnd, hdr))

	switch {
	case f == nil:
		// No files at all (defensive; the view only opens with ≥1 file).
		out = append(out, blankRow(width, colPanel))
	case !f.resolvable():
		for _, line := range []string{
			"",
			" ✗ " + f.note,
			"",
			"   resolve this one manually (edit files, then jj it out)",
			"   [ / ] switch file · esc back",
		} {
			if line == "" {
				out = append(out, blankRow(width, colPanel))
				continue
			}
			var fg lipgloss.TerminalColor = colTextMuted
			if strings.HasPrefix(line, " ✗") {
				fg = colRed
			}
			out = append(out, bgRow(width, colPanel, seg{text: line, fg: fg, bg: colPanel}))
		}
	default:
		focus := m.conflictFocusBlock()
		// Conflict ordinal map: block idx → k/n label.
		kOfN := map[int]int{}
		for i, bi := range f.conflicts {
			kOfN[bi] = i
		}
		end := min(c.scrollY+bodyH, total)
		lineIdx := 0
		for r := c.scrollY; r < end; r++ {
			row := f.rows[r]
			out = append(out, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, lineIdx, thumbStart, thumbEnd,
				m.renderConflictRow(f, row, focus, kOfN, paneW, rightW, scrollW)))
			lineIdx++
		}
	}

	// Pad to the requested height.
	for len(out) < height {
		out = append(out, blankRow(width, colPanel))
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}

// renderConflictRow renders one body row to exactly scrollW cells (without
// the scrollbar, which the caller appends).
func (m Model) renderConflictRow(f *conflictFile, row conflictRow, focusBlock int, kOfN map[int]int, paneW, rightW, scrollW int) string {
	switch row.kind {
	case cRowContext:
		cell := paneCell(row.l, paneW, colTextMuted, colPanel, false, false, true)
		rcell := paneCell(row.r, rightW, colTextMuted, colPanel, false, false, true)
		div := styleFor(seg{text: "│", fg: colBorderSubtle, bg: colPanel}).render("│")
		return cell + div + rcell
	case cRowAuto:
		{
			b := f.blocks[row.block]
			fg := lipgloss.TerminalColor(colGreen) // auto lines from the right side tint green
			if b.autoLeft {
				fg = colBlue
			}
			cell := paneCell(row.l, paneW, fg, colPanel, false, false, true)
			rcell := paneCell(row.r, rightW, fg, colPanel, false, false, true)
			div := styleFor(seg{text: "│", fg: colBorderSubtle, bg: colPanel}).render("│")
			return cell + div + rcell
		}
	case cConflictHeader:
		{
			return m.renderConflictHeadRow(f, row.block, focusBlock, kOfN, scrollW)
		}
	default: // cConflictLine
		b := f.blocks[row.block]
		focused := row.block == focusBlock
		choice := b.choice
		done := f.done

		// Pane backgrounds: focused unresolved blocks get the brighter tint.
		lbg, rbg := confLeftBg, confRightBg
		if focused {
			lbg, rbg = confLeftFocusBg, confRightFocusBg
		}

		// After a pick, the losing pane dims.
		lFaint, rFaint := false, false
		switch choice {
		case resolutionLeft:
			rFaint = true
			rbg = confLoserBg
		case resolutionRight:
			lFaint = true
			lbg = confLoserBg
		}
		if done {
			// Applied: freeze presentation in the "done" palette.
			if choice == resolutionRight {
				lFaint = true
				lbg = confLoserBg
			} else if choice == resolutionLeft {
				rFaint = true
				rbg = confLoserBg
			}
		}

		lUnderline := !done && choice == resolutionUnset && m.hover.conflictBlock == row.block && m.hover.conflictLeft
		rUnderline := !done && choice == resolutionUnset && m.hover.conflictBlock == row.block && !m.hover.conflictLeft && m.hover.conflictBlock != -1

		cell := paneCell(row.l, paneW, colText, lbg, lUnderline, lFaint, row.hasL)
		rcell := paneCell(row.r, rightW, colText, rbg, rUnderline, rFaint, row.hasR)

		divFg := lipgloss.TerminalColor(colBorderSubtle)
		if focused {
			divFg = colYellow
		}
		div := styleFor(seg{text: "│", fg: divFg, bg: colPanel, bold: focused}).render("│")
		if focused {
			div = styleFor(seg{text: "┃", fg: divFg, bg: colPanel, bold: true}).render("┃")
		}
		return cell + div + rcell
	}
}

// renderConflictHeadRow renders the full-width conflict block header to
// exactly scrollW cells.
func (m Model) renderConflictHeadRow(f *conflictFile, blockIdx, focusBlock int, kOfN map[int]int, scrollW int) string {
	b := f.blocks[blockIdx]
	n := len(f.conflicts)
	k := kOfN[blockIdx] + 1

	stateSeg := seg{text: "· unresolved", fg: colRed, bg: colElement}
	if f.done {
		stateSeg = seg{text: "· applied", fg: colGreen, bg: colElement}
	} else {
		switch b.choice {
		case resolutionLeft:
			stateSeg = seg{text: "· → left", fg: colBlue, bg: colElement}
		case resolutionRight:
			stateSeg = seg{text: "· → right", fg: colGreen, bg: colElement}
		case resolutionBoth:
			stateSeg = seg{text: "· → both", fg: colYellow, bg: colElement}
		}
	}

	marker := "   "
	if blockIdx == focusBlock && !f.done {
		marker = " ▶ "
	}
	segs := []seg{
		{text: marker, fg: colYellow, bg: colElement},
		{text: fmt.Sprintf("conflict %d of %d ", k, n), fg: colText, bold: true, bg: colElement},
		stateSeg,
	}
	return bgRow(scrollW, colElement, segs...)
}
