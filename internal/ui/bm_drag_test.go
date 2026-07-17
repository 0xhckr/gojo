package ui

import (
	"strings"
	"testing"
	tea "github.com/charmbracelet/bubbletea"
	"gojo/internal/jj"
)

func bookmarkTestModel() Model {
	return Model{
		ready:  true,
		width:  100,
		height: 30,
		view:   viewLog,
		entries: []jj.LogEntry{
			{ChangeID: "aaaa0000", CommitID: "c0ffee01", Subject: "first", Bookmarks: []string{"main"}},
			{ChangeID: "bbbb1111", CommitID: "c0ffee02", Subject: "second"},
			{ChangeID: "cccc2222", CommitID: "c0ffee03", Subject: "third"},
		},
	}
}

func TestBookmarkDragPressStartsDrag(t *testing.T) {
	m := bookmarkTestModel()
	// Entry 0 header is at Y = contentTopBarHeight + 1 (padding) = 3.
	y := contentTopBarHeight + 1 // Y=3
	// Find the X of the "main" bookmark segment.
	name, idx, ok := m.bookmarkAtMouse(24, y)
	if !ok {
		t.Fatalf("bookmarkAtMouse(24, %d) = ok=false, want main", y)
	}
	if name != "main" || idx != 0 {
		t.Fatalf("bookmarkAtMouse = (%q, %d), want (main, 0)", name, idx)
	}

	// Press on the bookmark segment.
	m2, _ := m.Update(leftClick(24, y))
	m = m2.(Model)
	if m.bookmarkDrag == nil {
		t.Fatal("press on bookmark did not start drag")
	}
	if m.bookmarkDrag.name != "main" || m.bookmarkDrag.sourceIdx != 0 {
		t.Fatalf("drag = %+v, want name=main sourceIdx=0", m.bookmarkDrag)
	}
}

func TestBookmarkDragMotionUpdatesTarget(t *testing.T) {
	m := bookmarkTestModel()
	y0 := contentTopBarHeight + 1 // entry 0 header
	m2, _ := m.Update(leftClick(24, y0))
	m = m2.(Model)

	// Move to entry 1 header (Y=5).
	m2, _ = m.Update(motion(10, 5))
	m = m2.(Model)
	if m.bookmarkDrag == nil {
		t.Fatal("drag lost after motion")
	}
	if m.bookmarkDrag.targetIdx != 1 {
		t.Errorf("targetIdx = %d, want 1", m.bookmarkDrag.targetIdx)
	}
}

func TestBookmarkDragReleaseOnSameCommitNoOp(t *testing.T) {
	m := bookmarkTestModel()
	y0 := contentTopBarHeight + 1
	m2, _ := m.Update(leftClick(24, y0))
	m = m2.(Model)

	// Release on the same entry — no cmd, drag cleared.
	m2, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 10, Y: y0})
	m = m2.(Model)
	if m.bookmarkDrag != nil {
		t.Error("drag not cleared on release")
	}
	if cmd != nil {
		t.Error("release on same commit should not produce a cmd")
	}
}

func TestBookmarkDragReleaseOnDifferentCommitProducesCmd(t *testing.T) {
	m := bookmarkTestModel()
	m.runner = jj.NewRunner(jj.Config{})
	y0 := contentTopBarHeight + 1
	m2, _ := m.Update(leftClick(24, y0))
	m = m2.(Model)

	// Release on entry 1 (Y=5).
	m2, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 10, Y: 5})
	m = m2.(Model)
	if m.bookmarkDrag != nil {
		t.Error("drag not cleared on release")
	}
	if cmd == nil {
		t.Fatal("release on different commit should produce a cmd")
	}
}

// TestBookmarkDragRenderShowsDropMarker verifies the rendered log shows a
// drop marker on the target row during a drag.
func TestBookmarkDragRenderShowsDropMarker(t *testing.T) {
	m := bookmarkTestModel()
	y0 := contentTopBarHeight + 1
	m2, _ := m.Update(leftClick(24, y0))
	m = m2.(Model)
	m2, _ = m.Update(motion(10, 5)) // hover over entry 1
	m = m2.(Model)

	view := m.View()
	if !strings.Contains(view, "drop") {
		t.Fatal("drag view does not show drop marker")
	}
	if !strings.Contains(view, "dragging main") {
		t.Fatal("drag view does not show dragging indicator")
	}
}

// TestBookmarkDragPressOutsideBookmarkSelectsCommit verifies a press that
// doesn't land on a bookmark segment falls through to normal click handling.
func TestBookmarkDragPressOutsideBookmarkSelectsCommit(t *testing.T) {
	m := bookmarkTestModel()
	y0 := contentTopBarHeight + 1
	// X=5 is within the ChangeID, not a bookmark.
	m2, _ := m.Update(leftClick(5, y0))
	m = m2.(Model)
	if m.bookmarkDrag != nil {
		t.Fatal("press outside bookmark started a drag")
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (same row, no drag)", m.cursor)
	}
}
