package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// filePhase is the active sub-screen of the file view.
type filePhase int

const (
	filePicker filePhase = iota
	fileBlame
	fileHistory
)

// fileNode is one entry in the file-picker tree.
type fileNode struct {
	name     string
	full     string // full repo-relative path (files only)
	isDir    bool
	children []*fileNode
	expanded bool
}

// treeRow is a flattened, visible node plus its indent depth.
type treeRow struct {
	node  *fileNode
	depth int
}

// fzfResult is one matched file from the inline fuzzy finder.
type fzfResult struct {
	path    string
	score   int
	matched []bool // per-rune: did this position match the query
}

// isWordBoundary reports whether r precedes a word boundary (for fuzzy
// match scoring bonuses).
func isWordBoundary(r rune) bool {
	switch r {
	case '/', '_', '-', '.', ' ', '\t':
		return true
	}
	return false
}

// fuzzyMatch performs case-insensitive subsequence matching of query against
// path. It returns a score (higher is better), a per-rune matched mask for
// highlighting, and ok=false when the query is not a subsequence.
func fuzzyMatch(query, path string) (fzfResult, bool) {
	if query == "" {
		return fzfResult{path: path}, true
	}
	qr := []rune(query)
	sr := []rune(path)
	matched := make([]bool, len(sr))
	qi := 0
	score := 0
	prevMatch := -10
	for si := 0; si < len(sr) && qi < len(qr); si++ {
		if unicode.ToLower(sr[si]) == unicode.ToLower(qr[qi]) {
			matched[si] = true
			if si == prevMatch+1 {
				score += 15 // consecutive match bonus
			} else {
				score += 1
			}
			if si == 0 || isWordBoundary(sr[si-1]) {
				score += 20 // word boundary bonus
			}
			prevMatch = si
			qi++
		}
	}
	if qi < len(qr) {
		return fzfResult{}, false
	}
	score -= len(sr) / 10 // mild preference for shorter paths
	return fzfResult{path: path, score: score, matched: matched}, true
}

// fileViewState holds all state for the file view: the picker tree, the open
// file's blame, and the file's history list.
type fileViewState struct {
	phase filePhase
	err   string

	// picker
	files  []string    // flat tracked-file list (fed to fzf)
	tree   []*fileNode // root nodes
	rows   []treeRow   // flattened visible rows
	cursor int         // index into rows
	offset int

	// inline fuzzy finder (overlay within the picker)
	fzfActive  bool
	fzfQuery   string
	fzfResults []fzfResult
	fzfCursor  int
	fzfOffset  int

	// blame (file open)
	path       string
	lines      []jj.AnnotateLine
	highlights [][]span // per-line syntax-highlighted spans (chroma); nil until computed
	cursorY    int      // absolute line index under the cursor

	// blame cache — built in Update (not View) so it persists across frames.
	// View uses a value receiver, so any render-time mutation is lost. The
	// cache bundles the expensive O(file-size) computations: syntax
	// highlighting, row conversion, and wrapped-line layout. Rebuilt on file
	// load and resize.
	blameRows    []diffRow  // annotateToDiffRows output
	blameDigits  int        // line-number gutter width
	blameLayout  diffLayout // wrapped-line layout for the body
	blameCacheW  int        // width used to build the cache (0 = dirty)
	blameCacheCH int        // contentH used to build the cache

	// history
	hist    []jj.LogEntry
	histCur int
	histOff int
}

// newFileViewState builds the picker tree from a flat file list.
func newFileViewState(files []string) fileViewState {
	fv := fileViewState{phase: filePicker, files: files}
	fv.tree = buildFileTree(files)
	fv.reflow()
	// Expand the top-level so the user lands on content immediately.
	for _, n := range fv.tree {
		if n.isDir {
			n.expanded = true
		}
	}
	fv.reflow()
	return fv
}

// buildFileTree turns a flat list of repo-relative paths into a nested tree,
// directories first then files, each group sorted alphabetically.
func buildFileTree(files []string) []*fileNode {
	root := &fileNode{isDir: true}
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		parts := strings.Split(f, "/")
		cur := root
		for i, p := range parts {
			isLast := i == len(parts)-1
			child := findChild(cur, p)
			if child == nil {
				child = &fileNode{name: p, isDir: !isLast}
				if isLast {
					child.full = f
				}
				cur.children = append(cur.children, child)
			}
			cur = child
		}
	}
	sortNodes(root)
	return root.children
}

