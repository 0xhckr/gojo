package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"gojo/internal/jj"
)

// mouseTestModel builds a ready log-view model with three synthetic commits.
func mouseTestModel() Model {
	return Model{
		ready:  true,
		width:  100,
		height: 30,
		view:   viewLog,
		entries: []jj.LogEntry{
			{ChangeID: "aaaa0000", CommitID: "c0ffee01", Subject: "first"},
			{ChangeID: "bbbb1111", CommitID: "c0ffee02", Subject: "second"},
			{ChangeID: "cccc2222", CommitID: "c0ffee03", Subject: "third"},
		},
	}
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      x,
		Y:      y,
	}
}

// TestMouseLogClickSelectsCommit verifies that clicking a commit row moves
// the cursor to it, and clicking the already-selected row opens its diff.
func TestMouseLogClickSelectsCommit(t *testing.T) {
	m := mouseTestModel()

	// Terminal Y: top bar (2) + padding row (1) + entry0 (2 rows) → entry1
	// starts at Y=5.
	m2, cmd := m.Update(leftClick(10, 5))
	m = m2.(Model)
	if cmd != nil {
		t.Error("selecting a commit should not produce a command")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	// Clicking the selected commit again opens its diff.
	m2, cmd = m.Update(leftClick(10, 5))
	m = m2.(Model)
	if !m.diffOpen {
		t.Fatal("re-click on selected commit did not open the diff")
	}
	if m.diffRev != "bbbb1111" {
		t.Errorf("diffRev = %q, want bbbb1111", m.diffRev)
	}
	if cmd == nil {
		t.Error("opening a diff should produce a load command")
	}
}

// TestMouseLogClickPadding verifies clicks on empty rows below the log and on
// the padding row are ignored.
func TestMouseLogClickPadding(t *testing.T) {
	m := mouseTestModel()

	m2, _ := m.Update(leftClick(10, 2)) // list padding row
	m = m2.(Model)
	if m.cursor != 0 {
		t.Errorf("click on padding moved cursor to %d", m.cursor)
	}

	m2, _ = m.Update(leftClick(10, 20)) // below the last entry
	m = m2.(Model)
	if m.cursor != 0 {
		t.Errorf("click below entries moved cursor to %d", m.cursor)
	}
}

// TestMouseLogClickRebaseMovesDest verifies that in rebase mode a click moves
// the destination indicator rather than the cursor, and never opens a diff.
func TestMouseLogClickRebaseMovesDest(t *testing.T) {
	m := mouseTestModel()
	m.rebaseMode = true
	m.rebaseSource = 0
	m.rebaseDest = 1

	// Entry 2 starts at Y=7 (top bar 2 + padding 1 + two 2-row entries).
	m2, cmd := m.Update(leftClick(10, 7))
	m = m2.(Model)
	if cmd != nil {
		t.Error("rebase click should not produce a command")
	}
	if m.rebaseDest != 2 {
		t.Errorf("rebaseDest = %d, want 2", m.rebaseDest)
	}
	if m.diffOpen {
		t.Error("rebase click should never open a diff")
	}
}

// TestMouseClickScrollbarIgnored verifies clicks inside the scrollbar area
// are not treated as row selection (they start a scrollbar drag instead).
func TestMouseClickScrollbarIgnored(t *testing.T) {
	m := mouseTestModel()

	m2, _ := m.Update(leftClick(m.width-1, 5))
	m = m2.(Model)
	if m.cursor != 0 {
		t.Errorf("scrollbar-area click moved cursor to %d", m.cursor)
	}
}

// TestMouseClickIgnoredInModes verifies clicks are swallowed while a modal
// input mode (e.g. bookmark prompt) is active.
func TestMouseClickIgnoredInModes(t *testing.T) {
	m := mouseTestModel()
	m.bookmarkMode = true

	m2, _ := m.Update(leftClick(10, 5))
	m = m2.(Model)
	if m.cursor != 0 {
		t.Errorf("click during bookmark mode moved cursor to %d", m.cursor)
	}
}

// TestMousePickerClick verifies click-to-select in the file picker, and that
// re-clicking a directory toggles expansion while re-clicking a file opens it.
func TestMousePickerClick(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m = step(t, m, fileListMsg{files: []string{"a/b.go", "a/c.go", "main.go"}})
	// Rows: 0 dir a/ (expanded), 1 b.go, 2 c.go, 3 main.go.
	if len(m.fileView.rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(m.fileView.rows))
	}

	// Row i renders at terminal Y = 3+i (top bar 2 + picker title 1).
	m2, _ := m.Update(leftClick(10, 5)) // row 2: c.go
	m = m2.(Model)
	if m.fileView.cursor != 2 {
		t.Fatalf("picker cursor = %d, want 2", m.fileView.cursor)
	}

	// Re-clicking the file opens the blame view (busy label pushed).
	m2, _ = m.Update(leftClick(10, 5))
	m = m2.(Model)
	if len(m.busy) == 0 {
		t.Error("re-click on selected file did not start annotating")
	}

	// Click the directory row, then re-click to collapse it.
	m2, _ = m.Update(leftClick(10, 3)) // row 0: dir a/
	m = m2.(Model)
	if m.fileView.cursor != 0 {
		t.Fatalf("picker cursor = %d, want 0", m.fileView.cursor)
	}
	m2, _ = m.Update(leftClick(10, 3))
	m = m2.(Model)
	if len(m.fileView.rows) != 2 { // a/ collapsed + main.go
		t.Errorf("rows after collapse = %d, want 2", len(m.fileView.rows))
	}
}

