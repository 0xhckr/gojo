package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// Click behaviour across views: a left-click moves the cursor (or the
// rebase/squash destination) to the row under the mouse. Clicking the row
// that is already focused activates it — opening a diff, expanding a
// directory, or opening a file — mirroring what enter does on the keyboard.
// In the diff panel a click moves the chunk cursor; on a file header it also
// toggles collapse, and in split mode it toggles the mark instead.

// modalInputActive reports whether a text-input/menu mode is capturing all
// input. Clicks are ignored in those states (the wheel still scrolls).
func (m Model) modalInputActive() bool {
	return m.pendingElev != nil || m.bookmarkMode || m.tagMode || m.gitMode
}

// handleClick dispatches a left-press at the given terminal Y (0-based) to
// the active view. The caller has already verified the X is outside the
// scrollbar area.
func (m Model) handleClick(mouseY int) (tea.Model, tea.Cmd) {
	switch {
	case m.diffOpen:
		return m.handleDiffClick(mouseY)
	case m.view == viewFile:
		switch m.fileView.phase {
		case fileBlame:
			return m.handleBlameClick(mouseY)
		case fileHistory:
			return m.handleHistoryClick(mouseY)
		default:
			return m.handlePickerClick(mouseY)
		}
	case m.view == viewHelp:
		return m, nil
	default:
		return m.handleLogClick(mouseY)
	}
}

// logEntryAtContentY maps a Y coordinate within a rendered log list to the
// index of the entry under it. contentY is 0-based from the top of the list's
// own area (row 0 is the list's padding row); height is that area's height.
// focus/offset drive the same variable-height windowing renderLog uses.
// Returns ok=false when the position is on padding or past the last row.
func logEntryAtContentY(entries []jj.LogEntry, focus, offset, contentY, height int) (int, bool) {
	if len(entries) == 0 {
		return 0, false
	}
	lineIdx := contentY - 1 // skip the list's top padding row
	if lineIdx < 0 {
		return 0, false
	}
	off, end := logWindow(entries, focus, offset, height-1)
	acc := 0
	for i := off; i < end; i++ {
		h := commitLines(entries[i])
		if lineIdx < acc+h {
			return i, true
		}
		acc += h
	}
	return 0, false
}

// handleLogClick selects the commit under the mouse. In rebase/squash mode it
// moves the destination indicator instead. Re-clicking the selected commit
// opens its diff (same as enter).
func (m Model) handleLogClick(mouseY int) (tea.Model, tea.Cmd) {
	focus := m.cursor
	if m.rebaseMode {
		focus = m.rebaseDest
	}
	if m.squashMode {
		focus = m.squashDest
	}
	idx, ok := logEntryAtContentY(m.entries, focus, m.offset, mouseY-contentTopBarHeight, m.contentHeight())
	if !ok {
		return m, nil
	}
	if m.rebaseMode {
		m.rebaseDest = idx
		m.recomputeOffset()
		return m, nil
	}
	if m.squashMode {
		m.squashDest = idx
		m.recomputeOffset()
		return m, nil
	}
	if idx == m.cursor {
		if e := m.selectedEntry(); e != nil {
			return m.openRevisionDiff(e.ChangeID, e.CommitID, e.ChangeIDPrefixLen, e.Subject)
		}
		return m, nil
	}
	m.cursor = idx
	m.recomputeOffset()
	return m, nil
}

// handleDiffClick moves the chunk cursor to the diff row under the mouse.
// File headers additionally toggle collapse; in split mode the click toggles
// the mark (like space) instead of collapsing.
func (m Model) handleDiffClick(mouseY int) (tea.Model, tea.Cmd) {
	rowIdx, ok := m.diffRowAtMouseY(mouseY)
	if !ok {
		return m, nil
	}
	// Only navigable rows (file headers, addition/deletion lines) take the
	// cursor; clicks on context lines or hunk headers are ignored.
	if !m.setDiffCursorToRow(rowIdx) {
		return m, nil
	}
	if m.splitMode {
		m.splitToggle()
		return m, nil
	}
	if m.diffRows[rowIdx].kind == rowFileHeader {
		m.toggleDiffCollapse(rowIdx)
	}
	return m, nil
}