func findChild(parent *fileNode, name string) *fileNode {
	for _, c := range parent.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func sortNodes(n *fileNode) {
	sort.SliceStable(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.isDir != b.isDir {
			return a.isDir // directories first
		}
		return a.name < b.name
	})
	for _, c := range n.children {
		if c.isDir {
			sortNodes(c)
		}
	}
}

// reflow rebuilds the flattened visible-rows slice (respecting expanded
// directories) and keeps the cursor in range.
func (fv *fileViewState) reflow() {
	var rows []treeRow
	var walk func(nodes []*fileNode, depth int)
	walk = func(nodes []*fileNode, depth int) {
		for _, n := range nodes {
			rows = append(rows, treeRow{node: n, depth: depth})
			if n.isDir && n.expanded {
				walk(n.children, depth+1)
			}
		}
	}
	walk(fv.tree, 0)
	fv.rows = rows
	if fv.cursor >= len(fv.rows) {
		fv.cursor = len(fv.rows) - 1
	}
	if fv.cursor < 0 {
		fv.cursor = 0
	}
}

// ensureHighlights lazily syntax-highlights the open file's source lines
// via chroma, caching the per-line spans. Falls back to nil (plain text)
// when no lexer matches the file. Idempotent across renders.
func (fv *fileViewState) ensureHighlights() {
	if fv.highlights != nil {
		return
	}
	if fv.path == "" || len(fv.lines) == 0 {
		fv.highlights = [][]span{}
		return
	}
	texts := make([]string, len(fv.lines))
	for i, l := range fv.lines {
		texts[i] = l.Text
	}
	fv.highlights = highlightLines(fv.path, texts)
	if fv.highlights == nil {
		fv.highlights = [][]span{} // sentinel: tried, no lexer
	}
}

// buildBlameCache pre-computes the expensive O(file-size) data that
// renderFileBlame needs every frame: syntax highlighting, diffRow
// conversion, and the wrapped-line layout. Must be called from Update (not
// View) so the cache persists across frames — View's value receiver would
// discard any render-time mutation. Rebuild when width or contentH changes.
func (fv *fileViewState) buildBlameCache(width, contentH int) {
	fv.ensureHighlights()
	fv.blameRows = annotateToDiffRows(fv.lines, fv.highlights)
	fv.blameDigits = lineDigits(len(fv.lines))
	fv.blameLayout = computeDiffLayoutPure(width, contentH, 0, fv.blameRows, "", fv.blameDigits, nil, false, true)
	fv.blameCacheW = width
	fv.blameCacheCH = contentH
}

// blameCacheValid reports whether the blame cache matches the given dimensions.
func (fv *fileViewState) blameCacheValid(width, contentH int) bool {
	return fv.blameCacheW == width && fv.blameCacheCH == contentH && fv.blameRows != nil
}

// curRow returns the row under the picker cursor, or nil.
func (fv *fileViewState) curRow() *treeRow {
	if fv.cursor < 0 || fv.cursor >= len(fv.rows) {
		return nil
	}
	return &fv.rows[fv.cursor]
}

// pickerVisibleRange computes the [start, end) row window for the cursor.
func (fv *fileViewState) pickerVisibleRange(height int) (int, int) {
	total := len(fv.rows)
	off := fv.offset
	if fv.cursor < off {
		off = fv.cursor
	}
	end := off
	used := 0
	for end < total && used < height {
		used++
		end++
	}
	if fv.cursor >= end {
		off = fv.cursor - height + 1
		if off < 0 {
			off = 0
		}
		end = fv.cursor + 1
	}
	fv.offset = off
	return off, end
}

// fzfFilter re-computes the fuzzy match results for the current query,
// sorted by score (descending) then alphabetically. The cursor is clamped
// to the new result set.
func (fv *fileViewState) fzfFilter() {
	var results []fzfResult
	for _, f := range fv.files {
		if r, ok := fuzzyMatch(fv.fzfQuery, f); ok {
			results = append(results, r)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].path < results[j].path
	})
	fv.fzfResults = results
	if fv.fzfCursor >= len(results) {
		fv.fzfCursor = max(0, len(results)-1)
	}
}

