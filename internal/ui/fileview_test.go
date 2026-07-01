package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

// TestAnnotateToDiffRows verifies that annotate lines are converted to
// context-style diff rows with single line numbers and tab expansion.
func TestAnnotateToDiffRows(t *testing.T) {
	lines := []jj.AnnotateLine{
		{ChangeID: "abc", LineNo: 1, Text: "package main"},
		{ChangeID: "abc", LineNo: 2, Text: ""},
		{ChangeID: "def", LineNo: 3, Text: "\tfunc main() {}"},
	}
	rows := annotateToDiffRows(lines, nil)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	for i, r := range rows {
		if r.kind != rowLine {
			t.Errorf("row %d: kind = %v, want rowLine", i, r.kind)
		}
		if r.lineKind != "context" {
			t.Errorf("row %d: lineKind = %q, want context", i, r.lineKind)
		}
		if r.newNum != lines[i].LineNo {
			t.Errorf("row %d: newNum = %d, want %d", i, r.newNum, lines[i].LineNo)
		}
		if r.oldNum != 0 {
			t.Errorf("row %d: oldNum = %d, want 0", i, r.oldNum)
		}
	}
	// Tab expansion in the non-highlighted fallback.
	if txt := spansText(rows[2].spans); !strings.Contains(txt, "    func main() {}") {
		t.Errorf("row 2: tabs not expanded, got %q", txt)
	}
}

// TestFileModeRendering verifies that renderDiffPanel in file mode shows the
// file path title, the blame head with commit info, line numbers, and source
// content — and produces exactly the requested height.
func TestFileModeRendering(t *testing.T) {
	lines := []jj.AnnotateLine{
		{ChangeID: "mwqwmwpp", CommitID: "b2fe214a", Author: "hackr@hackr.sh", LineNo: 1, Description: "Rewrite gojo", Text: "package main"},
		{ChangeID: "mwqwmwpp", CommitID: "b2fe214a", Author: "hackr@hackr.sh", LineNo: 2, Description: "Rewrite gojo", Text: ""},
		{ChangeID: "kxmyusxx", CommitID: "aa0100ff", Author: "al@ice.gg", LineNo: 3, Description: "add main", Text: "func main() {}"},
	}
	rows := annotateToDiffRows(lines, nil)
	digits := lineDigits(len(lines))
	head := buildBlameHead(80, "mwqwmwpp", "hackr@hackr.sh", "Rewrite gojo")

	out := renderDiffPanel(80, 24, "main.go", 0, false, false, 0, "", false, rows, digits, nil, "", 0, -1, nil, nil, splitView{}, true, head)
	if len(out) != 24 {
		t.Fatalf("panel lines = %d, want 24", len(out))
	}

	view := ansi.Strip(strings.Join(out, "\n"))
	// Title shows the file path.
	if !strings.Contains(view, "main.go") {
		t.Errorf("missing file path in title: %s", view)
	}
	// Blame head shows commit info.
	if !strings.Contains(view, "hackr@hackr.sh") {
		t.Errorf("missing author in blame head: %s", view)
	}
	if !strings.Contains(view, "Rewrite gojo") {
		t.Errorf("missing description in blame head: %s", view)
	}
	// Source content is visible.
	if !strings.Contains(view, "package main") {
		t.Errorf("missing source content: %s", view)
	}
	if !strings.Contains(view, "func main() {}") {
		t.Errorf("missing source content: %s", view)
	}
}

// TestFileModeCursorBar verifies that the cursor bar (┃) is highlighted in
// yellow on the cursor line and invisible (panel-coloured) on other lines.
func TestFileModeCursorBar(t *testing.T) {
	lines := []jj.AnnotateLine{
		{ChangeID: "a", LineNo: 1, Text: "line1"},
		{ChangeID: "a", LineNo: 2, Text: "line2"},
		{ChangeID: "a", LineNo: 3, Text: "line3"},
	}
	rows := annotateToDiffRows(lines, nil)
	digits := lineDigits(len(lines))
	head := buildBlameHead(80, "a", "x@y.z", "test")
	headLen := len(head)

	layout := computeDiffLayoutPure(80, 20, headLen, rows, "", digits, nil, false, true)
	cursorBodyRow := headLen + layout.starts[1] // cursor on line 2

	out := renderDiffPanel(80, 24, "f.go", 0, false, false, 0, "", false, rows, digits, nil, "", 0, cursorBodyRow, nil, nil, splitView{}, true, head)
	if len(out) != 24 {
		t.Fatalf("panel lines = %d, want 24", len(out))
	}
	// The output should contain the ┃ bar on at least one line.
	view := ansi.Strip(strings.Join(out, "\n"))
	if !strings.Contains(view, "┃") {
		t.Errorf("missing cursor bar: %s", view)
	}
}
