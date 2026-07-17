package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// hoverState tracks the item currently under the mouse for visual hover
// highlighting. It is view-specific: only one target field is populated at a
// time.
type hoverState struct {
	valid     bool
	logIdx    int
	logEdge   int // edge-line index within logIdx, or -1
	diffRow   int
	pickerRow int
	fzfRow    int
	blameLine int
	histIdx   int
	searchRow int
	refName   string // bookmark/tag name under the mouse, or ""
	refKind   string // "bookmark" | "tag", or ""
}

// bookmarkDragState tracks an in-progress mouse drag of a bookmark from its
// source revision toward a drop target revision. sourceIdx/name are fixed for
// the drag's lifetime; targetIdx is updated each motion event (or -1 when the
// cursor is off any commit row). On release, if targetIdx differs from
// sourceIdx the bookmark is moved via jj bookmark move.
type bookmarkDragState struct {
	name      string
	sourceIdx int
	targetIdx int
}

// refInfo records the bookmark or tag that a context menu was opened on, so
// the menu items can act on the specific ref.
type refInfo struct {
	kind     string // "bookmark" | "tag"
	name     string
	entryIdx int
	rev      string // ChangeID of the entry the ref is on (for tag rename)
}

// renameRef tracks the target of an in-progress rename (entered from the
// bookmark/tag context menu). For bookmarks it maps to jj bookmark rename;
// for tags (which have no rename command) it is implemented as delete + set
// at the same revision.
type renameRef struct {
	kind    string // "bookmark" | "tag"
	oldName string
	rev     string
}

// contextMenuItem is one entry in the right-click context menu.
type contextMenuItem struct {
	label   string
	keyHint string
	action  func(Model) (tea.Model, tea.Cmd)
}

const (
	contextMenuMinWidth  = 24
	contextMenuMaxHeight = 16
)

// menuItem is a convenience constructor for contextMenuItem.
func menuItem(label, keyHint string, action func(Model) (tea.Model, tea.Cmd)) contextMenuItem {
	return contextMenuItem{label: label, keyHint: keyHint, action: action}
}

// openContextMenu builds and positions the context menu for the current view.
// The clicked row is selected first (without activation) so the menu applies
// to it.
func (m *Model) openContextMenu(x, y int) {
	items := m.buildContextMenuItems()
	if len(items) == 0 {
		return
	}
	mw := m.contextMenuWidth(items)
	mh := min(len(items), contextMenuMaxHeight)

	// The border adds one cell on each side (left/right) and one row on
	// top/bottom.
	fw, fh := mw+2, mh+2

	// Clamp position so the whole menu stays inside the terminal.
	if x+fw > m.width {
		x = m.width - fw
	}
	if x < 0 {
		x = 0
	}
	contentBottom := contentTopBarHeight + m.contentHeight()
	if y+fh > contentBottom {
		y = contentBottom - fh
	}
	if y < contentTopBarHeight {
		y = contentTopBarHeight
	}

	m.contextMenuOpen = true
	m.contextMenuItems = items
	m.contextMenuCursor = 0
	m.contextMenuOffset = 0
	m.contextMenuX = x
	m.contextMenuY = y
}

// closeContextMenu dismisses the menu and clears its state.
func (m *Model) closeContextMenu() {
	m.contextMenuOpen = false
	m.contextMenuItems = nil
	m.contextMenuCursor = 0
	m.contextMenuOffset = 0
	m.contextMenuX = 0
	m.contextMenuY = 0
	m.contextMenuRef = nil
}

// contextMenuWidth returns the rendered inner width of the menu (excluding
// borders), capped to the terminal width.
func (m Model) contextMenuWidth(items []contextMenuItem) int {
	maxW := contextMenuMinWidth
	for _, it := range items {
		// Inner layout: " " + label + " " + keyHint + " " = label + key + 3.
		w := lipgloss.Width(it.label) + lipgloss.Width(it.keyHint) + 3
		if w > maxW {
			maxW = w
		}
	}
	// Reserve 2 columns for the left/right borders.
	if maxW > m.width-2 {
		maxW = m.width - 2
	}
	return maxW
}