// diffRowAtMouseY maps a terminal Y coordinate to a diff row index (0-based
// into diffRows). Returns ok=false when the click lands on the head section,
// outside the body, or on a raw list view (which has no diffRows).
func (m Model) diffRowAtMouseY(mouseY int) (int, bool) {
	bodyStartY := contentTopBarHeight + 1 // top bar + diff title bar
	bodyLine := mouseY - bodyStartY + m.diffScrollY
	headLen := m.diffHeadLen()
	if bodyLine < headLen {
		return 0, false
	}
	diffBodyLine := bodyLine - headLen
	if diffBodyLine < 0 || diffBodyLine >= m.diffBodyTotal() {
		return 0, false
	}
	if len(m.diffLayout.starts) == 0 {
		return 0, false
	}
	rowIdx, _ := m.diffLayout.rowAt(diffBodyLine)
	if rowIdx < 0 || rowIdx >= len(m.diffRows) {
		return 0, false
	}
	return rowIdx, true
}

// setDiffCursorToRow moves the chunk cursor to the navigable chunk containing
// diff row rowIdx (a 0-based index into diffRows). Returns false when the row
// is not part of any chunk (context lines, hunk headers, …).
func (m *Model) setDiffCursorToRow(rowIdx int) bool {
	target := m.diffHeadLen() + rowIdx
	for i, chunk := range m.diffChunks {
		for j, r := range chunk {
			if r == target {
				m.diffCurChunk = i
				m.diffCurLine = j
				return true
			}
		}
	}
	return false
}

// handlePickerClick moves the file-picker cursor to the row under the mouse.
// Re-clicking the selected row activates it: directories toggle expansion,
// files open in the blame view. With the fuzzy finder active, clicks select
// among the filtered results and re-clicking opens the file.
func (m Model) handlePickerClick(mouseY int) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	rowIdx := mouseY - contentTopBarHeight - 1 // picker title bar
	if rowIdx < 0 {
		return m, nil
	}
	if fv.fzfActive {
		// Below the title: prompt row, divider row, then results.
		resIdx := rowIdx - 2
		if resIdx < 0 {
			return m, nil
		}
		contentH := m.contentHeight() - 3
		if contentH < 0 {
			contentH = 0
		}
		start, end := fv.fzfVisibleRange(contentH)
		i := start + resIdx
		if i >= end || i >= len(fv.fzfResults) {
			return m, nil
		}
		if i == fv.fzfCursor {
			path := fv.fzfResults[i].path
			fv.fzfActive = false
			m, tick := m.startBusy("annotating " + path + "…")
			return m, tea.Batch(tick, m.loadAnnotateCmd(path))
		}
		fv.fzfCursor = i
		return m, nil
	}
	contentH := m.contentHeight() - 1
	if contentH < 0 {
		contentH = 0
	}
	start, end := fv.pickerVisibleRange(contentH)
	i := start + rowIdx
	if i >= end || i >= len(fv.rows) {
		return m, nil
	}
	if i == fv.cursor {
		if row := fv.curRow(); row != nil {
			if row.node.isDir {
				row.node.expanded = !row.node.expanded
				fv.reflow()
				return m, nil
			}
			path := row.node.full
			m, tick := m.startBusy("annotating " + path + "…")
			return m, tea.Batch(tick, m.loadAnnotateCmd(path))
		}
		return m, nil
	}
	fv.cursor = i
	return m, nil
}