// TestMouseFzfClick verifies click-to-select and re-click-to-open in the
// picker's fuzzy finder overlay.
func TestMouseFzfClick(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m = step(t, m, fileListMsg{files: []string{"alpha.go", "beta.go"}})
	fv := &m.fileView
	fv.fzfActive = true
	fv.fzfQuery = ""
	fv.fzfFilter()

	// Results render at terminal Y = 5+i (top bar 2 + title 1 + prompt 1 +
	// divider 1).
	m2, _ := m.Update(leftClick(10, 6)) // second result
	m = m2.(Model)
	if m.fileView.fzfCursor != 1 {
		t.Fatalf("fzf cursor = %d, want 1", m.fileView.fzfCursor)
	}

	m2, _ = m.Update(leftClick(10, 6)) // re-click: open
	m = m2.(Model)
	if m.fileView.fzfActive {
		t.Error("re-click on selected fzf result did not close the finder")
	}
	if len(m.busy) == 0 {
		t.Error("re-click on selected fzf result did not start annotating")
	}
}

// diffClickTestModel builds a model with an open revision diff loaded from
// sampleDiff (foo.go modified + new.txt added).
func diffClickTestModel(t *testing.T) Model {
	t.Helper()
	m := mouseTestModel()
	m.diffOpen = true
	m.diffRev = "aaaa0000"
	m.diffIsRevision = true
	m.diffLoading = true
	m = step(t, m, diffLoadedMsg{rev: "aaaa0000", desc: "first", rows: renderDiff(sampleDiff)})
	if len(m.diffRows) == 0 {
		t.Fatal("no diff rows loaded")
	}
	return m
}

// TestMouseDiffClickFileHeader verifies clicking a diff file header toggles
// its collapsed state, and clicking an addition line moves the chunk cursor.
func TestMouseDiffClickFileHeader(t *testing.T) {
	m := diffClickTestModel(t)

	// Row 0 is the foo.go file header. Its terminal Y: top bar (2) + diff
	// title (1) + head rows.
	headLen := m.diffHeadLen()
	headerY := contentTopBarHeight + 1 + headLen

	m2, _ := m.Update(leftClick(10, headerY))
	m = m2.(Model)
	if !m.diffCollapsed["foo.go"] {
		t.Fatal("click on file header did not collapse foo.go")
	}

	// Expand it again, then click the first addition line.
	m2, _ = m.Update(leftClick(10, headerY))
	m = m2.(Model)
	if m.diffCollapsed["foo.go"] {
		t.Fatal("second click on file header did not expand foo.go")
	}

	addIdx := -1
	for i, r := range m.diffRows {
		if r.kind == rowLine && r.lineKind == "addition" {
			addIdx = i
			break
		}
	}
	if addIdx < 0 {
		t.Fatal("no addition row in sample diff")
	}
	m2, _ = m.Update(leftClick(10, headerY+addIdx))
	m = m2.(Model)
	want := m.diffHeadLen() + addIdx
	if m.diffChunks[m.diffCurChunk][m.diffCurLine] != want {
		t.Errorf("chunk cursor = chunk %d line %d (row %d), want body row %d",
			m.diffCurChunk, m.diffCurLine, m.diffChunks[m.diffCurChunk][m.diffCurLine], want)
	}
}

