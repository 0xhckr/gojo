package ui

import (
	"bytes"
	"os/exec"
	"sort"
	"strings"

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

	// blame (file open)
	path    string
	lines   []jj.AnnotateLine
	cursorY int // absolute line index under the cursor
	scrollY int // top visible line index

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

// blameVisibleRange keeps the cursor inside the viewport, scrolling minimally.
func (fv *fileViewState) blameVisibleRange(height int) (int, int) {
	total := len(fv.lines)
	if height <= 0 {
		return 0, 0
	}
	if fv.cursorY < fv.scrollY {
		fv.scrollY = fv.cursorY
	}
	if fv.cursorY >= fv.scrollY+height {
		fv.scrollY = fv.cursorY - height + 1
	}
	if fv.scrollY < 0 {
		fv.scrollY = 0
	}
	if fv.scrollY > total-height && total >= height {
		fv.scrollY = total - height
	}
	if fv.scrollY < 0 {
		fv.scrollY = 0
	}
	end := fv.scrollY + height
	if end > total {
		end = total
	}
	return fv.scrollY, end
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

// fzfPickCmd suspends the TUI and runs fzf over the tracked-file list. fzf
// renders its finder to stderr (the terminal) and prints the selection to
// stdout, which we capture by pre-setting cmd.Stdout — tea.ExecProcess only
// overrides stdout when it's nil, so our buffer survives.
func (m Model) fzfPickCmd(initial string) tea.Cmd {
	files := m.fileView.files
	if len(files) == 0 {
		return nil
	}
	var buf bytes.Buffer
	args := []string{
		"--prompt", "gojo file> ",
		"--ansi",
		"--delimiter", "/",
		"--preview", "jj file show -r @ {} 2>/dev/null | head -80",
		"--preview-window", "right:50%:wrap:hidden",
		"--height", "60%",
		"--layout", "reverse",
		"--info", "inline",
	}
	if initial != "" {
		args = append(args, "--query", initial)
	}
	c := exec.Command("fzf", args...)
	c.Stdin = strings.NewReader(strings.Join(files, "\n"))
	c.Stdout = &buf
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return fzfPickedMsg{path: strings.TrimSpace(buf.String()), err: err}
	})
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
	titleLeft := " file browser"
	titleRight := " f: fzf · ⏎ open · l/→ expand · h/← collapse · esc leave "
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
		return padLines(append(out, plainRow(width, seg{text: " ✖ " + fv.err, fg: colRed})), height)
	}
	if len(fv.rows) == 0 {
		return padLines(append(out, plainRow(width, seg{text: "  (no tracked files)", fg: colGray})), height)
	}

	start, end := fv.pickerVisibleRange(contentH)
	var content []string
	for i := start; i < end; i++ {
		row := fv.rows[i]
		content = append(content, renderTreeRowString(width, row, i == fv.cursor))
	}
	content = padLines(content, contentH)
	out = append(out, content...)
	return padLines(out, height)
}

func renderTreeRowString(width int, row treeRow, selected bool) string {
	n := row.node
	indent := strings.Repeat("  ", row.depth)
	var marker, name string
	if n.isDir {
		if n.expanded {
			marker = "▾"
		} else {
			marker = "▸"
		}
		name = n.name + "/"
	} else {
		marker = " "
		name = n.name
	}

	var bg lipgloss.TerminalColor
	var nameFg lipgloss.TerminalColor
	if selected {
		bg = colDarkPurple
	}
	switch {
	case n.isDir:
		nameFg = colBlue
	case selected:
		nameFg = colYellow
	default:
		nameFg = colWhite
	}

	segs := []seg{{text: " " + indent, fg: colDarkGray, bg: bg}}
	markerFg := colGray
	if selected {
		markerFg = colYellow
	}
	segs = append(segs, seg{text: marker + " ", fg: markerFg, bg: bg})
	segs = append(segs, seg{text: name, fg: nameFg, bg: bg, bold: n.isDir})
	return renderRow(width, bg, segs)
}