// fzfVisibleRange computes the [start, end) row window for the fzf cursor,
// updating fzfOffset for scroll tracking.
func (fv *fileViewState) fzfVisibleRange(height int) (int, int) {
	total := len(fv.fzfResults)
	off := fv.fzfOffset
	if fv.fzfCursor < off {
		off = fv.fzfCursor
	}
	end := off
	used := 0
	for end < total && used < height {
		used++
		end++
	}
	if fv.fzfCursor >= end {
		off = max(0, fv.fzfCursor-height+1)
		end = fv.fzfCursor + 1
	}
	fv.fzfOffset = off
	return off, end
}

// ── Commands ────────────────────────────────────────────────────────────────

func (m Model) loadFileListCmd() tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		files, err := r.FileList()
		return fileListMsg{files: files, err: err}
	}
}

func (m Model) loadAnnotateCmd(path string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		lines, err := r.FileAnnotate(path, "")
		return fileAnnotateMsg{path: path, lines: lines, err: err}
	}
}

func (m Model) loadFileHistoryCmd(path string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		entries, err := r.FileLog(path, "all()", 0)
		return fileHistoryMsg{entries: entries, err: err}
	}
}

// ── Rendering ───────────────────────────────────────────────────────────────

// renderFileView dispatches to the active phase's renderer.
func (m Model) renderFileView(width, height int) []string {
	switch m.fileView.phase {
	case fileBlame:
		return m.renderFileBlame(width, height)
	case fileHistory:
		return m.renderFileHistory(width, height)
	default:
		return m.renderFilePicker(width, height)
	}
}

func (m Model) renderFilePicker(width, height int) []string {
	fv := &m.fileView
	if fv.fzfActive {
		return m.renderFzf(width, height)
	}
	titleLeft := " file browser"
	titleRight := " f: fzf · ⏎ open · l/→ expand · h/← collapse · esc/q leave "
	pad := max(1, width-len(titleLeft)-len(titleRight))
	out := []string{bgRow(width, colDarkPurple,
		seg{text: titleLeft, fg: colPurple, bg: colDarkPurple},
		seg{text: strings.Repeat(" ", pad), bg: colDarkPurple},
		seg{text: titleRight, fg: colGray, bg: colDarkPurple},
	)}

	contentH := height - 1
	if contentH < 0 {
		contentH = 0
	}

	if fv.err != "" {
		return padLines(append(out, bgRow(width, colPanel, seg{text: " ✖ " + fv.err, fg: colRed})), height, width)
	}
	if len(fv.rows) == 0 {
		return padLines(append(out, bgRow(width, colPanel, seg{text: "  (no tracked files)", fg: colGray})), height, width)
	}

	start, end := fv.pickerVisibleRange(contentH)
	var content []string
	for i := start; i < end; i++ {
		row := fv.rows[i]
		selected := i == fv.cursor
		hovered := m.hover.pickerRow == i && !selected
		content = append(content, renderTreeRowString(width, row, selected, hovered))
	}
	content = padLines(content, contentH, width)
	out = append(out, content...)
	return padLines(out, height, width)
}