// openContextMenuCmd is the MouseMsg handler entry point for right-clicks.
func (m Model) openContextMenuCmd(x, y int) (tea.Model, tea.Cmd) {
	if !m.ready || m.modalInputActive() {
		return m, nil
	}
	// Right-clicking while the menu is open closes it.
	if m.contextMenuOpen {
		m.closeContextMenu()
		return m, nil
	}
	// Check for a right-click on a bookmark or tag segment — if so, select
	// the entry and build a ref-specific context menu.
	if x < m.width-scrollbarWidth && !m.diffOpen && m.view == viewLog && !m.rebaseMode && !m.squashMode {
		if kind, name, idx, ok := m.refAtMouse(x, y); ok {
			rev := ""
			if idx >= 0 && idx < len(m.entries) {
				rev = m.entries[idx].ChangeID
			}
			m.cursor = idx
			m.recomputeOffset()
			m.contextMenuRef = &refInfo{kind: kind, name: name, entryIdx: idx, rev: rev}
			m.openContextMenu(x, y)
			return m, nil
		}
	}
	m.contextMenuRef = nil
	// Select the row under the click (but do not activate it).
	if x < m.width-scrollbarWidth {
		m = m.rightClickSelect(y)
	}
	m.openContextMenu(x, y)
	return m, nil
}

// buildContextMenuItems returns the context-sensitive actions for the current
// view/state.
func (m Model) buildContextMenuItems() []contextMenuItem {
	// Ref-specific menu (right-click on a bookmark or tag).
	if m.contextMenuRef != nil {
		return m.refContextMenuItems()
	}
	switch {
	case m.rebaseMode:
		return m.rebaseContextMenuItems()
	case m.squashMode:
		return m.squashContextMenuItems()
	case m.diffOpen && m.splitMode:
		return m.splitContextMenuItems()
	case m.diffOpen:
		return m.diffContextMenuItems()
	case m.view == viewFile:
		switch m.fileView.phase {
		case fileBlame:
			return m.blameContextMenuItems()
		case fileHistory:
			return m.historyContextMenuItems()
		default:
			return m.pickerContextMenuItems()
		}
	case m.view == viewHelp:
		return m.helpContextMenuItems()
	default:
		return m.logContextMenuItems()
	}
}

// rightClickSelect moves the cursor/selection to the row under the click
// without activating it, mirroring the left-click selection logic.
func (m Model) rightClickSelect(mouseY int) Model {
	switch {
	case m.diffOpen:
		return m.rightClickSelectDiff(mouseY)
	case m.view == viewFile:
		switch m.fileView.phase {
		case fileBlame:
			return m.rightClickSelectBlame(mouseY)
		case fileHistory:
			return m.rightClickSelectHistory(mouseY)
		default:
			return m.rightClickSelectPicker(mouseY)
		}
	case m.view == viewHelp:
		return m
	default:
		return m.rightClickSelectLog(mouseY)
	}
}

// rightClickSelectLog selects the commit under the mouse in the log view (or
// the rebase/squash destination indicator when those modes are active).
func (m Model) rightClickSelectLog(mouseY int) Model {
	focus := m.cursor
	if m.rebaseMode {
		focus = m.rebaseDest
	}
	if m.squashMode {
		focus = m.squashDest
	}
	idx, ok := logEntryAtContentY(m.entries, focus, m.offset, mouseY-contentTopBarHeight, m.contentHeight())
	if !ok {
		return m
	}
	if m.rebaseMode {
		m.rebaseDest = idx
		m.recomputeOffset()
		return m
	}
	if m.squashMode {
		m.squashDest = idx
		m.recomputeOffset()
		return m
	}
	m.cursor = idx
	m.recomputeOffset()
	return m
}

// rightClickSelectDiff moves the diff chunk cursor to the row under the mouse.
func (m Model) rightClickSelectDiff(mouseY int) Model {
	rowIdx, ok := m.diffRowAtMouseY(mouseY)
	if !ok {
		return m
	}
	m.setDiffCursorToRow(rowIdx)
	return m
}