// handleBlameClick moves the blame cursor to the source line under the mouse.
// Re-clicking the focused line opens the commit that last touched it (same
// as enter).
func (m Model) handleBlameClick(mouseY int) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	if fv.err != "" || len(fv.lines) == 0 {
		return m, nil
	}
	// Title bar + 3-line sticky blame head sit above the scrollable body.
	bodyLine := mouseY - contentTopBarHeight - 1 - 3
	if bodyLine < 0 {
		return m, nil
	}
	bodyH := fileViewContentH(m)
	layout := fv.blameLayout
	if !fv.blameCacheValid(m.width, bodyH) {
		fv.buildBlameCache(m.width, bodyH)
		layout = fv.blameLayout
	}
	if len(layout.starts) == 0 {
		return m, nil
	}
	// renderFileBlame centers the cursor in the viewport; reproduce that
	// scroll offset so the click maps to the line actually drawn there.
	cursorY := min(fv.cursorY, len(fv.lines)-1)
	cursorBodyRow := -1
	if cursorY >= 0 && cursorY < len(layout.starts) {
		cursorBodyRow = layout.starts[cursorY]
	}
	termScrollY := 0
	if cursorBodyRow >= 0 {
		termScrollY = cursorBodyRow - bodyH/2
	}
	termScrollY = max(0, min(termScrollY, max(0, layout.total-bodyH)))
	rowIdx, _ := layout.rowAt(bodyLine + termScrollY)
	if rowIdx < 0 || rowIdx >= len(fv.lines) {
		return m, nil
	}
	if rowIdx == fv.cursorY {
		line := fv.lines[rowIdx]
		return m.openRevisionDiff(line.ChangeID, line.CommitID, 0, line.Description)
	}
	fv.cursorY = rowIdx
	return m, nil
}

// handleHistoryClick moves the file-history cursor to the commit under the
// mouse. Re-clicking the selected commit opens its diff (same as enter).
func (m Model) handleHistoryClick(mouseY int) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	// The history view renders its own title bar above the log list.
	contentY := mouseY - contentTopBarHeight - 1
	idx, ok := logEntryAtContentY(fv.hist, fv.histCur, fv.histOff, contentY, m.contentHeight()-1)
	if !ok {
		return m, nil
	}
	if idx == fv.histCur {
		e := fv.hist[idx]
		return m.openRevisionDiff(e.ChangeID, e.CommitID, e.ChangeIDPrefixLen, e.Subject)
	}
	fv.histCur = idx
	m.recomputeFileHistOffset()
	return m, nil
}

// updateHover recomputes the view-specific hover target from a mouse position.
// It clears hover when the mouse is outside the content area or over the
// scrollbar. Clicks are also fed through here so pressing sets the highlight
// even if no motion event preceded the press.
func (m Model) updateHover(x, y int) Model {
	m.hover = hoverState{valid: true, logIdx: -1, diffRow: -1, pickerRow: -1, fzfRow: -1, blameLine: -1, histIdx: -1}

	// Check shortcut hover (help bar / status bar menus) first — this works
	// regardless of content area bounds.
	if span, ok := m.shortcutSpanAt(x, y); ok {
		m.hoverShortcut = span.keyHint
	} else {
		m.hoverShortcut = ""
	}

	if x >= m.width-scrollbarWidth || x < 0 {
		m.hover.valid = false
		return m
	}
	if y < contentTopBarHeight || y >= contentTopBarHeight+m.contentHeight() {
		m.hover.valid = false
		return m
	}

	switch {
	case m.diffOpen:
		if rowIdx, ok := m.diffRowAtMouseY(y); ok {
			m.hover.diffRow = rowIdx
		}
	case m.view == viewFile:
		switch m.fileView.phase {
		case fileBlame:
			if line, ok := m.blameLineAtMouseY(y); ok {
				m.hover.blameLine = line
			}
		case fileHistory:
			if idx, ok := m.historyEntryAtMouseY(y); ok {
				m.hover.histIdx = idx
			}
		default:
			if m.fileView.fzfActive {
				if idx, ok := m.fzfResultAtMouseY(y); ok {
					m.hover.fzfRow = idx
				}
			} else {
				if idx, ok := m.pickerRowAtMouseY(y); ok {
					m.hover.pickerRow = idx
				}
			}
		}
	case m.view == viewHelp:
		m.hover.valid = false
	default:
		if idx, ok := m.logEntryAtMouseY(y); ok {
			m.hover.logIdx = idx
		}
	}
	return m
}