// renderFzf renders the inline fuzzy finder as an overlay within the file
// picker content area. A prompt bar with the live query and match count sits
// above a divider and the filtered result list. Matched characters are
// highlighted in yellow; the cursor row uses colElement with a yellow ┃ bar,
// matching the diff panel's cursor style.
func (m Model) renderFzf(width, height int) []string {
	fv := &m.fileView

	// Title bar.
	titleLeft := " file browser"
	titleRight := " esc back · ⏎ open · type to filter "
	pad := max(1, width-len(titleLeft)-len(titleRight))
	out := []string{bgRow(width, colDarkPurple,
		seg{text: titleLeft, fg: colPurple, bg: colDarkPurple},
		seg{text: strings.Repeat(" ", pad), bg: colDarkPurple},
		seg{text: titleRight, fg: colGray, bg: colDarkPurple},
	)}

	// Prompt bar — ┃ fzf  query█  …  N matches
	queryStr := fv.fzfQuery + "█"
	var matchStr string
	if len(fv.fzfResults) == 0 {
		matchStr = "no matches"
	} else {
		matchStr = fmt.Sprintf("%d matches", len(fv.fzfResults))
	}
	leftW := lipgloss.Width("┃ fzf  ") + lipgloss.Width(queryStr)
	rightW := lipgloss.Width(matchStr) + 1
	promptPad := max(0, width-leftW-rightW)
	prompt := bgRow(width, colPanel,
		seg{text: "┃ ", fg: colCyan, bold: true, bg: colPanel},
		seg{text: "fzf  ", fg: colTextMuted, bg: colPanel},
		seg{text: queryStr, fg: colYellow, bold: true, bg: colPanel},
		seg{text: strings.Repeat(" ", promptPad), bg: colPanel},
		seg{text: matchStr, fg: colTextMuted, bg: colPanel},
		seg{text: " ", bg: colPanel},
	)
	out = append(out, prompt)

	// Divider.
	out = append(out, bgRow(width, colPanel, seg{text: strings.Repeat("─", width), fg: colBorder, bg: colPanel}))

	// Results.
	contentH := height - 3 // title + prompt + divider
	if contentH < 0 {
		contentH = 0
	}

	if len(fv.fzfResults) == 0 {
		out = append(out, bgRow(width, colPanel, seg{text: "  (no matches)", fg: colTextMuted, bg: colPanel}))
		return padLines(out, height, width)
	}

	start, end := fv.fzfVisibleRange(contentH)
	for i := start; i < end; i++ {
		r := fv.fzfResults[i]
		selected := i == fv.fzfCursor
		hovered := m.hover.fzfRow == i && !selected
		bg := colPanel
		if selected {
			bg = colElement
		} else if hovered {
			bg = colHover
		}
		barFg := bg
		if selected {
			barFg = colYellow
		}

		segs := []seg{
			{text: "┃", fg: barFg, bold: true, bg: bg},
			{text: " ", bg: bg},
		}

		// Render path with matched chars highlighted, grouping consecutive
		// matched / non-matched runs into single segments.
		sr := []rune(r.path)
		var buf strings.Builder
		bufMatched := false
		for si, ch := range sr {
			isMatch := r.matched != nil && si < len(r.matched) && r.matched[si]
			if si == 0 {
				bufMatched = isMatch
			}
			if isMatch != bufMatched {
				if buf.Len() > 0 {
					fg := colText
					if bufMatched {
						fg = colYellow
					}
					segs = append(segs, seg{text: buf.String(), fg: fg, bold: bufMatched, bg: bg})
					buf.Reset()
				}
				bufMatched = isMatch
			}
			buf.WriteRune(ch)
		}
		if buf.Len() > 0 {
			fg := colText
			if bufMatched {
				fg = colYellow
			}
			segs = append(segs, seg{text: buf.String(), fg: fg, bold: bufMatched, bg: bg})
		}

		out = append(out, bgRow(width, bg, segs...))
	}

	return padLines(out, height, width)
}

// renderTreeRowString renders a single file-picker tree row. The layout
// mirrors the diff panel: a left-edge ┃ cursor bar (bright yellow on the
// selected row, invisible otherwise), the ▼/▶ expand/collapse arrow for
// directories (matching the diff view's file headers), and the name. Every
// row is filled with colPanel (or colElement when selected, colHover when
// hovered) so no transparent gaps show through.
func renderTreeRowString(width int, row treeRow, selected, hovered bool) string {
	n := row.node
	indent := strings.Repeat("  ", row.depth)

	var arrow, name string
	if n.isDir {
		if n.expanded {
			arrow = "▼"
		} else {
			arrow = "▶"
		}
		name = n.name + "/"
	} else {
		arrow = " "
		name = n.name
	}

	bg := colPanel
	if selected {
		bg = colElement
	} else if hovered {
		bg = colHover
	}

	// Cursor bar: yellow on the selected row, bg-coloured (invisible) else.
	barFg := bg
	if selected {
		barFg = colYellow
	}

	var nameFg lipgloss.TerminalColor
	switch {
	case n.isDir:
		nameFg = colBlue
	case selected:
		nameFg = colYellow
	default:
		nameFg = colText
	}

	arrowFg := colTextMuted
	if selected {
		arrowFg = colYellow
	}

	segs := []seg{
		{text: "┃", fg: barFg, bold: true, bg: bg},
		{text: " " + indent, bg: bg},
		{text: arrow + " ", fg: arrowFg, bg: bg},
		{text: name, fg: nameFg, bg: bg, bold: n.isDir},
	}
	return bgRow(width, bg, segs...)
}