// rightClickSelectPicker moves the file-picker cursor to the row under the
// mouse, including the fzf overlay.
func (m Model) rightClickSelectPicker(mouseY int) Model {
	fv := &m.fileView
	rowIdx := mouseY - contentTopBarHeight - 1
	if rowIdx < 0 {
		return m
	}
	if fv.fzfActive {
		resIdx := rowIdx - 2
		if resIdx < 0 {
			return m
		}
		contentH := m.contentHeight() - 3
		if contentH < 0 {
			contentH = 0
		}
		start, end := fv.fzfVisibleRange(contentH)
		i := start + resIdx
		if i >= end || i >= len(fv.fzfResults) {
			return m
		}
		fv.fzfCursor = i
		return m
	}
	contentH := m.contentHeight() - 1
	if contentH < 0 {
		contentH = 0
	}
	start, end := fv.pickerVisibleRange(contentH)
	i := start + rowIdx
	if i >= end || i >= len(fv.rows) {
		return m
	}
	fv.cursor = i
	return m
}

// rightClickSelectBlame moves the blame cursor to the source line under the
// mouse.
func (m Model) rightClickSelectBlame(mouseY int) Model {
	fv := &m.fileView
	if fv.err != "" || len(fv.lines) == 0 {
		return m
	}
	bodyLine := mouseY - contentTopBarHeight - 1 - 3
	if bodyLine < 0 {
		return m
	}
	bodyH := fileViewContentH(m)
	layout := fv.blameLayout
	if !fv.blameCacheValid(m.width, bodyH) {
		fv.buildBlameCache(m.width, bodyH)
		layout = fv.blameLayout
	}
	if len(layout.starts) == 0 {
		return m
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
		return m
	}
	fv.cursorY = rowIdx
	return m
}

// rightClickSelectHistory selects the commit under the mouse in the file
// history view.
func (m Model) rightClickSelectHistory(mouseY int) Model {
	fv := &m.fileView
	contentY := mouseY - contentTopBarHeight - 1
	idx, ok := logEntryAtContentY(fv.hist, fv.histCur, fv.histOff, contentY, m.contentHeight()-1)
	if !ok {
		return m
	}
	fv.histCur = idx
	m.recomputeFileHistOffset()
	return m
}

// handleContextMenuKey drives keyboard navigation and activation while the
// context menu is open.
func (m Model) handleContextMenuKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q":
		m.closeContextMenu()
		return m, nil
	case "enter":
		return m.activateContextMenuItem()
	case "up", "k":
		if m.contextMenuCursor > 0 {
			m.contextMenuCursor--
		}
		return m, nil
	case "down", "j":
		if m.contextMenuCursor < len(m.contextMenuItems)-1 {
			m.contextMenuCursor++
		}
		return m, nil
	case "home", "g":
		m.contextMenuCursor = 0
		return m, nil
	case "end", "G":
		m.contextMenuCursor = len(m.contextMenuItems) - 1
		return m, nil
	}
	return m, nil
}

// handleContextMenuMouse handles mouse events while the menu is open: item
// hover/click, wheel scrolling, and dismissal on clicks outside the menu.
// The menu's clickable item area starts one row below the top border and one
// column inside the left/right borders.
func (m Model) handleContextMenuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mw := m.contextMenuWidth(m.contextMenuItems)
	mh := min(len(m.contextMenuItems), contextMenuMaxHeight)
	menuTop := m.contextMenuY
	menuBottom := m.contextMenuY + mh + 1 // +1 top border, inclusive bottom border
	menuRight := m.contextMenuX + mw + 2
	inMenu := msg.X >= m.contextMenuX && msg.X < menuRight &&
		msg.Y >= menuTop && msg.Y <= menuBottom

	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonLeft:
			if inMenu {
				itemY := msg.Y - m.contextMenuY - 1 // -1 for top border
				if itemY >= 0 && itemY < mh {
					idx := m.contextMenuOffset + itemY
					if idx >= 0 && idx < len(m.contextMenuItems) {
						m.contextMenuCursor = idx
						return m.activateContextMenuItem()
					}
				}
			}
			m.closeContextMenu()
			return m, nil
		case tea.MouseButtonRight:
			m.closeContextMenu()
			return m, nil
		case tea.MouseButtonWheelUp:
			if m.contextMenuCursor > 0 {
				m.contextMenuCursor--
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.contextMenuCursor < len(m.contextMenuItems)-1 {
				m.contextMenuCursor++
			}
			return m, nil
		}
	case tea.MouseActionMotion:
		if inMenu {
			itemY := msg.Y - m.contextMenuY - 1
			if itemY >= 0 && itemY < mh {
				idx := m.contextMenuOffset + itemY
				if idx >= 0 && idx < len(m.contextMenuItems) {
					m.contextMenuCursor = idx
				}
			}
		}
	}
	return m, nil
}