// logEntryAtMouseY maps a terminal Y coordinate to a log entry index in the
// main log view.
func (m Model) logEntryAtMouseY(mouseY int) (int, bool) {
	focus := m.cursor
	if m.rebaseMode {
		focus = m.rebaseDest
	}
	if m.squashMode {
		focus = m.squashDest
	}
	return logEntryAtContentY(m.entries, focus, m.offset, mouseY-contentTopBarHeight, m.contentHeight())
}

// bookmarkSegmentAt maps a terminal coordinate to the bookmark rendered on a
// commit's header line, if any. It mirrors renderLog's header segment layout
// (prefix: leading space, HeaderPrefix, space, ChangeID, space, Authors,
// space, Date, space, CommitID; then for each bookmark a separating space and
// the bookmark text). Returns ok=false when the coordinate is not on a
// header row or not within a bookmark segment's cell range. contentX is the
// 0-based terminal X (already excluding the scrollbar, which the caller
// filters out).
func bookmarkSegmentAt(entries []jj.LogEntry, focus, offset, contentY, contentX, height int) (name string, entryIdx int, ok bool) {
	lineIdx := contentY - 1 // skip the list's top padding row
	if lineIdx < 0 || contentX < 0 {
		return "", 0, false
	}
	off, end := logWindow(entries, focus, offset, height-1)
	acc := 0
	for i := off; i < end; i++ {
		entryHeaderLine := acc // first sub-line of the entry is the header
		h := commitLines(entries[i])
		if lineIdx == entryHeaderLine {
			// Hit-test X against this entry's bookmark segments.
			prefix := 1 + runeWidthStr(entries[i].HeaderPrefix) + 1 +
				runeWidthStr(entries[i].ChangeID) + 1 +
				runeWidthStr(entries[i].Authors) + 1 +
				runeWidthStr(entries[i].Date) + 1 +
				runeWidthStr(entries[i].CommitID)
			x := contentX
			for _, bm := range entries[i].Bookmarks {
				segStart := prefix + 1 // separating space
				segEnd := segStart + runeWidthStr(bm)
				if x >= segStart && x < segEnd {
					return bm, i, true
				}
				prefix = segEnd
			}
			return "", 0, false
		}
		acc += h
	}
	return "", 0, false
}

// historyEntryAtMouseY maps a terminal Y coordinate to a file-history entry
// index.
func (m Model) historyEntryAtMouseY(mouseY int) (int, bool) {
	fv := &m.fileView
	return logEntryAtContentY(fv.hist, fv.histCur, fv.histOff, mouseY-contentTopBarHeight-1, m.contentHeight()-1)
}

// pickerRowAtMouseY maps a terminal Y coordinate to a file-picker tree row
// index.
func (m Model) pickerRowAtMouseY(mouseY int) (int, bool) {
	fv := &m.fileView
	rowIdx := mouseY - contentTopBarHeight - 1
	if rowIdx < 0 {
		return 0, false
	}
	contentH := m.contentHeight() - 1
	if contentH < 0 {
		contentH = 0
	}
	start, end := fv.pickerVisibleRange(contentH)
	i := start + rowIdx
	if i >= end || i >= len(fv.rows) {
		return 0, false
	}
	return i, true
}

// fzfResultAtMouseY maps a terminal Y coordinate to a fuzzy-finder result
// index.
func (m Model) fzfResultAtMouseY(mouseY int) (int, bool) {
	fv := &m.fileView
	rowIdx := mouseY - contentTopBarHeight - 1
	resIdx := rowIdx - 2
	if resIdx < 0 {
		return 0, false
	}
	contentH := m.contentHeight() - 3
	if contentH < 0 {
		contentH = 0
	}
	start, end := fv.fzfVisibleRange(contentH)
	i := start + resIdx
	if i >= end || i >= len(fv.fzfResults) {
		return 0, false
	}
	return i, true
}

