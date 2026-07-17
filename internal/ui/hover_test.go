package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonNone,
		X:      x,
		Y:      y,
	}
}

// TestHoverLogHighlight verifies that moving the mouse over a log entry
// highlights it with the hover background color.
func TestHoverLogHighlight(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(motion(10, 5)) // entry 1
	m = m2.(Model)
	if !m.hover.valid || m.hover.logIdx != 1 {
		t.Fatalf("hover = %+v, want logIdx 1", m.hover)
	}

	view := ansi.Strip(m.View())
	// The hovered entry's subject should appear.
	if !strings.Contains(view, "second") {
		t.Fatal("hovered entry not rendered")
	}
}

// TestHoverLogIgnoredOnScrollbar verifies the scrollbar area does not get
// hover highlights.
func TestHoverLogIgnoredOnScrollbar(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(motion(m.width-1, 5))
	m = m2.(Model)
	if m.hover.valid {
		t.Fatal("scrollbar should clear hover")
	}
}

// TestHoverDiffBar verifies hovering a diff row highlights the left bar.
func TestHoverDiffBar(t *testing.T) {
	m := diffClickTestModel(t)
	// Hover over the first file header row.
	headLen := m.diffHeadLen()
	headerY := contentTopBarHeight + 1 + headLen
	m2, _ := m.Update(motion(10, headerY))
	m = m2.(Model)
	if !m.hover.valid || m.hover.diffRow != 0 {
		t.Fatalf("hover = %+v, want diffRow 0", m.hover)
	}
	// View should render without errors.
	_ = ansi.Strip(m.View())
}

// TestHoverPickerHighlight verifies moving the mouse over a picker row sets
// the picker hover target.
func TestHoverPickerHighlight(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m = step(t, m, fileListMsg{files: []string{"a/b.go", "main.go"}})

	m2, _ := m.Update(motion(10, 4)) // row 1: b.go
	m = m2.(Model)
	if !m.hover.valid || m.hover.pickerRow != 1 {
		t.Fatalf("hover = %+v, want pickerRow 1", m.hover)
	}
}

// TestHoverFzfHighlight verifies moving the mouse over an fzf result sets the
// fzf hover target.
func TestHoverFzfHighlight(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m = step(t, m, fileListMsg{files: []string{"alpha.go", "beta.go"}})
	fv := &m.fileView
	fv.fzfActive = true
	fv.fzfQuery = ""
	fv.fzfFilter()

	m2, _ := m.Update(motion(10, 5)) // first result
	m = m2.(Model)
	if !m.hover.valid || m.hover.fzfRow != 0 {
		t.Fatalf("hover = %+v, want fzfRow 0", m.hover)
	}
}

// TestHoverBlameLine verifies moving the mouse over a blame line sets the
// blame hover target.
func TestHoverBlameLine(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	lines := []jj.AnnotateLine{
		{LineNo: 1, ChangeID: "a", CommitID: "b", Text: "one"},
		{LineNo: 2, ChangeID: "a", CommitID: "b", Text: "two"},
	}
	m = step(t, m, fileAnnotateMsg{path: "f.go", lines: lines})

	m2, _ := m.Update(motion(10, 7)) // body line 1
	m = m2.(Model)
	if !m.hover.valid || m.hover.blameLine != 1 {
		t.Fatalf("hover = %+v, want blameLine 1", m.hover)
	}
}

// TestHoverHistoryEntry verifies moving the mouse over a history entry sets
// the history hover target.
func TestHoverHistoryEntry(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m.fileView.phase = fileHistory
	m = step(t, m, fileHistoryMsg{entries: []jj.LogEntry{
		{ChangeID: "h1", CommitID: "c1", Subject: "old"},
		{ChangeID: "h2", CommitID: "c2", Subject: "older"},
	}})

	m2, _ := m.Update(motion(10, 6)) // entry 1
	m = m2.(Model)
	if !m.hover.valid || m.hover.histIdx != 1 {
		t.Fatalf("hover = %+v, want histIdx 1", m.hover)
	}
}

// TestHoverClearedOnBlur verifies BlurMsg clears hover state.
func TestHoverClearedOnBlur(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(motion(10, 5))
	m = m2.(Model)
	if !m.hover.valid {
		t.Fatal("setup: hover should be valid")
	}
	m2, _ = m.Update(tea.BlurMsg{})
	m = m2.(Model)
	if m.hover.valid {
		t.Fatal("blur did not clear hover")
	}
}

// TestHoverContextMenuSuppressed verifies hover is not updated while the
// context menu is open.
func TestHoverContextMenuSuppressed(t *testing.T) {
	m := mouseTestModel()
	m2, _ := m.Update(rightClick(10, 5))
	m = m2.(Model)
	if !m.contextMenuOpen {
		t.Fatal("setup: menu should be open")
	}
	prev := m.hover.logIdx
	m2, _ = m.Update(motion(10, 7))
	m = m2.(Model)
	if m.hover.logIdx != prev {
		t.Fatal("hover updated while context menu was open")
	}
}

// TestHoverLogEdgeLine verifies that moving the mouse over a "~" elided edge
// line sets the edge hover target so it gets highlighted.
func TestHoverLogEdgeLine(t *testing.T) {
	m := mouseTestModel()
	m.entries = []jj.LogEntry{
		{ChangeID: "aaaa0000", CommitID: "c0ffee01", Subject: "first",
			EdgeLines: []string{"~  (elided revisions)"}},
		{ChangeID: "bbbb1111", CommitID: "c0ffee02", Subject: "second"},
	}

	// Entry 0: header Y=3, body Y=4, elided edge line Y=5.
	m2, _ := m.Update(motion(10, 5))
	m = m2.(Model)
	if !m.hover.valid || m.hover.logEdge != 0 {
		t.Fatalf("hover = %+v, want logEdge 0", m.hover)
	}
	if m.hover.logIdx != 0 {
		t.Fatalf("logIdx = %d, want 0", m.hover.logIdx)
	}

	// Moving off the edge line clears the edge hover; entry stays hovered.
	m2, _ = m.Update(motion(10, 4)) // body line
	m = m2.(Model)
	if m.hover.logEdge != -1 {
		t.Fatalf("logEdge = %d, want -1 after moving off edge line", m.hover.logEdge)
	}
	if m.hover.logIdx != 0 {
		t.Fatalf("logIdx = %d, want 0 after moving to body line", m.hover.logIdx)
	}
}