// activateContextMenuItem executes the currently selected menu item and closes
// the menu.
func (m Model) activateContextMenuItem() (tea.Model, tea.Cmd) {
	if m.contextMenuCursor < 0 || m.contextMenuCursor >= len(m.contextMenuItems) {
		return m, nil
	}
	item := m.contextMenuItems[m.contextMenuCursor]
	m.closeContextMenu()
	return item.action(m)
}

// renderContextMenu overlays the open menu onto the rendered lines. The menu
// is drawn with a thin border: ┌─┐ top, │ │ sides, └─┘ bottom.
func (m Model) renderContextMenu(lines []string) []string {
	if !m.contextMenuOpen || len(m.contextMenuItems) == 0 {
		return lines
	}
	mw := m.contextMenuWidth(m.contextMenuItems)
	mh := min(len(m.contextMenuItems), contextMenuMaxHeight)

	// Keep the cursor inside the visible window.
	if m.contextMenuCursor < m.contextMenuOffset {
		m.contextMenuOffset = m.contextMenuCursor
	}
	if m.contextMenuCursor >= m.contextMenuOffset+mh {
		m.contextMenuOffset = m.contextMenuCursor - mh + 1
	}

	fw := mw + 2 // full width including borders

	// Top border: ┌───┐
	m.overlayMenuLine(lines, m.contextMenuY, m.contextMenuX, fw,
		bgRow(fw, colPanel,
			seg{text: "┌" + strings.Repeat("─", mw) + "┐", fg: colBorder, bg: colPanel},
		))

	// Item rows.
	for i := 0; i < mh; i++ {
		idx := m.contextMenuOffset + i
		if idx >= len(m.contextMenuItems) {
			break
		}
		row := m.contextMenuY + 1 + i
		if row < 0 || row >= len(lines) {
			break
		}
		item := m.contextMenuItems[idx]
		selected := idx == m.contextMenuCursor
		menuLine := m.renderContextMenuItem(mw, item, selected)
		m.overlayMenuLine(lines, row, m.contextMenuX, fw, menuLine)
	}

	// Bottom border: └───┘
	m.overlayMenuLine(lines, m.contextMenuY+1+mh, m.contextMenuX, fw,
		bgRow(fw, colPanel,
			seg{text: "└" + strings.Repeat("─", mw) + "┘", fg: colBorder, bg: colPanel},
		))

	return lines
}

// overlayMenuLine splices a rendered menu line (of width w) into an existing
// terminal line at column x. The line is clipped/padded as needed.
func (m Model) overlayMenuLine(lines []string, row, x, w int, menuLine string) []string {
	if row < 0 || row >= len(lines) {
		return lines
	}
	left := ansi.Truncate(lines[row], x, "")
	right := ansi.TruncateLeft(lines[row], x+w, "")
	lines[row] = left + menuLine + right
	return lines
}

// renderContextMenuItem renders a single menu row of inner width mw, flanked
// by left/right border characters. Inner layout: " " + label + " " + key + " ".
func (m Model) renderContextMenuItem(mw int, item contextMenuItem, selected bool) string {
	bg := colPanel
	fg := colText
	if selected {
		bg = colElement
		fg = colYellow
	}
	keyW := lipgloss.Width(item.keyHint)
	labelW := mw - keyW - 3 // 3 padding columns: space, space, space
	label := item.label
	if lipgloss.Width(label) > labelW {
		label = ansi.Truncate(label, labelW, "…")
	}
	pad := labelW - lipgloss.Width(label)
	if pad < 0 {
		pad = 0
	}
	return bgRow(mw+2, bg,
		seg{text: "│", fg: colBorder, bg: colPanel},
		seg{text: " " + label + strings.Repeat(" ", pad) + " ", fg: fg, bg: bg},
		seg{text: item.keyHint + " ", fg: colGray, bg: bg},
		seg{text: "│", fg: colBorder, bg: colPanel},
	)
}