func (m Model) renderFileBlame(width, height int) []string {
	fv := &m.fileView
	titleLeft := " " + fv.path + "  (blame)"
	titleRight := " h: history · ⏎ open commit · esc leave "
	pad := max(1, width-len(titleLeft)-len(titleRight))
	out := []string{bgRow(width, colDarkPurple,
		seg{text: titleLeft, fg: colWhite, bg: colDarkPurple},
		seg{text: strings.Repeat(" ", pad), bg: colDarkPurple},
		seg{text: titleRight, fg: colGray, bg: colDarkPurple},
	)}

	contentH := height - 1
	if contentH < 0 {
		contentH = 0
	}

	if fv.err != "" {
		return padLines(append(out, plainRow(width, seg{text: " ✖ " + fv.err, fg: colRed})), height)
	}
	if len(fv.lines) == 0 {
		return padLines(append(out, plainRow(width, seg{text: "  (empty file)", fg: colGray})), height)
	}

	// Line-number gutter width.
	digits := lineDigits(len(fv.lines))
	// Blame column: change id (8) + space + author, capped.
	blameW := min(30, width/3)
	if blameW < 12 {
		blameW = 12
	}

	start, end := fv.blameVisibleRange(contentH)
	var content []string
	prevChange := ""
	for i := start; i < end; i++ {
		l := fv.lines[i]
		isCursor := i == fv.cursorY
		showBlame := l.ChangeID != prevChange
		prevChange = l.ChangeID

		content = append(content, renderBlameLine(width, digits, blameW, l, showBlame, isCursor))
	}
	content = padLines(content, contentH)
	out = append(out, content...)
	return padLines(out, height)
}

func renderBlameLine(width, digits, blameW int, l jj.AnnotateLine, showBlame, isCursor bool) string {
	var bg lipgloss.TerminalColor
	if isCursor {
		bg = colDarkPurple
	}

	// blame cell: change_id padded to 8, then truncated author.
	cid := l.ChangeID
	if len(cid) > 8 {
		cid = cid[:8]
	}
	author := l.Author
	at := strings.IndexByte(author, '@')
	if at > 0 {
		author = author[:at] // drop the domain for brevity
	}

	var cidFg lipgloss.TerminalColor = colPurple
	authorFg := colBlue
	if !showBlame {
		cidFg = nil
		authorFg = nil
		author = ""
		cid = ""
	}

	num := padNum(l.LineNo, digits)

	// The blame cell is a fixed width so the source text stays aligned
	// whether or not blame is shown on this line: leading space + cid
	// (padded to 8) + space + author (padded to authorW).
	authorW := blameW - 9
	if authorW < 1 {
		authorW = 1
	}
	cidCell := cid + strings.Repeat(" ", 8-len([]rune(cid)))
	authorCell := author + strings.Repeat(" ", authorW-len([]rune(author)))

	segs := []seg{{text: " ", bg: bg}}
	segs = append(segs, seg{text: cidCell, fg: cidFg, bg: bg, bold: showBlame})
	segs = append(segs, seg{text: " ", bg: bg})
	segs = append(segs, seg{text: authorCell, fg: authorFg, bg: bg})
	segs = append(segs, seg{text: "│ ", fg: colDarkGray, bg: bg})
	segs = append(segs, seg{text: num + " ", fg: colGray, bg: bg})
	text := strings.ReplaceAll(l.Text, "\t", "    ")
	segs = append(segs, seg{text: text, fg: colWhite, bg: bg})
	return renderRow(width, bg, segs)
}

func lineDigits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
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
		return padLines([]string{title, plainRow(width, seg{text: " ✖ " + fv.err, fg: colRed})}, height)
	}
	if len(fv.hist) == 0 {
		return padLines([]string{title, plainRow(width, seg{text: "  (no commits touched this file)", fg: colGray})}, height)
	}

	body := renderLog(width, height-1, fv.hist, fv.histCur, fv.histOff, nil, 0, rebaseView{}, squashView{})
	return padLines(append([]string{title}, body...), height)
}
