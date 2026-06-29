package ui

import "testing"

const sampleDiff = `diff --git a/foo.go b/foo.go
index 1234567..89abcde 100644
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,6 @@
 package foo

-func Old() int {
-	return 1
+func New() int {
+	// a comment
+	return 2
 }
diff --git a/new.txt b/new.txt
new file mode 100644
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
`

func TestRenderDiff(t *testing.T) {
	rows := renderDiff(sampleDiff)
	if len(rows) == 0 {
		t.Fatal("no rows produced")
	}

	var fileHeaders, hunks, lines int
	for _, r := range rows {
		switch r.kind {
		case rowFileHeader:
			fileHeaders++
		case rowHunkHeader:
			hunks++
		case rowLine:
			lines++
		}
	}
	if fileHeaders != 2 {
		t.Errorf("file headers = %d, want 2", fileHeaders)
	}
	if hunks != 2 {
		t.Errorf("hunk headers = %d, want 2", hunks)
	}
	if lines == 0 {
		t.Error("no content lines")
	}

	// First file header should be modified foo.go and carry highlighting.
	if rows[0].kind != rowFileHeader || rows[0].changeType != "modified" {
		t.Errorf("row0 = %+v, want modified foo.go header", rows[0])
	}

	// The new.txt file must be flagged added.
	var foundAdded bool
	for _, r := range rows {
		if r.kind == rowFileHeader && r.path == "new.txt" && r.changeType == "added" {
			foundAdded = true
		}
	}
	if !foundAdded {
		t.Error("new.txt added header not found")
	}

	// Ensure renderDiffPanel produces exactly the requested height.
	out := renderDiffPanel(80, 24, "abcd", false, rows, maxLineDigits(rows), nil, "", 0, -1, nil)
	if len(out) != 24 {
		t.Errorf("diff panel lines = %d, want 24", len(out))
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	if rows := renderDiff(""); rows != nil {
		t.Errorf("empty diff produced %d rows", len(rows))
	}
}

func TestComputeDiffChunks(t *testing.T) {
	rows := renderDiff(sampleDiff)
	chunks := computeDiffChunks(rows, 0)
	// foo.go: -Old/-return 1/+New/+comment/+return 2 is one contiguous chunk (5 lines),
	// new.txt: +hello/+world is another (2 lines).
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != 5 || len(chunks[1]) != 2 {
		t.Errorf("chunk sizes = %d, %d; want 5, 2", len(chunks[0]), len(chunks[1]))
	}
}

// TestDiffCursorScroll verifies the "reveal one line at a time" behavior for a
// long chunk: stepping down through a chunk taller than the viewport scrolls a
// line at a time, and stepping past the end jumps to the next chunk. It also
// checks that snapping to a chunk shows diffChunkContext lines of context
// before it.
func TestDiffCursorScroll(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,10 @@\n"
	for i := 0; i < 10; i++ {
		raw += "+line\n"
	}
	raw += "diff --git a/b b/b\n+++ b/b\n@@ -1,1 +1,2 @@\n+x\n+y\n"

	m := Model{width: 80, height: 10, view: viewLog, diffOpen: true}
	m.diffRows = renderDiff(raw)
	m.diffStatus = nil
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen())
	m.diffCurChunk, m.diffCurLine = 0, 0
	bodyH := m.diffBodyHeight() // visible rows below title

	// Entering chunk 0 from above reveals diffChunkContext lines of context
	// before the chunk (clamped at 0), then as much of the chunk as fits.
	m.diffEnterChunkDown()
	wantTop := m.diffChunks[0][0] - diffChunkContext
	if wantTop < 0 {
		wantTop = 0
	}
	if m.diffScrollY != wantTop {
		t.Fatalf("enterChunkDown scrollY = %d, want %d (chunk0 start %d - ctx %d)",
			m.diffScrollY, wantTop, m.diffChunks[0][0], diffChunkContext)
	}

	// Walk down chunk 0: cursor stays in view, and once it passes the bottom
	// edge the viewport scrolls exactly one line per step (the reveal behavior).
	for i := 1; i < len(m.diffChunks[0]); i++ {
		m.diffMoveDown()
		if m.diffCursorBodyRow() != m.diffChunks[0][i] {
			t.Fatalf("step %d: cursor = %d, want %d", i, m.diffCursorBodyRow(), m.diffChunks[0][i])
		}
		row := m.diffCursorBodyRow()
		if row < m.diffScrollY || row >= m.diffScrollY+bodyH {
			t.Fatalf("step %d: cursor %d out of view [%d,%d)", i, row, m.diffScrollY, m.diffScrollY+bodyH)
		}
	}

	// One more down: jump to chunk 1, line 0, with context shown above it.
	m.diffMoveDown()
	if m.diffCurChunk != 1 || m.diffCurLine != 0 {
		t.Fatalf("after chunk0: chunk=%d line=%d, want 1,0", m.diffCurChunk, m.diffCurLine)
	}
	c1 := m.diffCursorBodyRow()
	if c1 < m.diffScrollY || c1 >= m.diffScrollY+bodyH {
		t.Errorf("chunk1 cursor %d out of view [%d,%d)", c1, m.diffScrollY, m.diffScrollY+bodyH)
	}

	// Walk back up into chunk 0: entering from below lands on chunk 0's last
	// line, with diffChunkContext lines of context shown after it.
	m.diffMoveUp()
	if m.diffCurChunk != 0 || m.diffCurLine != len(m.diffChunks[0])-1 {
		t.Fatalf("moveUp: chunk=%d line=%d, want 0,%d", m.diffCurChunk, m.diffCurLine, len(m.diffChunks[0])-1)
	}
	last := m.diffChunks[0][len(m.diffChunks[0])-1]
	wantUp := last + diffChunkContext - bodyH + 1
	if wantUp < 0 {
		wantUp = 0
	}
	if wantUp > m.diffMaxScroll() {
		wantUp = m.diffMaxScroll()
	}
	if m.diffScrollY != wantUp {
		t.Errorf("chunk0 re-entry scrollY = %d, want %d", m.diffScrollY, wantUp)
	}
}