// ── Per-view context menu builders ──────────────────────────────────────────

func (m Model) logContextMenuItems() []contextMenuItem {
	var items []contextMenuItem
	hasSel := m.selectedEntry() != nil

	if hasSel {
		items = append(items, menuItem("open diff", "enter", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleLogKey(tea.KeyMsg{Type: tea.KeyEnter}, "enter")
		}))
	}
	if hasSel {
		items = append(items, menuItem("describe", "d", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}, "d")
		}))
	}
	if hasSel {
		items = append(items, menuItem("AI describe", "D", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}}, "D")
		}))
	}
	if hasSel {
		items = append(items, menuItem("edit", "e", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, "e")
		}))
	}
	items = append(items, menuItem("new change", "n", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, "n")
	}))
	if hasSel {
		items = append(items, menuItem("abandon", "a", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, "a")
		}))
	}
	if hasSel {
		items = append(items, menuItem("absorb", "x", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}, "x")
		}))
	}
	items = append(items, menuItem("rebase", "r", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}, "r")
	}))
	items = append(items, menuItem("squash", "s", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}, "s")
	}))
	items = append(items, menuItem("bookmark", "b", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}, "b")
	}))
	items = append(items, menuItem("tag", "t", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}, "t")
	}))
	items = append(items, menuItem("git", "g", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}, "g")
	}))
	items = append(items, menuItem("file view", "f", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}, "f")
	}))
	items = append(items, menuItem("toggle all revisions", "A", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}, "A")
	}))
	items = append(items, menuItem("undo", "u", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}, "u")
	}))
	items = append(items, menuItem("redo", "R", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}, "R")
	}))
	return items
}

func (m Model) rebaseContextMenuItems() []contextMenuItem {
	return []contextMenuItem{
		menuItem("confirm rebase", "enter", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleRebaseKey("enter")
		}),
		menuItem("toggle scope", "s", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleRebaseKey("s")
		}),
		menuItem("cycle placement", "tab", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleRebaseKey("tab")
		}),
		menuItem("cancel", "esc", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleRebaseKey("esc")
		}),
	}
}

func (m Model) squashContextMenuItems() []contextMenuItem {
	return []contextMenuItem{
		menuItem("confirm squash", "enter", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleSquashKey("enter")
		}),
		menuItem("cancel", "esc", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleSquashKey("esc")
		}),
	}
}

func (m Model) diffContextMenuItems() []contextMenuItem {
	var items []contextMenuItem
	items = append(items, menuItem("close diff", "q", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleDiffKey("q")
	}))
	if m.diffIsRevision && m.diffRev != "" {
		items = append(items, menuItem("describe", "d", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleDiffKey("d")
		}))
		items = append(items, menuItem("AI describe", "D", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleDiffKey("D")
		}))
		items = append(items, menuItem("new change", "n", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleDiffKey("n")
		}))
		items = append(items, menuItem("split", "s", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleDiffKey("s")
		}))
		items = append(items, menuItem("absorb", "x", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleDiffKey("x")
		}))
	}
	if fileIdx, ok := m.cursorFileHeader(); ok {
		path := m.diffRows[fileIdx].path
		collapsed := m.diffCollapsed != nil && m.diffCollapsed[path]
		if collapsed {
			items = append(items, menuItem("expand file", "l", func(m Model) (tea.Model, tea.Cmd) {
				return m.handleDiffKey("l")
			}))
		} else {
			items = append(items, menuItem("collapse file", "h", func(m Model) (tea.Model, tea.Cmd) {
				return m.handleDiffKey("h")
			}))
		}
	}
	return items
}

func (m Model) splitContextMenuItems() []contextMenuItem {
	return []contextMenuItem{
		menuItem("toggle mark", "space", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleSplitKey(" ")
		}),
		menuItem("confirm split", "c", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleSplitKey("c")
		}),
		menuItem("cancel", "q", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleSplitKey("q")
		}),
	}
}

