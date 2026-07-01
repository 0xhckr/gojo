package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// setupSplitModel builds a model with a diff panel open on sampleDiff, ready
// for split-mode tests.
func setupSplitModel() Model {
	rows := renderDiff(sampleDiff)
	m := Model{
		width:          80,
		height:         30,
		view:           viewLog,
		diffOpen:       true,
		diffIsRevision: true,
		diffRev:        "abc",
		diffRows:       rows,
		diffDigits:     maxLineDigits(rows),
		diffCollapsed:  nil,
		ready:          true,
	}
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	m.computeDiffLayout()
	return m
}

// TestSplitEnterMode verifies that pressing `s` in the diff panel enters split
// mode with an empty marked set.
func TestSplitEnterMode(t *testing.T) {
	m := setupSplitModel()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("s should not produce a command")
	}
	if !m.splitMode {
		t.Fatal("split mode not active after s")
	}
	if m.splitMarked == nil {
		t.Fatal("splitMarked is nil after entering split mode")
	}
	if len(m.splitMarked) != 0 {
		t.Errorf("splitMarked has %d entries, want 0 (empty)", len(m.splitMarked))
	}
}

// TestSplitCancel verifies that q and esc exit split mode without running jj.
func TestSplitCancel(t *testing.T) {
	for _, key := range []string{"q", "esc"} {
		m := setupSplitModel()
		m.splitMode = true
		m.splitMarked = map[int]bool{}

		var next tea.Model
		if key == "esc" {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
		} else {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
		m = next.(Model)
		if m.splitMode {
			t.Errorf("%s did not exit split mode", key)
		}
		if m.splitMarked != nil {
			t.Errorf("%s did not clear splitMarked", key)
		}
	}
}

// TestSplitToggleLine verifies that space toggles a single addition/deletion
// line at the cursor.
func TestSplitToggleLine(t *testing.T) {
	m := setupSplitModel()
	m.splitMode = true
	m.splitMarked = map[int]bool{}

	// Navigate to the first change chunk (chunk 1 = foo.go changes).
	m.diffCurChunk = 1
	m.diffCurLine = 0

	// Get the row index at the cursor.
	headLen := m.diffHeadLen()
	rowIdx := m.diffChunks[1][0] - headLen
	r := m.diffRows[rowIdx]
	if r.kind != rowLine || (r.lineKind != "addition" && r.lineKind != "deletion") {
		t.Fatalf("cursor row %d is not an addition/deletion line: %+v", rowIdx, r)
	}

	// Toggle: should mark the line.
	m.splitToggle()
	if !m.splitMarked[rowIdx] {
		t.Error("space did not mark the line")
	}

	// Toggle again: should unmark.
	m.splitToggle()
	if m.splitMarked[rowIdx] {
		t.Error("space did not unmark the line")
	}
}

// TestSplitToggleFile verifies that space on a file header toggles all
// addition/deletion lines in that file.
func TestSplitToggleFile(t *testing.T) {
	m := setupSplitModel()
	m.splitMode = true
	m.splitMarked = map[int]bool{}

	// Put cursor on the first file header (chunk 0).
	m.diffCurChunk = 0
	m.diffCurLine = 0

	headLen := m.diffHeadLen()
	headerRowIdx := m.diffChunks[0][0] - headLen
	r := m.diffRows[headerRowIdx]
	if r.kind != rowFileHeader {
		t.Fatalf("cursor row is not a file header: %+v", r)
	}

	// Find all addition/deletion rows in this file.
	var changeRows []int
	end := len(m.diffRows)
	for j := headerRowIdx + 1; j < len(m.diffRows); j++ {
		if m.diffRows[j].kind == rowFileHeader {
			end = j
			break
		}
	}
	for j := headerRowIdx + 1; j < end; j++ {
		if m.diffRows[j].kind == rowLine && (m.diffRows[j].lineKind == "addition" || m.diffRows[j].lineKind == "deletion") {
			changeRows = append(changeRows, j)
		}
	}
	if len(changeRows) == 0 {
		t.Fatal("no addition/deletion lines in first file")
	}

	// Toggle: should mark all lines in the file.
	m.splitToggle()
	for _, idx := range changeRows {
		if !m.splitMarked[idx] {
			t.Errorf("file toggle did not mark row %d", idx)
		}
	}

	// Toggle again: should unmark all.
	m.splitToggle()
	for _, idx := range changeRows {
		if m.splitMarked[idx] {
			t.Errorf("file toggle did not unmark row %d", idx)
		}
	}
}

// TestSplitIndicatorForRow verifies the indicator glyphs for different row
// types and selection states.
func TestSplitIndicatorForRow(t *testing.T) {
	rows := renderDiff(sampleDiff)

	// File header with no lines marked → "[ ]"
	sv := splitView{active: true, marked: map[int]bool{}}
	ind := splitIndicatorForRow(rows, 0, sv)
	if ind != "[ ]" {
		t.Errorf("unmarked file indicator = %q, want [ ]", ind)
	}

	// Mark all lines in the first file → "✓"
	sv.marked = map[int]bool{}
	for i, r := range rows {
		if r.kind == rowLine && (r.lineKind == "addition" || r.lineKind == "deletion") {
			sv.marked[i] = true
			// Stop at the second file header.
			if i > 0 && rows[i-1].kind == rowHunkHeader {
				break
			}
		}
	}
	// Actually, let me mark only the first file's lines.
	sv.marked = map[int]bool{}
	end := len(rows)
	for j := 1; j < len(rows); j++ {
		if rows[j].kind == rowFileHeader {
			end = j
			break
		}
	}
	for j := 1; j < end; j++ {
		if rows[j].kind == rowLine && (rows[j].lineKind == "addition" || rows[j].lineKind == "deletion") {
			sv.marked[j] = true
		}
	}
	ind = splitIndicatorForRow(rows, 0, sv)
	if ind != "[x]" {
		t.Errorf("all-marked file indicator = %q, want [x]", ind)
	}

	// Partial: mark only the first addition/deletion line.
	sv.marked = map[int]bool{}
	for j := 1; j < end; j++ {
		if rows[j].kind == rowLine && (rows[j].lineKind == "addition" || rows[j].lineKind == "deletion") {
			sv.marked[j] = true
			break
		}
	}
	ind = splitIndicatorForRow(rows, 0, sv)
	if ind != "[~]" {
		t.Errorf("partial file indicator = %q, want [~]", ind)
	}

	// Inactive split mode → ""
	sv2 := splitView{active: false}
	ind = splitIndicatorForRow(rows, 0, sv2)
	if ind != "" {
		t.Errorf("inactive indicator = %q, want empty", ind)
	}

	// Context line → ""
	for i, r := range rows {
		if r.kind == rowLine && r.lineKind == "context" {
			ind = splitIndicatorForRow(rows, i, splitView{active: true, marked: map[int]bool{}})
			if ind != "" {
				t.Errorf("context line indicator = %q, want empty", ind)
			}
			break
		}
	}
}

// TestSplitPanelRendering verifies that split indicators appear in the rendered
// diff panel.
func TestSplitPanelRendering(t *testing.T) {
	rows := renderDiff(sampleDiff)
	digits := maxLineDigits(rows)

	// Render without split mode — no indicators.
	out := renderDiffPanel(80, 24, "abcd", 0, false, false, 0, "", false, rows, digits, nil, "", 0, -1, nil, nil, splitView{}, false, nil, nil)
	plain := stripANSI(out)
	if strings.Contains(plain, "[x]") || strings.Contains(plain, "[~]") || strings.Contains(plain, "[ ]") {
		t.Error("split indicators found in non-split-mode render")
	}

	// Render with split mode active — indicators should appear.
	sv := splitView{active: true, marked: map[int]bool{}}
	out = renderDiffPanel(80, 24, "abcd", 0, false, false, 0, "", false, rows, digits, nil, "", 0, -1, nil, nil, sv, false, nil, nil)
	if len(out) != 24 {
		t.Errorf("split panel lines = %d, want 24", len(out))
	}
	plain = stripANSI(out)
	if !strings.Contains(plain, "[ ]") {
		t.Error("unmarked indicator ([ ]) not found in split-mode render")
	}
}

// TestComputeIntermediateFile verifies that the intermediate file content is
// correctly reconstructed from the diff and marked set.
func TestComputeIntermediateFile(t *testing.T) {
	// Simple diff: modify one line in a 3-line file.
	raw := `diff --git a/test.txt b/test.txt
--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,3 @@
 line1
-old line2
+new line2
 line3
`
	rows := renderDiff(raw)

	// Find the file header, deletion, and addition rows.
	var headerIdx, delIdx, addIdx int = -1, -1, -1
	for i, r := range rows {
		if r.kind == rowFileHeader {
			headerIdx = i
		}
		if r.kind == rowLine && r.lineKind == "deletion" {
			delIdx = i
		}
		if r.kind == rowLine && r.lineKind == "addition" {
			addIdx = i
		}
	}
	if headerIdx < 0 || delIdx < 0 || addIdx < 0 {
		t.Fatalf("missing rows: header=%d del=%d add=%d", headerIdx, delIdx, addIdx)
	}

	// Parent content (the old version).
	parentContent := "line1\nold line2\nline3\n"

	// Case 1: nothing marked → intermediate = parent (all changes go to
	// preceding revision, which means the preceding revision has the parent
	// content... wait, that's wrong. If nothing is marked, the unmarked
	// changes (both deletion and addition) are applied to the intermediate.
	// So intermediate = parent with old line2 replaced by new line2 = the
	// current content.
	// Actually: unmarked deletion → skip (removed in intermediate)
	//           unmarked addition → output (added in intermediate)
	// So: line1 (context), skip old line2 (deletion unmarked), output new
	// line2 (addition unmarked), line3 (context).
	// Result: "line1\nnew line2\nline3" — this is the full current content.
	marked := map[int]bool{}
	result := computeIntermediateFile(parentContent, rows, headerIdx, len(rows), marked)
	if !strings.Contains(result, "new line2") {
		t.Errorf("unmarked intermediate should contain new line2: %q", result)
	}
	if strings.Contains(result, "old line2") {
		t.Errorf("unmarked intermediate should not contain old line2: %q", result)
	}

	// Case 2: both deletion and addition marked → intermediate = parent
	// (all changes stay in current revision).
	marked = map[int]bool{delIdx: true, addIdx: true}
	result = computeIntermediateFile(parentContent, rows, headerIdx, len(rows), marked)
	if !strings.Contains(result, "old line2") {
		t.Errorf("all-marked intermediate should contain old line2: %q", result)
	}
	if strings.Contains(result, "new line2") {
		t.Errorf("all-marked intermediate should not contain new line2: %q", result)
	}

	// Case 3: only addition marked (deletion unmarked) → intermediate has
	// neither old nor new line2 (deletion removes old, addition stays in
	// current).
	// Actually: deletion unmarked → skip (removed in intermediate)
	//           addition marked → skip (stays in current)
	// Result: "line1\nline3" — the line2 is gone.
	marked = map[int]bool{addIdx: true}
	result = computeIntermediateFile(parentContent, rows, headerIdx, len(rows), marked)
	if strings.Contains(result, "old line2") {
		t.Errorf("add-marked intermediate should not contain old line2: %q", result)
	}
	if strings.Contains(result, "new line2") {
		t.Errorf("add-marked intermediate should not contain new line2: %q", result)
	}
	if !strings.Contains(result, "line1") || !strings.Contains(result, "line3") {
		t.Errorf("add-marked intermediate should contain line1 and line3: %q", result)
	}
}

// TestSplitConfirmNothingToSplit verifies that confirming with all lines
// marked shows an error and stays in split mode so the user can adjust.
func TestSplitConfirmNothingToSplit(t *testing.T) {
	m := setupSplitModel()
	m.splitMode = true
	m.splitMarked = map[int]bool{}

	// Mark all addition/deletion lines.
	for i, r := range m.diffRows {
		if r.kind == rowLine && (r.lineKind == "addition" || r.lineKind == "deletion") {
			m.splitMarked[i] = true
		}
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	if cmd != nil {
		t.Errorf("confirm with all marked should not produce a command")
	}
	if m.errMsg == "" {
		t.Error("confirm with all marked should set errMsg")
	}
}

// TestSplitNavigation verifies that navigation keys work in split mode.
func TestSplitNavigation(t *testing.T) {
	m := setupSplitModel()
	m.splitMode = true
	m.splitMarked = map[int]bool{}

	// down should move the cursor.
	initialChunk := m.diffCurChunk
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = next.(Model)
	if m.diffCurChunk == initialChunk {
		t.Error("j did not advance cursor in split mode")
	}

	// up should move it back.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = next.(Model)
	if m.diffCurChunk != initialChunk {
		t.Error("k did not move cursor back in split mode")
	}

	// home should jump to top.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = next.(Model)
	if m.diffCurChunk != 0 || m.diffCurLine != 0 {
		t.Errorf("g did not jump to top: chunk=%d line=%d", m.diffCurChunk, m.diffCurLine)
	}
}