// annotateToDiffRows converts annotated file lines into diffRow format for
// rendering via renderDiffPanel in file viewer mode. All lines become context
// lines (no +/- signs, no diff backgrounds) with a single line number in
// newNum. Tabs are expanded to 4 spaces to match the previous blame renderer.
// Section backgrounds alternate per ChangeID run so contiguous blame hunks
// are visually distinguishable.
func annotateToDiffRows(lines []jj.AnnotateLine, highlights [][]span) []diffRow {
	if len(lines) == 0 {
		return nil
	}
	rows := make([]diffRow, len(lines))
	parity := 0
	for i, l := range lines {
		if i > 0 && l.ChangeID != lines[i-1].ChangeID {
			parity ^= 1
		}
		var sp []span
		if highlights != nil && i < len(highlights) && len(highlights[i]) > 0 {
			for _, s := range highlights[i] {
				sp = append(sp, span{text: strings.ReplaceAll(s.text, "\t", "    "), fg: s.fg})
			}
		} else if l.Text != "" {
			sp = []span{{text: strings.ReplaceAll(l.Text, "\t", "    ")}}
		} else {
			sp = []span{}
		}
		bg := fileSectionBg[parity]
		rows[i] = diffRow{
			kind:          rowLine,
			lineKind:      "context",
			newNum:        l.LineNo,
			spans:         sp,
			sectionBg:     bg,
			sectionParity: parity,
		}
	}
	return rows
}

// buildBlameHead renders the blame header — the commit that last touched the
// cursor's current section — in the same labelled style as the diff panel's
// description/status headers. It scrolls with the content as a head section.
func buildBlameHead(width int, cid, author, desc string) []string {
	head := []string{bgRow(width, colPanel, seg{text: "┃ ", fg: colCyan, bold: true, bg: colPanel}, seg{text: "blame", fg: colTextMuted, bg: colPanel})}

	var infoSegs []seg
	infoSegs = append(infoSegs, seg{text: "  ", bg: colPanel})
	if cid != "" {
		infoSegs = append(infoSegs, seg{text: cid, fg: colPurple, bold: true, bg: colPanel})
	}
	if author != "" {
		infoSegs = append(infoSegs, seg{text: " ", bg: colPanel})
		infoSegs = append(infoSegs, seg{text: author, fg: colBlue, bg: colPanel})
	}
	if desc != "" {
		infoSegs = append(infoSegs, seg{text: " — " + desc, fg: colGray, bg: colPanel})
	}
	head = append(head, bgRow(width, colPanel, infoSegs...))
	head = append(head, bgRow(width, colPanel, seg{text: strings.Repeat("─", width), fg: colBorder, bg: colPanel}))
	return head
}