func (m Model) pickerContextMenuItems() []contextMenuItem {
	fv := &m.fileView
	if fv.fzfActive {
		var items []contextMenuItem
		if fv.fzfCursor >= 0 && fv.fzfCursor < len(fv.fzfResults) {
			items = append(items, menuItem("open file", "enter", func(m Model) (tea.Model, tea.Cmd) {
				return m.handleFzfKey(tea.KeyMsg{Type: tea.KeyEnter}, "enter")
			}))
		}
		items = append(items, menuItem("close finder", "esc", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleFzfKey(tea.KeyMsg{Type: tea.KeyEscape}, "esc")
		}))
		return items
	}
	var items []contextMenuItem
	if row := fv.curRow(); row != nil {
		if row.node.isDir {
			items = append(items, menuItem("expand/collapse", "enter", func(m Model) (tea.Model, tea.Cmd) {
				return m.handleFilePickerKey(tea.KeyMsg{Type: tea.KeyEnter}, "enter")
			}))
		} else {
			items = append(items, menuItem("open file", "enter", func(m Model) (tea.Model, tea.Cmd) {
				return m.handleFilePickerKey(tea.KeyMsg{Type: tea.KeyEnter}, "enter")
			}))
		}
	}
	items = append(items, menuItem("leave file view", "q", func(m Model) (tea.Model, tea.Cmd) {
		return m.handleFilePickerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}, "q")
	}))
	return items
}

func (m Model) blameContextMenuItems() []contextMenuItem {
	return []contextMenuItem{
		menuItem("open commit", "enter", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleFileBlameKey("enter")
		}),
		menuItem("file history", "h", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleFileBlameKey("h")
		}),
		menuItem("back", "q", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleFileBlameKey("q")
		}),
	}
}

func (m Model) historyContextMenuItems() []contextMenuItem {
	return []contextMenuItem{
		menuItem("open commit", "enter", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleFileHistoryKey("enter")
		}),
		menuItem("back", "q", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleFileHistoryKey("q")
		}),
	}
}

func (m Model) helpContextMenuItems() []contextMenuItem {
	return []contextMenuItem{
		menuItem("close help", "q", func(m Model) (tea.Model, tea.Cmd) {
			return m.handleHelpKey("q"), nil
		}),
	}
}

// refContextMenuItems builds the context menu for a right-click on a bookmark
// or tag segment. It offers push to origin, forget (delete for tags), and
// rename.
func (m Model) refContextMenuItems() []contextMenuItem {
	ref := m.contextMenuRef
	if ref == nil {
		return nil
	}
	var items []contextMenuItem

	// Push to origin.
	items = append(items, menuItem("push to origin", "p", func(m Model) (tea.Model, tea.Cmd) {
		r := m.runner
		if r == nil {
			return m, nil
		}
		if ref.kind == "bookmark" {
			return m.busySimpleCmd("pushing "+ref.name+"…",
				func() error { return r.GitPush("--bookmark", ref.name) },
				"pushed "+ref.name)
		}
		return m.busySimpleCmd("pushing tag "+ref.name+"…",
			func() error { return r.GitPush("--tag", ref.name) },
			"pushed tag "+ref.name)
	}))

	// Forget (delete for tags).
	forgetLabel := "forget"
	if ref.kind == "tag" {
		forgetLabel = "delete"
	}
	items = append(items, menuItem(forgetLabel, "f", func(m Model) (tea.Model, tea.Cmd) {
		r := m.runner
		if r == nil {
			return m, nil
		}
		var spec actionSpec
		if ref.kind == "bookmark" {
			spec = actionSpec{
				run:     func() error { return r.BookmarkForget(ref.name) },
				okMsg:   "forgot bookmark: " + ref.name,
				elevate: func(flag string) func() error { return func() error { return r.BookmarkForget(ref.name, flag) } },
			}
		} else {
			spec = actionSpec{
				run:     func() error { return r.TagDelete(ref.name) },
				okMsg:   "deleted tag: " + ref.name,
				elevate: func(flag string) func() error { return func() error { return r.TagDelete(ref.name, flag) } },
			}
		}
		return m.busyActionCmd(forgetLabel+"ing "+ref.name+"…", spec)
	}))

	// Rename.
	items = append(items, menuItem("rename", "r", func(m Model) (tea.Model, tea.Cmd) {
		m.renameMode = true
		m.renameInput = ""
		m.renameTarget = renameRef{kind: ref.kind, oldName: ref.name, rev: ref.rev}
		return m, nil
	}))

	return items
}