// TestMouseSplitClickTogglesMark verifies that in split mode clicking an
// addition/deletion line toggles its mark and clicking a file header toggles
// the whole file.
func TestMouseSplitClickTogglesMark(t *testing.T) {
	m := diffClickTestModel(t)
	m.splitMode = true
	m.splitMarked = map[int]bool{}

	headLen := m.diffHeadLen()
	headerY := contentTopBarHeight + 1 + headLen

	addIdx := -1
	for i, r := range m.diffRows {
		if r.kind == rowLine && r.lineKind == "addition" {
			addIdx = i
			break
		}
	}
	if addIdx < 0 {
		t.Fatal("no addition row in sample diff")
	}

	// Click the addition line: marked.
	m2, _ := m.Update(leftClick(10, headerY+addIdx))
	m = m2.(Model)
	if !m.splitMarked[addIdx] {
		t.Errorf("click on addition line did not mark row %d", addIdx)
	}

	// Click it again: unmarked.
	m2, _ = m.Update(leftClick(10, headerY+addIdx))
	m = m2.(Model)
	if m.splitMarked[addIdx] {
		t.Errorf("second click on addition line did not unmark row %d", addIdx)
	}

	// Click the new.txt file header: every add/del line in that file marks.
	newIdx := -1
	for i, r := range m.diffRows {
		if r.kind == rowFileHeader && r.path == "new.txt" {
			newIdx = i
			break
		}
	}
	if newIdx < 0 {
		t.Fatal("new.txt header not found")
	}
	m2, _ = m.Update(leftClick(10, headerY+newIdx))
	m = m2.(Model)
	for i := newIdx + 1; i < len(m.diffRows); i++ {
		r := m.diffRows[i]
		if r.kind == rowLine && (r.lineKind == "addition" || r.lineKind == "deletion") {
			if !m.splitMarked[i] {
				t.Errorf("row %d not marked after file-header click", i)
			}
		}
	}
	// The other file's rows stay untouched.
	if m.splitMarked[addIdx] {
		t.Error("file-header click leaked a mark into another file")
	}
}

// TestMouseHistoryClick verifies click-to-select and re-click-to-open in the
// file-history view.
func TestMouseHistoryClick(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m.fileView.phase = fileHistory
	m = step(t, m, fileHistoryMsg{entries: []jj.LogEntry{
		{ChangeID: "hist0001", CommitID: "beef0001", Subject: "old"},
		{ChangeID: "hist0002", CommitID: "beef0002", Subject: "older"},
	}})

	// History layout: top bar 2 + history title 1 + log padding 1 → entry 0
	// at Y=4, entry 1 at Y=6.
	m2, _ := m.Update(leftClick(10, 6))
	m = m2.(Model)
	if m.fileView.histCur != 1 {
		t.Fatalf("histCur = %d, want 1", m.fileView.histCur)
	}

	m2, cmd := m.Update(leftClick(10, 6))
	m = m2.(Model)
	if !m.diffOpen {
		t.Fatal("re-click on selected history entry did not open the diff")
	}
	if m.diffRev != "hist0002" {
		t.Errorf("diffRev = %q, want hist0002", m.diffRev)
	}
	if cmd == nil {
		t.Error("opening a diff should produce a load command")
	}
}

// TestMouseBlameClick verifies click-to-move-cursor and re-click-to-open in
// the blame view.
func TestMouseBlameClick(t *testing.T) {
	m := mouseTestModel()
	m.view = viewFile
	m.fileView.phase = filePicker
	lines := []jj.AnnotateLine{
		{LineNo: 1, ChangeID: "blame001", CommitID: "deaf001", Text: "one"},
		{LineNo: 2, ChangeID: "blame001", CommitID: "deaf001", Text: "two"},
		{LineNo: 3, ChangeID: "blame002", CommitID: "deaf002", Text: "three"},
		{LineNo: 4, ChangeID: "blame002", CommitID: "deaf002", Text: "four"},
	}
	m = step(t, m, fileAnnotateMsg{path: "f.go", lines: lines})
	if m.fileView.phase != fileBlame {
		t.Fatalf("phase = %v, want fileBlame", m.fileView.phase)
	}

	// Blame layout: top bar 2 + title 1 + sticky head 3 → body starts Y=6.
	m2, _ := m.Update(leftClick(10, 8)) // body line 2 → source line index 2
	m = m2.(Model)
	if m.fileView.cursorY != 2 {
		t.Fatalf("cursorY = %d, want 2", m.fileView.cursorY)
	}

	m2, cmd := m.Update(leftClick(10, 8))
	m = m2.(Model)
	if !m.diffOpen {
		t.Fatal("re-click on focused blame line did not open the diff")
	}
	if m.diffRev != "blame002" {
		t.Errorf("diffRev = %q, want blame002", m.diffRev)
	}
	if cmd == nil {
		t.Error("opening a diff should produce a load command")
	}
}