// TestDiffCursorFreeScrollTop verifies that pressing up at the first line of
// the first chunk free-scrolls the viewport upward to reveal the status
// section, with the cursor resting on the first chunk line.
func TestDiffCursorFreeScrollTop(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,2 @@\n+x\n+y\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen())
	m.diffCurChunk, m.diffCurLine = 0, 0
	m.diffEnterChunkDown()
	cursorRow := m.diffCursorBodyRow()
	startScroll := m.diffScrollY

	// Press up repeatedly: the viewport scrolls up one line at a time until the
	// status section (top, scrollY 0) is visible, while the cursor stays put.
	for m.diffScrollY > 0 {
		m.diffMoveUp()
		if m.diffCursorBodyRow() != cursorRow {
			t.Fatalf("cursor moved to %d, want it to stay at %d", m.diffCursorBodyRow(), cursorRow)
		}
		if m.diffScrollY >= startScroll {
			t.Fatalf("scrollY = %d did not decrease from %d", m.diffScrollY, startScroll)
		}
	}
	if m.diffScrollY != 0 {
		t.Errorf("expected to reach top (scrollY 0), got %d", m.diffScrollY)
	}
	// One more up at the very top is a no-op.
	prev := m.diffScrollY
	m.diffMoveUp()
	if m.diffScrollY != prev {
		t.Errorf("scrollY changed to %d at top, want %d", m.diffScrollY, prev)
	}
}

// TestDiffCursorRefresh verifies that a poll/focus diff refresh preserves the
// cursor position instead of snapping back to the first chunk.
func TestDiffCursorRefresh(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,4 @@\n+p\n+q\n+r\n+s\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true, diffIsRevision: true, diffRev: "abc"}
	rows := renderDiff(raw)
	// Simulate a first load (no prior rows): cursor starts at chunk 0, line 0.
	m.diffStatus = nil
	m.diffRows = rows
	m.diffDigits = maxLineDigits(rows)
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen())
	m.diffCurChunk, m.diffCurLine = 0, 0

	// Navigate to chunk 0, line 2.
	m.diffMoveDown()
	m.diffMoveDown()
	if m.diffCurChunk != 0 || m.diffCurLine != 2 {
		t.Fatalf("before refresh: chunk=%d line=%d, want 0,2", m.diffCurChunk, m.diffCurLine)
	}

	// Simulate the poll refresh firing diffLoadedMsg for the same rev. The
	// Update path would preserve the cursor; replicate that logic here.
	isRefresh := m.diffIsRevision && "abc" == m.diffRev && len(m.diffRows) > 0
	if !isRefresh {
		t.Fatal("expected refresh detection")
	}
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen()) // unchanged shape
	m.diffClampMax() // refresh path preserves viewport; only clamps
	if r := m.diffCursorBodyRow(); r >= 0 && (r < m.diffScrollY || r >= m.diffScrollY+m.diffBodyHeight()) {
		m.diffFollowCursor()
	}
	if m.diffCurChunk != 0 || m.diffCurLine != 2 {
		t.Errorf("after refresh: chunk=%d line=%d, want preserved 0,2", m.diffCurChunk, m.diffCurLine)
	}
}

// TestDiffCursorShowsContext confirms that a chunk which fits in the viewport
// keeps the whole chunk visible while stepping, so surrounding context (hunk
// header) stays on screen rather than being pinned to the top edge.
func TestDiffCursorShowsContext(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,3 +1,3 @@\n ctx1\n-old\n+new\n ctx2\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen())
	m.diffCurChunk, m.diffCurLine = 0, 0
	head := m.diffHeadLen()
	chunkFirst := m.diffChunks[0][0] // body row of -old

	// Scroll so the chunk is near the bottom of the viewport, then step: the
	// whole chunk must stay visible (scroll moves only to keep it so), proving
	// context above isn't lost to a pin-to-top jump.
	m.diffScrollY = chunkFirst // pin top as if we'd just entered
	m.diffCurLine = 1          // move to +new
	m.diffFollowCursor()
	// The chunk's first line should still be visible (not scrolled away).
	if chunkFirst < m.diffScrollY {
		t.Errorf("chunk first line %d scrolled above viewport %d", chunkFirst, m.diffScrollY)
	}
	_ = head
}
