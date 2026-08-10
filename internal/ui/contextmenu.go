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
	// Conflict view: the conflict block under the mouse (-1 = none) and which
	// pane (left = true) the pointer is over; conflict lines underline on
	// hover to signal click-to-pick.
	conflictBlock int
	conflictLeft  bool
	refName       string // bookmark/tag name under the mouse, or ""
	refKind       string // "bookmark" | "tag", or ""
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
	switch m.keys.resolve(ctxMenu, k) {
	case actClose:
		m.closeContextMenu()
		return m, nil
	case actAccept:
		return m.activateContextMenuItem()
	case actUp:
		if m.contextMenuCursor > 0 {
			m.contextMenuCursor--
		}
		return m, nil
	case actDown:
		if m.contextMenuCursor < len(m.contextMenuItems)-1 {
			m.contextMenuCursor++
		}
		return m, nil
	case actTop:
		m.contextMenuCursor = 0
		return m, nil
	case actBottom:
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

// logKeyItem builds a menu item that synthesizes the log-view key currently
// bound to action, showing the primary binding as the hint.
func (m Model) logKeyItem(label, action string) contextMenuItem {
	msg, k := m.keyMsg(ctxLog, action)
	return menuItem(label, displayKey(k), func(m Model) (tea.Model, tea.Cmd) {
		return m.handleLogKey(msg, k)
	})
}

// diffKeyItem builds a menu item that synthesizes the diff-view key currently
// bound to action.
func (m Model) diffKeyItem(label, action string) contextMenuItem {
	k := m.keys.primary(ctxDiff, action)
	return menuItem(label, displayKey(k), func(m Model) (tea.Model, tea.Cmd) {
		return m.handleDiffKey(k)
	})
}

func (m Model) logContextMenuItems() []contextMenuItem {
	var items []contextMenuItem
	hasSel := m.selectedEntry() != nil

	if hasSel {
		items = append(items, m.logKeyItem("open diff", actOpen))
	}
	if hasSel {
		items = append(items, m.logKeyItem("describe", actDescribe))
	}
	if hasSel {
		items = append(items, m.logKeyItem("AI describe", actAIDescribe))
	}
	if hasSel {
		items = append(items, m.logKeyItem("edit", actEdit))
	}
	items = append(items, m.logKeyItem("new change", actNew))
	if hasSel {
		items = append(items, m.logKeyItem("abandon", actAbandon))
	}
	if hasSel {
		items = append(items, m.logKeyItem("absorb", actAbsorb))
	}
	items = append(items, m.logKeyItem("rebase", actRebase))
	items = append(items, m.logKeyItem("squash", actSquash))
	items = append(items, m.logKeyItem("bookmark", actBookmark))
	items = append(items, m.logKeyItem("tag", actTag))
	items = append(items, m.logKeyItem("git", actGit))
	items = append(items, m.logKeyItem("file view", actFiles))
	items = append(items, m.logKeyItem("toggle all revisions", actAllRev))
	items = append(items, m.logKeyItem("undo", actUndo))
	items = append(items, m.logKeyItem("redo", actRedo))
	return items
}

// modeKeyItem builds a menu item that runs handler with the key currently
// bound to action in ctx, showing the primary binding as the hint.
func (m Model) modeKeyItem(label, ctx, action string, handler func(Model, string) (tea.Model, tea.Cmd)) contextMenuItem {
	k := m.keys.primary(ctx, action)
	return menuItem(label, displayKey(k), func(m Model) (tea.Model, tea.Cmd) {
		return handler(m, k)
	})
}

func (m Model) rebaseContextMenuItems() []contextMenuItem {
	rb := func(label, action string) contextMenuItem {
		return m.modeKeyItem(label, ctxRebase, action, func(mm Model, k string) (tea.Model, tea.Cmd) { return mm.handleRebaseKey(k) })
	}
	return []contextMenuItem{
		rb("confirm rebase", actConfirm),
		rb("toggle scope", actScope),
		rb("cycle placement", actPlace),
		rb("cancel", actCancel),
	}
}

func (m Model) squashContextMenuItems() []contextMenuItem {
	sq := func(label, action string) contextMenuItem {
		return m.modeKeyItem(label, ctxSquash, action, func(mm Model, k string) (tea.Model, tea.Cmd) { return mm.handleSquashKey(k) })
	}
	return []contextMenuItem{
		sq("confirm squash", actConfirm),
		sq("cancel", actCancel),
	}
}

func (m Model) diffContextMenuItems() []contextMenuItem {
	var items []contextMenuItem
	items = append(items, m.diffKeyItem("close diff", actClose))
	if m.diffIsRevision && m.diffRev != "" {
		items = append(items, m.diffKeyItem("describe", actDescribe))
		items = append(items, m.diffKeyItem("AI describe", actAIDescribe))
		items = append(items, m.diffKeyItem("new change", actNew))
		items = append(items, m.diffKeyItem("split", actSplit))
		items = append(items, m.diffKeyItem("absorb", actAbsorb))
	}
	if fileIdx, ok := m.cursorFileHeader(); ok {
		path := m.diffRows[fileIdx].path
		collapsed := m.diffCollapsed != nil && m.diffCollapsed[path]
		if collapsed {
			items = append(items, m.diffKeyItem("expand file", actExpand))
		} else {
			items = append(items, m.diffKeyItem("collapse file", actCollapse))
		}
	}
	return items
}

func (m Model) splitContextMenuItems() []contextMenuItem {
	sp := func(label, action string) contextMenuItem {
		return m.modeKeyItem(label, ctxSplit, action, func(mm Model, k string) (tea.Model, tea.Cmd) { return mm.handleSplitKey(k) })
	}
	return []contextMenuItem{
		sp("toggle mark", actToggle),
		sp("confirm split", actConfirm),
		sp("cancel", actCancel),
	}
}

// msgKeyItem builds a menu item that runs handler with a synthesized KeyMsg
// for the key currently bound to action in ctx.
func (m Model) msgKeyItem(label, ctx, action string, handler func(Model, tea.KeyMsg, string) (tea.Model, tea.Cmd)) contextMenuItem {
	msg, k := m.keyMsg(ctx, action)
	return menuItem(label, displayKey(k), func(m Model) (tea.Model, tea.Cmd) {
		return handler(m, msg, k)
	})
}

func (m Model) pickerContextMenuItems() []contextMenuItem {
	fv := &m.fileView
	if fv.fzfActive {
		var items []contextMenuItem
		if fv.fzfCursor >= 0 && fv.fzfCursor < len(fv.fzfResults) {
			items = append(items, m.msgKeyItem("open file", ctxFzf, actAccept,
				func(mm Model, msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) { return mm.handleFzfKey(msg, k) }))
		}
		items = append(items, m.msgKeyItem("close finder", ctxFzf, actCancel,
			func(mm Model, msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) { return mm.handleFzfKey(msg, k) }))
		return items
	}
	var items []contextMenuItem
	if row := fv.curRow(); row != nil {
		if row.node.isDir {
			items = append(items, m.msgKeyItem("expand/collapse", ctxPicker, actOpen,
				func(mm Model, msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) { return mm.handleFilePickerKey(msg, k) }))
		} else {
			items = append(items, m.msgKeyItem("open file", ctxPicker, actOpen,
				func(mm Model, msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) { return mm.handleFilePickerKey(msg, k) }))
		}
	}
	items = append(items, m.msgKeyItem("leave file view", ctxPicker, actQuit,
		func(mm Model, msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) { return mm.handleFilePickerKey(msg, k) }))
	return items
}

func (m Model) blameContextMenuItems() []contextMenuItem {
	bl := func(label, action string) contextMenuItem {
		return m.modeKeyItem(label, ctxBlame, action, func(mm Model, k string) (tea.Model, tea.Cmd) { return mm.handleFileBlameKey(k) })
	}
	return []contextMenuItem{
		bl("open commit", actOpen),
		bl("file history", actHistory),
		bl("back", actBack),
	}
}

func (m Model) historyContextMenuItems() []contextMenuItem {
	hi := func(label, action string) contextMenuItem {
		return m.modeKeyItem(label, ctxHist, action, func(mm Model, k string) (tea.Model, tea.Cmd) { return mm.handleFileHistoryKey(k) })
	}
	return []contextMenuItem{
		hi("open commit", actOpen),
		hi("back", actBack),
	}
}

func (m Model) helpContextMenuItems() []contextMenuItem {
	// Help closes via the global quit/help keys (handled in handleKey), not
	// handleHelpKey.
	k := m.hk(ctxGlobal, actQuit)
	return []contextMenuItem{
		menuItem("close help", k, func(m Model) (tea.Model, tea.Cmd) {
			m.view = viewLog
			return m, nil
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