func (m Model) renderFileBlame(width, height int) []string {
	fv := &m.fileView

	// Early-return cases (error, empty) use the same panel-style title bar
	// as the diff panel's file mode, but render a simple message instead of
	// delegating to renderDiffPanel.
	titleSegs := []seg{
		{text: " ", fg: colText, bg: colPanel},
		{text: fv.path, fg: colText, bold: true, bg: colPanel},
		{text: "  (esc/q to back) ", fg: colTextMuted, bg: colPanel},
	}
	title := bgRow(width, colPanel, titleSegs...)

	if fv.err != "" {
		return padLines([]string{title, bgRow(width, colPanel, seg{text: " ✖ " + fv.err, fg: colRed})}, height, width)
	}
	if len(fv.lines) == 0 {
		return padLines([]string{title, bgRow(width, colPanel, seg{text: "  (empty file)", fg: colGray})}, height, width)
	}

	// Build the blame head from the cursor's current line. This is rendered
	// as sticky chrome (always visible) by renderDiffPanel, not as part of
	// the scrollable body.
	cursorY := min(fv.cursorY, len(fv.lines)-1)
	cur := fv.lines[cursorY]
	cid := cur.ChangeID
	if len(cid) > 8 {
		cid = cid[:8]
	}
	desc := strings.TrimSpace(cur.Description)
	head := buildBlameHead(width, cid, cur.Author, desc)
	headLen := len(head)

	contentH := height - 1 - headLen // minus title bar and sticky blame header
	if contentH < 1 {
		contentH = 1
	}

	// Use the pre-computed cache (built in Update) for the expensive
	// O(file-size) data: syntax highlighting, row conversion, and
	// wrapped-line layout. Fall back to inline computation only when the
	// cache is stale (e.g., tests that bypass Update).
	rows := fv.blameRows
	digits := fv.blameDigits
	layout := fv.blameLayout
	if !fv.blameCacheValid(width, contentH) {
		fv.ensureHighlights()
		rows = annotateToDiffRows(fv.lines, fv.highlights)
		digits = lineDigits(len(fv.lines))
		layout = computeDiffLayoutPure(width, contentH, 0, rows, "", digits, nil, false, true)
	}

	// Compute the set of row indices that belong to the cursor's current
	// section (contiguous run of the same ChangeID) so the ┃ bar can
	// highlight the entire hunk. Scan outward from the cursor rather than
	// iterating the entire file.
	sectionRows := computeSectionRows(fv.lines, cursorY)

	// Map the cursor (source line) to a terminal body line (body-relative;
	// the head is not part of the body).
	cursorBodyRow := -1
	if cursorY >= 0 && cursorY < len(layout.starts) {
		cursorBodyRow = layout.starts[cursorY]
	}

	// Center the cursor: aim to place it at the vertical middle of the
	// viewport. Near the top and bottom of the file the scroll clamps so we
	// don't scroll past the boundaries — the cursor drifts from center only
	// at the extremes.
	half := contentH / 2
	termScrollY := 0
	if cursorBodyRow >= 0 {
		termScrollY = cursorBodyRow - half
	}

	maxScroll := max(0, layout.total-contentH)
	if termScrollY < 0 {
		termScrollY = 0
	}
	if termScrollY > maxScroll {
		termScrollY = maxScroll
	}

	return renderDiffPanel(width, height, fv.path, 0, false, false, 0, "", false, rows, digits, nil, "", termScrollY, cursorBodyRow, sectionRows, nil, splitView{}, true, head, &layout, m.hover.blameLine)
}

func lineDigits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

// fileViewContentH is the available terminal lines for the file view's
// scrollable body: content height minus the title bar (1) and the sticky
// blame header (3: label, info, divider).
func fileViewContentH(m Model) int {
	ch := m.contentHeight() - 4
	if ch < 1 {
		ch = 1
	}
	return ch
}

// computeSectionRows returns the set of line indices that belong to the same
// blame section (contiguous run of the same ChangeID) as the cursor line.
// Scans outward from cursorY instead of iterating the entire file, so cost
// is proportional to the section size, not the file size.
func computeSectionRows(lines []jj.AnnotateLine, cursorY int) map[int]bool {
	if cursorY < 0 || cursorY >= len(lines) {
		return nil
	}
	target := lines[cursorY].ChangeID
	rows := map[int]bool{}
	for i := cursorY; i >= 0 && lines[i].ChangeID == target; i-- {
		rows[i] = true
	}
	for i := cursorY + 1; i < len(lines) && lines[i].ChangeID == target; i++ {
		rows[i] = true
	}
	return rows
}

func (m Model) renderFileHistory(width, height int) []string {
	fv := &m.fileView
	titleLeft := " history: " + fv.path + "  (all())"
	titleRight := " ⏎ open commit · esc back "
	pad := max(1, width-len(titleLeft)-len(titleRight))
	title := bgRow(width, colDarkPurple,
		seg{text: titleLeft, fg: colWhite, bg: colDarkPurple},
		seg{text: strings.Repeat(" ", pad), bg: colDarkPurple},
		seg{text: titleRight, fg: colGray, bg: colDarkPurple},
	)

	if fv.err != "" {
		return padLines([]string{title, bgRow(width, colPanel, seg{text: " ✖ " + fv.err, fg: colRed})}, height, width)
	}
	if len(fv.hist) == 0 {
		return padLines([]string{title, bgRow(width, colPanel, seg{text: "  (no commits touched this file)", fg: colGray})}, height, width)
	}

	body := renderLog(width, height-1, fv.hist, fv.histCur, fv.histOff, -1, nil, 0, rebaseView{}, squashView{}, bookmarkDragView{}, m.hover.histIdx, -1)
	return padLines(append([]string{title}, body...), height, width)
}
