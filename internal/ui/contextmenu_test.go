package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func rightClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonRight,
		X:      x,
		Y:      y,
	}
}

// TestContextMenuOpenOnRightClick verifies a right-click opens the menu and
// selects the commit under the cursor.
func TestContextMenuOpenOnRightClick(t *testing.T) {
	m := mouseTestModel()

	m2, cmd := m.Update(rightClick(10, 5)) // entry 1
	m = m2.(Model)
	if cmd != nil {
		t.Error("right-click should not produce a command")
	}
	if !m.contextMenuOpen {
		t.Fatal("right-click did not open the context menu")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	if len(m.contextMenuItems) == 0 {
		t.Fatal("context menu has no items")
	}
}

// TestContextMenuClosesOnEsc verifies esc dismisses the menu without running
// an action.
func TestContextMenuClosesOnEsc(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)
	if !m.contextMenuOpen {
		t.Fatal("menu did not open")
	}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = m2.(Model)
	if cmd != nil {
		t.Error("esc produced a command")
	}
	if m.contextMenuOpen {
		t.Fatal("esc did not close the menu")
	}
}

// TestContextMenuClosesOnClickOutside verifies a left-click outside the menu
// dismisses it.
func TestContextMenuClosesOnClickOutside(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)

	m2, cmd := m.Update(leftClick(80, 5)) // outside the menu
	m = m2.(Model)
	if cmd != nil {
		t.Error("outside click produced a command")
	}
	if m.contextMenuOpen {
		t.Fatal("outside click did not close the menu")
	}
}

// TestContextMenuEnterActivates verifies pressing enter on a menu item runs
// the selected action.
func TestContextMenuEnterActivates(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)

	// First item with a selected commit is "open diff".
	if m.contextMenuItems[0].label != "open diff" {
		t.Fatalf("first item = %q, want open diff", m.contextMenuItems[0].label)
	}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.contextMenuOpen {
		t.Fatal("menu stayed open after activation")
	}
	if !m.diffOpen {
		t.Fatal("enter on 'open diff' did not open the diff panel")
	}
	if cmd == nil {
		t.Error("activation did not produce a load command")
	}
}

// TestContextMenuMouseClickItem verifies that clicking a menu item via mouse
// activates it, accounting for the border offset (items start one row below
// the top border).
func TestContextMenuMouseClickItem(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)

	// The first item is at contextMenuY + 1 (below the top border).
	itemY := m.contextMenuY + 1
	m2, cmd := m.Update(leftClick(m.contextMenuX+1, itemY))
	m = m2.(Model)
	if m.contextMenuOpen {
		t.Fatal("menu stayed open after mouse activation")
	}
	if !m.diffOpen {
		t.Fatal("clicking first menu item did not open the diff panel")
	}
	if cmd == nil {
		t.Error("mouse activation did not produce a load command")
	}
}

// TestContextMenuDownNavigation verifies the down arrow moves the cursor
// through the menu.
func TestContextMenuDownNavigation(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = m2.(Model)
	if m.contextMenuCursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.contextMenuCursor)
	}
}

// TestContextMenuInDiffView verifies the diff view shows diff-specific
// actions.
func TestContextMenuInDiffView(t *testing.T) {
	m := diffClickTestModel(t)

	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)
	if !m.contextMenuOpen {
		t.Fatal("menu did not open in diff view")
	}
	found := false
	for _, it := range m.contextMenuItems {
		if strings.Contains(it.label, "close diff") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("diff menu missing close diff; items=%v", labels(m.contextMenuItems))
	}
}

// TestContextMenuInRebaseMode verifies rebase mode shows rebase-specific
// actions and moves the destination on right-click.
func TestContextMenuInRebaseMode(t *testing.T) {
	m := mouseTestModel()
	m.rebaseMode = true
	m.rebaseSource = 0
	m.rebaseDest = 1

	m2, _ := m.Update(rightClick(10, 7)) // entry 2
	m = m2.(Model)
	if !m.contextMenuOpen {
		t.Fatal("menu did not open in rebase mode")
	}
	if m.rebaseDest != 2 {
		t.Fatalf("rebaseDest = %d, want 2", m.rebaseDest)
	}
	found := false
	for _, it := range m.contextMenuItems {
		if strings.Contains(it.label, "confirm rebase") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("rebase menu missing confirm; items=%v", labels(m.contextMenuItems))
	}
}

// TestContextMenuInSplitMode verifies split mode shows split-specific actions
// and toggles a mark on activation.
func TestContextMenuInSplitMode(t *testing.T) {
	m := diffClickTestModel(t)
	m.splitMode = true
	m.splitMarked = map[int]bool{}

	// Move cursor to the first addition line so the menu applies to it.
	addIdx := -1
	for i, r := range m.diffRows {
		if r.kind == rowLine && r.lineKind == "addition" {
			addIdx = i
			break
		}
	}
	if addIdx < 0 {
		t.Fatal("no addition row")
	}
	m.setDiffCursorToRow(addIdx)

	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)
	if !m.contextMenuOpen {
		t.Fatal("menu did not open in split mode")
	}

	// Activate "toggle mark" (first item).
	if m.contextMenuItems[0].label != "toggle mark" {
		t.Fatalf("first item = %q, want toggle mark", m.contextMenuItems[0].label)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if !m.splitMarked[addIdx] {
		t.Errorf("toggle mark did not mark row %d", addIdx)
	}
}

// TestContextMenuInFzf verifies the picker fuzzy finder shows fzf-specific
// menu items and opens the selected file on activation.
func TestContextMenuInFzf(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m = step(t, m, fileListMsg{files: []string{"alpha.go", "beta.go"}})
	fv := &m.fileView
	fv.fzfActive = true
	fv.fzfQuery = ""
	fv.fzfFilter()

	m2, _ := m.Update(rightClick(10, 5)) // first result
	m = m2.(Model)
	if !m.contextMenuOpen {
		t.Fatal("menu did not open in fzf")
	}
	if fv.fzfCursor != 0 {
		t.Fatalf("fzfCursor = %d, want 0", fv.fzfCursor)
	}
	if m.contextMenuItems[0].label != "open file" {
		t.Fatalf("first item = %q, want open file", m.contextMenuItems[0].label)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.contextMenuOpen {
		t.Fatal("menu stayed open after activation")
	}
	if fv.fzfActive {
		t.Fatal("fzf stayed active after open")
	}
	if len(m.busy) == 0 {
		t.Error("open file did not start annotating")
	}
}

// TestContextMenuModalIgnored verifies right-click is ignored while a modal
// input mode is active.
func TestContextMenuModalIgnored(t *testing.T) {
	m := mouseTestModel()
	m.bookmarkMode = true

	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)
	if m.contextMenuOpen {
		t.Fatal("right-click opened menu during bookmark mode")
	}
}

func labels(items []contextMenuItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.label
	}
	return out
}