// blameLineAtMouseY maps a terminal Y coordinate to a blame source-line index.
func (m Model) blameLineAtMouseY(mouseY int) (int, bool) {
	fv := &m.fileView
	if fv.err != "" || len(fv.lines) == 0 {
		return 0, false
	}
	bodyLine := mouseY - contentTopBarHeight - 1 - 3
	if bodyLine < 0 {
		return 0, false
	}
	bodyH := fileViewContentH(m)
	layout := fv.blameLayout
	if !fv.blameCacheValid(m.width, bodyH) {
		fv.buildBlameCache(m.width, bodyH)
		layout = fv.blameLayout
	}
	if len(layout.starts) == 0 {
		return 0, false
	}
	cursorY := min(fv.cursorY, len(fv.lines)-1)
	cursorBodyRow := -1
	if cursorY >= 0 && cursorY < len(layout.starts) {
		cursorBodyRow = layout.starts[cursorY]
	}
	termScrollY := 0
	if cursorBodyRow >= 0 {
		termScrollY = cursorBodyRow - bodyH/2
	}
	termScrollY = max(0, min(termScrollY, max(0, layout.total-bodyH)))
	rowIdx, _ := layout.rowAt(bodyLine + termScrollY)
	if rowIdx < 0 || rowIdx >= len(fv.lines) {
		return 0, false
	}
	return rowIdx, true
}

// ── Help/status bar shortcut clicks ─────────────────────────────────────────

// menuSpan records the screen position of a single clickable menu item.
type menuSpan struct {
	x1, x2, y int // screen coords; x2 exclusive
	keyHint   string
}

// computeMenuSpans mirrors wrapMenu's greedy packing and returns the screen
// position of each item's label. startY is the Y of the first row.
func computeMenuSpans(width int, prefix, sep string, items [][2]string, startY int) []menuSpan {
	if width <= 1 || len(items) == 0 {
		return nil
	}
	prefixW := lipgloss.Width(prefix)
	var spans []menuSpan
	rowY := startY
	curW := prefixW
	hasItem := false
	for _, it := range items {
		itemW := lipgloss.Width(it[0])
		addW := itemW
		if hasItem {
			addW += len(sep)
		}
		if curW+addW > width && hasItem {
			rowY++
			curW = 1
			hasItem = false
			addW = itemW
		}
		if hasItem {
			curW += len(sep)
		}
		x1 := curW
		curW += itemW
		hasItem = true
		spans = append(spans, menuSpan{x1: x1, x2: curW, y: rowY, keyHint: it[1]})
	}
	return spans
}

// statusBarStartY returns the terminal Y of the first status bar row.
func (m Model) statusBarStartY() int {
	y := contentTopBarHeight + m.contentHeight()
	if m.suggestionsVisible() {
		y++
	}
	return y
}

// helpBarStartY returns the terminal Y of the first help bar row.
func (m Model) helpBarStartY() int {
	return m.statusBarStartY() + m.statusBarHeight()
}

// tryShortcutClick checks whether a left-click at (x, y) lands on a menu item
// in the status bar (sub-menu modes) or the help bar. If so, it dispatches the
// corresponding key and returns ok=true.
func (m Model) tryShortcutClick(x, y int) (Model, tea.Cmd, bool) {
	// Status bar menu items (sub-menu modes with no sub-action selected).
	type menuDef struct {
		prefix string
		items  [][2]string
	}
	var statusMenu menuDef
	switch {
	case m.bookmarkMode && m.bookmarkAction == "":
		statusMenu = menuDef{" [bookmark mode] ", bookmarkMenuItems}
	case m.tagMode && m.tagAction == "":
		statusMenu = menuDef{" [tag mode] ", tagMenuItems}
	case m.gitMode && m.remoteMode && m.remoteAction == "":
		statusMenu = menuDef{" [git > remote] ", remoteMenuItems}
	case m.gitMode && !m.remoteMode:
		statusMenu = menuDef{" [git mode] ", gitMenuItems}
	}
	if statusMenu.items != nil {
		spans := computeMenuSpans(m.width, statusMenu.prefix, " ", statusMenu.items, m.statusBarStartY())
		if span, ok := hitMenuSpan(spans, x, y); ok {
			return m.dispatchShortcutKey(span.keyHint)
		}
	}

	// Help bar items.
	helpItems := m.helpBarItems()
	if helpItems != nil {
		spans := computeMenuSpans(m.width, " ", "  ", helpItems, m.helpBarStartY())
		if span, ok := hitMenuSpan(spans, x, y); ok {
			return m.dispatchShortcutKey(span.keyHint)
		}
	}

	return m, nil, false
}

func hitMenuSpan(spans []menuSpan, x, y int) (menuSpan, bool) {
	for _, s := range spans {
		if y == s.y && x >= s.x1 && x < s.x2 {
			return s, true
		}
	}
	return menuSpan{}, false
}

// dispatchShortcutKey synthesises a KeyMsg from a display key hint (e.g. "⏎",
// "↑", "d") and routes it through the normal key handler.
func (m Model) dispatchShortcutKey(keyHint string) (Model, tea.Cmd, bool) {
	msg, _ := keyMsgFromHint(keyHint)
	nm, cmd := m.handleKey(msg)
	m = nm.(Model)
	return m, cmd, true
}

// keyMsgFromHint converts a display key hint string into a (KeyMsg, string)
// pair suitable for handleKey. Simple letter keys map directly; special glyphs
// map to their corresponding KeyType.
func keyMsgFromHint(hint string) (tea.KeyMsg, string) {
	switch hint {
	case "⏎":
		return tea.KeyMsg{Type: tea.KeyEnter}, "enter"
	case "↑":
		return tea.KeyMsg{Type: tea.KeyUp}, "up"
	case "↓":
		return tea.KeyMsg{Type: tea.KeyDown}, "down"
	case "←":
		return tea.KeyMsg{Type: tea.KeyLeft}, "left"
	case "→":
		return tea.KeyMsg{Type: tea.KeyRight}, "right"
	case "esc", "esc/q", "esc/back":
		return tea.KeyMsg{Type: tea.KeyEscape}, "esc"
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}, " "
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, "tab"
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}, "backspace"
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(hint)}, hint
	}
}

// ── Shortcuts hover highlighting ─────────────────────────────────────────────

// shortcutSpanAt returns the menu span under the mouse position (x, y), if
// the mouse is over a shortcut in the status or help bar.
func (m Model) shortcutSpanAt(x, y int) (menuSpan, bool) {
	// Status bar menus (sub-menu modes).
	switch {
	case m.bookmarkMode && m.bookmarkAction == "":
		return hitMenuSpan(computeMenuSpans(m.width, " [bookmark mode] ", " ", bookmarkMenuItems, m.statusBarStartY()), x, y)
	case m.tagMode && m.tagAction == "":
		return hitMenuSpan(computeMenuSpans(m.width, " [tag mode] ", " ", tagMenuItems, m.statusBarStartY()), x, y)
	case m.gitMode && m.remoteMode && m.remoteAction == "":
		return hitMenuSpan(computeMenuSpans(m.width, " [git > remote] ", " ", remoteMenuItems, m.statusBarStartY()), x, y)
	case m.gitMode && !m.remoteMode:
		return hitMenuSpan(computeMenuSpans(m.width, " [git mode] ", " ", gitMenuItems, m.statusBarStartY()), x, y)
	}
	// Help bar.
	if helpItems := m.helpBarItems(); helpItems != nil {
		return hitMenuSpan(computeMenuSpans(m.width, " ", "  ", helpItems, m.helpBarStartY()), x, y)
	}
	return menuSpan{}, false
}
