package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

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
	out := renderDiffPanel(80, 24, "abcd", 0, false, false, 0, "", false, rows, maxLineDigits(rows), nil, "", 0, -1, -1, -1, nil, splitView{}, false, nil, nil, -1, "")
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
	chunks := computeDiffChunks(rows, 0, nil)
	// File headers are navigable single-element chunks. Layout:
	// [0] foo.go header (1), [1] foo.go changes (5),
	// [2] new.txt header (1), [3] new.txt changes (2).
	if len(chunks) != 4 {
		t.Fatalf("chunks = %d, want 4 (2 file headers + 2 change chunks)", len(chunks))
	}
	if len(chunks[0]) != 1 || len(chunks[1]) != 5 ||
		len(chunks[2]) != 1 || len(chunks[3]) != 2 {
		t.Errorf("chunk sizes = %d, %d, %d, %d; want 1, 5, 1, 2",
			len(chunks[0]), len(chunks[1]), len(chunks[2]), len(chunks[3]))
	}
}

// TestWordDiff verifies that word-level diff highlighting is applied to
// modified lines (paired deletion+addition) but not to pure additions or
// pure deletions.
func TestWordDiff(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,3 +1,3 @@\n context\n-old line\n+new line\n+pure addition\n"
	rows := renderDiff(raw)

	var ctxRow, delRow, modRow, pureRow *diffRow
	for i := range rows {
		r := &rows[i]
		if r.kind != rowLine {
			continue
		}
		switch {
		case r.lineKind == "context":
			ctxRow = r
		case r.lineKind == "deletion":
			delRow = r
		case r.lineKind == "addition" && pureRow == nil && modRow != nil:
			pureRow = r
		case r.lineKind == "addition" && modRow == nil:
			modRow = r
		}
	}
	if delRow == nil || modRow == nil || pureRow == nil {
		t.Fatal("missing expected rows")
	}

	// The modified addition line ("new line") should have at least one span
	// with a bg highlight (the changed word "new").
	hasBg := false
	for _, s := range modRow.spans {
		if s.bg != "" {
			hasBg = true
			break
		}
	}
	if !hasBg {
		t.Error("modified addition line has no highlighted spans; want word-diff bg on changed token")
	}

	// The deletion line ("old line") should also have a highlighted span.
	hasBg = false
	for _, s := range delRow.spans {
		if s.bg != "" {
			hasBg = true
			break
		}
	}
	if !hasBg {
		t.Error("deletion line has no highlighted spans; want word-diff bg on changed token")
	}

	// The pure addition line ("pure addition") should NOT have any bg
	// highlight — it has no paired deletion.
	for _, s := range pureRow.spans {
		if s.bg != "" {
			t.Error("pure addition line has highlighted spans; want none (no paired deletion)")
			break
		}
	}

	// The context line should never have bg highlights.
	for _, s := range ctxRow.spans {
		if s.bg != "" {
			t.Error("context line has highlighted spans; want none")
			break
		}
	}
}

// TestWordDiffSplitSpans verifies that splitSpansByRanges correctly splits
// spans at highlight boundaries and preserves text content.
func TestWordDiffSplitSpans(t *testing.T) {
	spans := []span{
		{text: "func", fg: "#kw"},
		{text: " Old", fg: "#fn"},
		{text: "() int {}", fg: "#plain"},
	}
	ranges := [][2]int{{5, 9}} // "Old(" in byte positions
	result := splitSpansByRanges(spans, ranges, "#highlight")

	// Reconstruct text to verify nothing was lost.
	got := spansText(result)
	want := "func Old() int {}"
	if got != want {
		t.Errorf("text = %q, want %q", got, want)
	}

	// At least one span should have the highlight bg.
	hasHighlight := false
	for _, s := range result {
		if s.bg == "#highlight" {
			hasHighlight = true
			break
		}
	}
	if !hasHighlight {
		t.Error("no span has highlight bg")
	}
}

// TestWordDiffCompute verifies the LCS-based word diff classification.
func TestWordDiffCompute(t *testing.T) {
	oldClass, newClass := computeWordDiff(tokenizeWords("hello world"), tokenizeWords("hello earth"))

	// Old: "hello"(common) " "(common) "world"(removed)
	if len(oldClass) != 3 {
		t.Fatalf("old classes = %d, want 3", len(oldClass))
	}
	if oldClass[0] != wordCommon || oldClass[1] != wordCommon {
		t.Errorf("old[0:1] = %d,%d; want wordCommon,wordCommon", oldClass[0], oldClass[1])
	}
	if oldClass[2] != wordRemoved {
		t.Errorf("old[2] = %d, want wordRemoved", oldClass[2])
	}

	// New: "hello"(common) " "(common) "earth"(added)
	if len(newClass) != 3 {
		t.Fatalf("new classes = %d, want 3", len(newClass))
	}
	if newClass[0] != wordCommon || newClass[1] != wordCommon {
		t.Errorf("new[0:1] = %d,%d; want wordCommon,wordCommon", newClass[0], newClass[1])
	}
	if newClass[2] != wordAdded {
		t.Errorf("new[2] = %d, want wordAdded", newClass[2])
	}
}

// TestDiffCursorScroll verifies the centering behavior for cursor moves:
// every j/k step recenters the viewport on the cursor row, scrolling one line
// per step inside a chunk and jumping to center on chunk hops, clamping at
// the page ends (top: scrollY 0, bottom: maxScroll).
func TestDiffCursorScroll(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,10 @@\n"
	for i := 0; i < 10; i++ {
		raw += "+line\n"
	}
	raw += "diff --git a/b b/b\n+++ b/b\n@@ -1,1 +1,2 @@\n+x\n+y\n"

	m := Model{width: 80, height: 11, view: viewLog, diffOpen: true}
	m.diffRows = renderDiff(raw)
	m.diffStatus = nil
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	// Chunks: [0] a header, [1] 10 additions, [2] b header, [3] 2 additions.
	const addChunk = 1 // index of a's 10-line addition chunk
	m.diffCurChunk, m.diffCurLine = addChunk, 0
	bodyH := m.diffBodyHeight()
	centerScroll := func(row int) int {
		s := row - (bodyH-1)/2
		if s < 0 {
			s = 0
		}
		if mx := m.diffMaxScroll(); s > mx {
			s = mx
		}
		return s
	}
	assertCentered := func(tag string) {
		t.Helper()
		row := m.diffCursorBodyRow()
		if w := centerScroll(row); m.diffScrollY != w {
			t.Errorf("%s: scrollY = %d, want %d (cursor %d centered in %d rows)", tag, m.diffScrollY, w, row, bodyH)
		}
	}

	m.diffCenterCursor()
	startScroll := m.diffScrollY
	assertCentered("initial")

	// Walk down the addition chunk: the cursor stays centered, so the
	// viewport scrolls exactly one line per step until the bottom clamp.
	prev := startScroll
	for i := 1; i < len(m.diffChunks[addChunk]); i++ {
		m.diffMoveDown()
		if m.diffCursorBodyRow() != m.diffChunks[addChunk][i] {
			t.Fatalf("step %d: cursor = %d, want %d", i, m.diffCursorBodyRow(), m.diffChunks[addChunk][i])
		}
		assertCentered(fmt.Sprintf("step %d", i))
		if m.diffScrollY < m.diffMaxScroll() && m.diffScrollY != prev+1 {
			t.Fatalf("step %d: scrollY = %d, want %d (one-line follow)", i, m.diffScrollY, prev+1)
		}
		prev = m.diffScrollY
	}

	// One more down: jump to the b file header (next navigable item), still
	// centered.
	m.diffMoveDown()
	if m.diffCurChunk != addChunk+1 || m.diffCurLine != 0 {
		t.Fatalf("after addition chunk: chunk=%d line=%d, want %d,0", m.diffCurChunk, m.diffCurLine, addChunk+1)
	}
	assertCentered("b file header")

	// Walk back up: re-enter the addition chunk on its last line, centered.
	m.diffMoveUp()
	if m.diffCurChunk != addChunk || m.diffCurLine != len(m.diffChunks[addChunk])-1 {
		t.Fatalf("moveUp: chunk=%d line=%d, want %d,%d", m.diffCurChunk, m.diffCurLine, addChunk, len(m.diffChunks[addChunk])-1)
	}
	assertCentered("re-entry from below")
}

// TestDiffCenterPageEnds pins the centering clamps: G centers onto the last
// chunk line but clamps at maxScroll (bottom page end), g jumps to the first
// chunk at scroll 0 (top page end), and a mid-document j step centers
// exactly.
func TestDiffCenterPageEnds(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,40 @@\n"
	for i := 0; i < 40; i++ {
		raw += "+line\n"
	}
	m := Model{width: 80, height: 11, view: viewLog, diffOpen: true}
	m.diffRows = renderDiff(raw)
	m.diffStatus = nil
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	bodyH := m.diffBodyHeight()

	// G: bottom page end — clamps to maxScroll.
	m.diffMoveBottom()
	if m.diffCurChunk != len(m.diffChunks)-1 || m.diffCurLine != len(m.diffChunks[len(m.diffChunks)-1])-1 {
		t.Fatalf("G: chunk=%d line=%d, want last chunk's last line", m.diffCurChunk, m.diffCurLine)
	}
	if m.diffScrollY != m.diffMaxScroll() {
		t.Errorf("G: scrollY = %d, want clamp %d", m.diffScrollY, m.diffMaxScroll())
	}

	// g: top page end — cursor on the first chunk, scroll pinned to 0 so the
	// head/status section is fully visible.
	m.diffMoveTop()
	if m.diffCurChunk != 0 || m.diffCurLine != 0 {
		t.Fatalf("g: chunk=%d line=%d, want 0,0", m.diffCurChunk, m.diffCurLine)
	}
	if m.diffScrollY != 0 {
		t.Errorf("g: scrollY = %d, want 0 (top of document)", m.diffScrollY)
	}

	// j into the middle: exact center, no clamp.
	m.diffMoveDown() // chunk 0 (file header) → chunk 1 first line
	if m.diffCurChunk != 1 || m.diffCurLine != 0 {
		t.Fatalf("j: chunk=%d line=%d, want 1,0", m.diffCurChunk, m.diffCurLine)
	}
	row := m.diffCursorBodyRow()
	if want := row - (bodyH-1)/2; m.diffScrollY != want {
		t.Errorf("mid step: scrollY = %d, want %d (cursor %d centered in %d)", m.diffScrollY, want, row, bodyH)
	}
	if row < m.diffScrollY || row >= m.diffScrollY+bodyH {
		t.Errorf("cursor %d out of view [%d,%d)", row, m.diffScrollY, m.diffScrollY+bodyH)
	}
}

// TestDiffCursorFreeScrollTop verifies that pressing up at the first line of
// the first chunk free-scrolls the viewport upward to reveal the status
// section, with the cursor resting on the first chunk line. (Centering can't
// reveal the head while the cursor sits on it, so the edge free-scrolls.)
func TestDiffCursorFreeScrollTop(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,10 @@\n"
	for i := 0; i < 10; i++ {
		raw += "+line\n"
	}
	m := Model{width: 80, height: 11, view: viewLog, diffOpen: true}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	m.diffCurChunk, m.diffCurLine = 0, 0
	m.diffCenterCursor()
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

// TestDiffWheelScrollsWithoutCursor verifies that mouse-wheel scrolling in the
// diff panel moves only the viewport — the chunk cursor stays where it is,
// even when the scroll leaves it off screen. The next cursor movement snaps
// the view back to the cursor.
func TestDiffWheelScrollsWithoutCursor(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,10 @@\n"
	for i := 0; i < 10; i++ {
		raw += "+line\n"
	}
	m := Model{ready: true, width: 80, height: 11, view: viewLog, diffOpen: true}
	m.diffRows = renderDiff(raw)
	m.diffStatus = nil
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	// Chunks: [0] a header, [1] 10 additions. Cursor into the addition chunk.
	m.diffCurChunk, m.diffCurLine = 1, 0
	m.diffCenterCursor()
	startScroll := m.diffScrollY

	// Message level: one wheel event scrolls the viewport one step without
	// touching the cursor.
	nm, _ := m.Update(wheelPress(tea.MouseButtonWheelDown, 10, 5))
	m = nm.(Model)
	if m.diffCurChunk != 1 || m.diffCurLine != 0 {
		t.Fatalf("wheel moved cursor to chunk=%d line=%d, want 1,0", m.diffCurChunk, m.diffCurLine)
	}
	if m.diffScrollY != startScroll+1 {
		t.Fatalf("scrollY = %d after one wheel step, want %d", m.diffScrollY, startScroll+1)
	}

	// Wheel down directly until the cursor leaves the viewport (wheelStep,
	// bypassing coalescing): still no cursor movement.
	bodyH := m.diffBodyHeight()
	cur := m.diffCursorBodyRow()
	maxS := m.diffMaxScroll()
	for m.diffScrollY <= cur && m.diffScrollY < maxS {
		nm, _ := m.Update(wheelTickMsg{})
		m = nm.(Model) // flush any pending burst
		m.wheelStep(1)
	}
	if m.diffCurChunk != 1 || m.diffCurLine != 0 {
		t.Fatalf("wheel moved cursor to chunk=%d line=%d, want 1,0", m.diffCurChunk, m.diffCurLine)
	}
	if m.diffScrollY <= cur {
		t.Fatalf("cursor %d should be above the viewport (scrollY %d) after wheeling down", cur, m.diffScrollY)
	}

	// The next cursor movement reclaims the viewport: j advances the cursor
	// and the view snaps back so the cursor is visible again.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = nm.(Model)
	if m.diffCurChunk != 1 || m.diffCurLine != 1 {
		t.Fatalf("after j: chunk=%d line=%d, want 1,1", m.diffCurChunk, m.diffCurLine)
	}
	cur = m.diffCursorBodyRow()
	if cur < m.diffScrollY || cur >= m.diffScrollY+bodyH {
		t.Fatalf("after j: cursor %d out of view [%d,%d)", cur, m.diffScrollY, m.diffScrollY+bodyH)
	}
}

// TestDiffPageKeysScrollWithoutCursor verifies that the page keys scroll the
// diff viewport by half a screen without touching the chunk cursor, in both
// directions, clamping at the ends.
func TestDiffPageKeysScrollWithoutCursor(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,40 @@\n"
	for i := 0; i < 40; i++ {
		raw += "+line\n"
	}
	m := Model{ready: true, width: 80, height: 11, view: viewLog, diffOpen: true}
	m.diffRows = renderDiff(raw)
	m.diffStatus = nil
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	m.diffCurChunk, m.diffCurLine = 1, 0
	m.diffCenterCursor()
	half := max(1, m.diffBodyHeight()) / 2
	cur := m.diffCursorBodyRow()

	press := func(k tea.KeyMsg, want int) {
		t.Helper()
		nm, _ := m.Update(k)
		m = nm.(Model)
		if m.diffCurChunk != 1 || m.diffCurLine != 0 {
			t.Fatalf("%v moved cursor to chunk=%d line=%d, want 1,0", k, m.diffCurChunk, m.diffCurLine)
		}
		if m.diffScrollY != want {
			t.Fatalf("%v: scrollY = %d, want %d", k, m.diffScrollY, want)
		}
		if m.diffCursorBodyRow() != cur {
			t.Fatalf("%v changed the cursor row from %d to %d", k, cur, m.diffCursorBodyRow())
		}
	}
	start := m.diffScrollY
	press(tea.KeyMsg{Type: tea.KeyPgDown}, min(m.diffMaxScroll(), start+half))
	press(tea.KeyMsg{Type: tea.KeyCtrlD}, min(m.diffMaxScroll(), start+2*half))
	press(tea.KeyMsg{Type: tea.KeyPgDown}, min(m.diffMaxScroll(), start+3*half))
	press(tea.KeyMsg{Type: tea.KeyCtrlU}, min(m.diffMaxScroll(), start+2*half))

	// Clamps at the top without touching the cursor.
	for i := 0; m.diffScrollY > 0; i++ {
		if i > 10 {
			t.Fatalf("pgup never clamped: scrollY %d", m.diffScrollY)
		}
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		m = nm.(Model)
	}
	if m.diffCurChunk != 1 || m.diffCurLine != 0 || m.diffCursorBodyRow() != cur {
		t.Fatal("clamping moved the cursor")
	}
}

// TestDiffCursorRefresh verifies that a poll/focus diff refresh preserves the
// cursor position instead of snapping back to the first chunk.
func TestDiffCursorRefresh(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,4 @@\n+p\n+q\n+r\n+s\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true, diffIsRevision: true, diffRev: "abc"}
	rows := renderDiff(raw)
	// Chunks: [0] a header, [1] 4 additions (p, q, r, s).
	m.diffStatus = nil
	m.diffRows = rows
	m.diffDigits = maxLineDigits(rows)
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	m.diffCurChunk, m.diffCurLine = 0, 0

	// Navigate to the addition chunk (1), line 2.
	m.diffMoveDown() // file header → chunk 1, line 0
	m.diffMoveDown() // line 1
	m.diffMoveDown() // line 2
	if m.diffCurChunk != 1 || m.diffCurLine != 2 {
		t.Fatalf("before refresh: chunk=%d line=%d, want 1,2", m.diffCurChunk, m.diffCurLine)
	}

	// Simulate the poll refresh firing diffLoadedMsg for the same rev. The
	// Update path would preserve the cursor; replicate that logic here.
	isRefresh := m.diffIsRevision && "abc" == m.diffRev && len(m.diffRows) > 0
	if !isRefresh {
		t.Fatal("expected refresh detection")
	}
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil) // unchanged shape
	m.diffClampMax()                                             // refresh path preserves viewport; only clamps
	if r := m.diffCursorBodyRow(); r >= 0 && (r < m.diffScrollY || r >= m.diffScrollY+m.diffBodyHeight()) {
		m.diffFollowCursor()
	}
	if m.diffCurChunk != 1 || m.diffCurLine != 2 {
		t.Errorf("after refresh: chunk=%d line=%d, want preserved 1,2", m.diffCurChunk, m.diffCurLine)
	}
}

// TestDiffUnchangedReloadKeepsState verifies the no-change poll fast path: an
// unchanged diffLoadedMsg keeps rows, cursor, and scroll intact while still
// accepting status updates.
func TestDiffUnchangedReloadKeepsState(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,4 @@\n+p\n+q\n+r\n+s\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true, diffIsRevision: true, diffRev: "abc"}
	m.diffRows = renderDiff(raw)
	m.diffSrcRaw = raw
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(m.diffRows)
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	m.diffChunksHead = m.diffHeadLen()
	m.diffCurChunk, m.diffCurLine = 1, 2
	m.diffScrollY = 3

	nm, _ := m.Update(diffLoadedMsg{
		rev:       "abc",
		status:    []jj.StatusEntry{{Path: "a", Status: jj.StatusModified}},
		unchanged: true,
	})
	m = nm.(Model)

	if len(m.diffRows) == 0 {
		t.Error("rows were dropped on unchanged reload")
	}
	if m.diffSrcRaw != raw {
		t.Error("diffSrcRaw changed on unchanged reload")
	}
	if m.diffCurChunk != 1 || m.diffCurLine != 2 {
		t.Errorf("cursor moved to chunk=%d line=%d, want 1,2", m.diffCurChunk, m.diffCurLine)
	}
	if m.diffScrollY != 3 {
		t.Errorf("scrollY = %d, want 3", m.diffScrollY)
	}
	if len(m.diffStatus) != 1 || m.diffStatus[0].Path != "a" {
		t.Errorf("status not updated: %+v", m.diffStatus)
	}
}

// TestDiffUnchangedReloadStatusResize verifies that a same-size class change
// in the status list keeps the cached chunk indices, while a size change
// recomputes them (chunk rows embed the head length).
func TestDiffUnchangedReloadStatusResize(t *testing.T) {
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,4 @@\n+p\n+q\n+r\n+s\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true, diffIsRevision: true, diffRev: "abc"}
	m.diffRows = renderDiff(raw)
	m.diffSrcRaw = raw
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(m.diffRows)
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	m.diffChunksHead = m.diffHeadLen()

	// No status → 1 placeholder row; two entries → head grows by one row.
	firstAdditionRowOld := m.diffChunks[1][0]

	nm, _ := m.Update(diffLoadedMsg{
		rev: "abc",
		status: []jj.StatusEntry{
			{Path: "a", Status: jj.StatusModified},
			{Path: "b", Status: jj.StatusAdded},
		},
		unchanged: true,
	})
	m = nm.(Model)

	firstAdditionRowNew := m.diffChunks[1][0]
	if firstAdditionRowNew != firstAdditionRowOld+1 {
		t.Errorf("chunk start = %d, want %d (shifted by +1 for the extra status row)",
			firstAdditionRowNew, firstAdditionRowOld+1)
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
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	// Chunks: [0] a header, [1] -old/+new (2 elements).
	const changeChunk = 1
	m.diffCurChunk, m.diffCurLine = changeChunk, 0
	chunkFirst := m.diffChunks[changeChunk][0]                               // body row of -old
	chunkLast := m.diffChunks[changeChunk][len(m.diffChunks[changeChunk])-1] // +new

	// Stepping through the chunk keeps the viewport centered on the cursor;
	// a chunk that fits stays fully visible so surrounding context isn't lost.
	m.diffCurChunk, m.diffCurLine = changeChunk, 0
	m.diffCenterCursor()
	m.diffMoveDown() // onto +new
	if m.diffCurLine != 1 {
		t.Fatalf("curLine = %d, want 1 (+new)", m.diffCurLine)
	}
	bodyH := m.diffBodyHeight()
	if chunkFirst < m.diffScrollY {
		t.Errorf("chunk first line %d scrolled above viewport %d", chunkFirst, m.diffScrollY)
	}
	if chunkLast >= m.diffScrollY+bodyH {
		t.Errorf("chunk last line %d below viewport %d", chunkLast, m.diffScrollY+bodyH)
	}
}

// TestDiffWrap verifies that long diff lines wrap onto extra terminal lines
// without breaking the panel's fixed-height contract or the chunk cursor's
// scroll-following. The wrapped-line layout must report more terminal lines
// than there are logical rows, and the cursor's terminal position must land on
// the wrapped row's first sub-line.
func TestDiffWrap(t *testing.T) {
	// One hunk with a single very long addition line.
	longLine := strings.Repeat("x", 200)
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,2 @@\n ctx\n+" + longLine + "\n"
	m := Model{width: 60, height: 24, view: viewLog, diffOpen: true, diffIsRevision: true, diffRev: "abc"}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	// Chunks: [0] file header, [1] long addition line. Start on the addition.
	m.diffCurChunk, m.diffCurLine = 1, 0
	m.computeDiffLayout()

	// The body must occupy more terminal lines than logical rows (wrapping
	// happened), and the layout's per-row counts must exceed 1 for the long
	// line.
	if m.diffBodyTotal() <= len(rows) {
		t.Errorf("bodyTotal %d <= rows %d; expected wrapping", m.diffBodyTotal(), len(rows))
	}
	longIdx := -1
	for i, r := range rows {
		if r.kind == rowLine && r.lineKind == "addition" && len(r.spans) > 0 && len(r.spans[0].text) >= 200 {
			longIdx = i
		}
	}
	if longIdx < 0 {
		t.Fatal("long addition row not found")
	}
	if m.rowCountTerm(longIdx) <= 1 {
		t.Errorf("long row wrapped count = %d, want >1", m.rowCountTerm(longIdx))
	}

	// The cursor's terminal body row must equal the wrapped first sub-line of
	// the long row (headLen + its start), not the 1:1 logical offset.
	headLen := m.diffHeadLen()
	wantCursor := headLen + m.rowStartTerm(longIdx)
	if got := m.diffCursorBodyRow(); got != wantCursor {
		t.Errorf("cursorBodyRow = %d, want %d (wrapped start)", got, wantCursor)
	}

	// The panel must still emit exactly `height` lines at this width.
	chunkFirst, chunkLast := m.diffChunkRange()
	out := renderDiffPanel(m.width, m.height, m.diffRev, 0, false, false, 0, m.diffDesc, true, rows, m.diffDigits, m.diffStatus, "", m.diffScrollY, m.diffCursorBodyRow(), chunkFirst, chunkLast, nil, splitView{}, false, nil, nil, -1, "")
	if len(out) != m.height {
		t.Errorf("wrapped panel lines = %d, want %d", len(out), m.height)
	}

	// Centering the cursor must keep the cursor's first sub-line in view.
	m.diffCenterCursor()
	row := m.diffCursorBodyRow()
	if row < m.diffScrollY || row >= m.diffScrollY+m.diffBodyHeight() {
		t.Errorf("wrapped cursor %d out of view [%d,%d)", row, m.diffScrollY, m.diffScrollY+m.diffBodyHeight())
	}
}

// TestDiffWrapNoMisplace checks that every visible terminal line produced by
// renderDiffPanel maps back to a valid logical row via the layout, i.e. no
// wrapped sub-line is dropped or duplicated relative to the computed layout.
func TestDiffWrapNoMisplace(t *testing.T) {
	longLine := "+" + strings.Repeat("y", 120) + "\n"
	raw := "diff --git a/a b/a\n+++ b/a\n@@ -1,1 +1,3 @@\n ctx\n" + longLine + longLine
	m := Model{width: 50, height: 30, view: viewLog, diffOpen: true}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.computeDiffLayout()
	headLen := m.diffHeadLen()

	// Walk every body terminal line and confirm rowAt round-trips within the
	// row's [start, start+count) range.
	for bl := 0; bl < m.diffBodyTotal(); bl++ {
		ri, sub := m.diffLayout.rowAt(bl)
		if ri < 0 || ri >= len(rows) {
			t.Fatalf("bodyLine %d -> rowIdx %d out of range", bl, ri)
		}
		if bl < m.rowStartTerm(ri) || bl >= m.rowStartTerm(ri)+m.rowCountTerm(ri) {
			t.Errorf("bodyLine %d not within row %d span [%d,%d)", bl, ri, m.rowStartTerm(ri), m.rowStartTerm(ri)+m.rowCountTerm(ri))
		}
		if sub != bl-m.rowStartTerm(ri) {
			t.Errorf("bodyLine %d sub = %d, want %d", bl, sub, bl-m.rowStartTerm(ri))
		}
	}
	_ = headLen
}

func stripANSI(lines []string) string {
	return ansi.Strip(strings.Join(lines, "\n"))
}

// TestCollapsedRowSet verifies that collapsedRowSet correctly identifies the
// body rows hidden by collapsed file headers.
func TestCollapsedRowSet(t *testing.T) {
	rows := renderDiff(sampleDiff)
	// File headers: foo.go at 0, new.txt at 10.
	collapsed := map[string]bool{"foo.go": true}
	hidden := collapsedRowSet(rows, collapsed)
	if hidden == nil {
		t.Fatal("expected non-nil hidden set")
	}
	// Rows 1-9 (hunk header + content) should be hidden; row 0 (header) and
	// rows 10+ (new.txt) should not.
	for i := 1; i <= 9; i++ {
		if !hidden[i] {
			t.Errorf("row %d should be hidden (inside collapsed foo.go)", i)
		}
	}
	if hidden[0] {
		t.Error("file header row 0 should not be hidden")
	}
	for i := 10; i < len(rows); i++ {
		if hidden[i] {
			t.Errorf("row %d should not be hidden (new.txt)", i)
		}
	}
}

// TestDiffFileHeaderForRow verifies that diffFileHeaderForRow finds the
// correct file header for any given row index.
func TestDiffFileHeaderForRow(t *testing.T) {
	rows := renderDiff(sampleDiff)
	// foo.go header is at 0, new.txt header is at 10.
	if got := diffFileHeaderForRow(rows, 5); got != 0 {
		t.Errorf("header for row 5 = %d, want 0 (foo.go)", got)
	}
	if got := diffFileHeaderForRow(rows, 12); got != 10 {
		t.Errorf("header for row 12 = %d, want 10 (new.txt)", got)
	}
	if got := diffFileHeaderForRow(rows, 0); got != 0 {
		t.Errorf("header for row 0 = %d, want 0", got)
	}
}

// TestComputeDiffChunksCollapsed verifies that computeDiffChunks skips body
// rows of collapsed files but keeps their file header as a navigable item.
func TestComputeDiffChunksCollapsed(t *testing.T) {
	rows := renderDiff(sampleDiff)
	// Collapse foo.go: foo.go header (1) + new.txt header (1) + new.txt chunk (2).
	chunks := computeDiffChunks(rows, 0, map[string]bool{"foo.go": true})
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3 (foo.hdr, new.hdr, new.chunk)", len(chunks))
	}
	if len(chunks[0]) != 1 || len(chunks[1]) != 1 || len(chunks[2]) != 2 {
		t.Errorf("chunk sizes = %d, %d, %d; want 1, 1, 2", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
	// Collapse new.txt: foo.go header (1) + foo.go chunk (5) + new.txt header (1).
	chunks = computeDiffChunks(rows, 0, map[string]bool{"new.txt": true})
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3 (foo.hdr, foo.chunk, new.hdr)", len(chunks))
	}
	if len(chunks[0]) != 1 || len(chunks[1]) != 5 || len(chunks[2]) != 1 {
		t.Errorf("chunk sizes = %d, %d, %d; want 1, 5, 1", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
	// Collapse both: only file headers remain.
	chunks = computeDiffChunks(rows, 0, map[string]bool{"foo.go": true, "new.txt": true})
	if len(chunks) != 2 {
		t.Errorf("chunks = %d, want 2 (both file headers)", len(chunks))
	}
}

// TestDiffCollapseToggle verifies that toggleDiffCollapse correctly toggles
// state, recomputes chunks, and keeps the cursor on the toggled file header.
func TestDiffCollapseToggle(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -1,3 +1,3 @@\n ctx\n-old\n+new\n" +
		"diff --git a/b.go b/b.go\n+++ b/b.go\n@@ -1,1 +1,2 @@\n+x\n+y\n"
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.computeDiffLayout()
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	// Chunks: [0] a.go header, [1] a.go chunk, [2] b.go header, [3] b.go chunk.
	m.diffCurChunk, m.diffCurLine = 0, 0 // cursor on a.go header

	// Cursor should be on a.go's file header.
	fileIdx, ok := m.cursorOnFileHeader()
	if !ok || fileIdx != 0 {
		t.Fatalf("cursorOnFileHeader = (%d, %v), want (0, true) (a.go)", fileIdx, ok)
	}
	m.toggleDiffCollapse(fileIdx)
	if !m.diffCollapsed["a.go"] {
		t.Error("a.go should be collapsed after toggle")
	}
	// a.go's body chunk is gone; remaining chunks: [0] a.go header, [1] b.go
	// header, [2] b.go chunk. Cursor should still be on a.go header (chunk 0).
	if len(m.diffChunks) != 3 {
		t.Fatalf("chunks after collapse = %d, want 3 (a.go hdr, b.go hdr, b.go chunk)", len(m.diffChunks))
	}
	if m.diffCurChunk != 0 {
		t.Errorf("cursor chunk = %d, want 0 (a.go header)", m.diffCurChunk)
	}

	// Expand a.go.
	m.toggleDiffCollapse(0)
	if m.diffCollapsed["a.go"] {
		t.Error("a.go should be expanded after second toggle")
	}
	if len(m.diffChunks) != 4 {
		t.Errorf("chunks after expand = %d, want 4", len(m.diffChunks))
	}
}

// TestDiffCollapseLayout verifies that the diff layout excludes collapsed
// file bodies, reducing the total terminal line count.
func TestDiffCollapseLayout(t *testing.T) {
	m := Model{width: 80, height: 24, view: viewLog, diffOpen: true}
	rows := renderDiff(sampleDiff)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.computeDiffLayout()
	fullTotal := m.diffBodyTotal()

	// Collapse foo.go (rows 1-9 hidden → 9 fewer terminal lines).
	m.diffCollapsed = map[string]bool{"foo.go": true}
	m.computeDiffLayout()
	collapsedTotal := m.diffBodyTotal()
	if collapsedTotal >= fullTotal {
		t.Errorf("collapsed total %d >= full total %d", collapsedTotal, fullTotal)
	}
	// The file header row (0) should still have count 1.
	if m.rowCountTerm(0) != 1 {
		t.Errorf("file header count = %d, want 1", m.rowCountTerm(0))
	}
	// Hidden rows should have count 0.
	for i := 1; i <= 9; i++ {
		if m.rowCountTerm(i) != 0 {
			t.Errorf("hidden row %d count = %d, want 0", i, m.rowCountTerm(i))
		}
	}
}

// TestDiffCollapseRendering verifies that renderDiffPanel still produces
// exactly `height` lines when files are collapsed, and that the collapsed
// file header shows the ▶ indicator.
func TestDiffCollapseRendering(t *testing.T) {
	rows := renderDiff(sampleDiff)
	collapsed := map[string]bool{"foo.go": true}
	out := renderDiffPanel(80, 24, "abcd", 0, false, false, 0, "", false, rows, maxLineDigits(rows), nil, "", 0, -1, -1, -1, collapsed, splitView{}, false, nil, nil, -1, "")
	if len(out) != 24 {
		t.Errorf("collapsed panel lines = %d, want 24", len(out))
	}
	// The collapsed file header (foo.go) should show ▶, and the expanded one
	// (new.txt) should show ▼.
	plain := stripANSI(out)
	if !strings.Contains(plain, "▶") {
		t.Error("collapsed panel missing ▶ indicator for foo.go")
	}
	if !strings.Contains(plain, "▼") {
		t.Error("collapsed panel missing ▼ indicator for new.txt")
	}
}

// TestDiffCollapseKeyboard verifies that pressing left/h/right/l in the diff
// panel toggles the collapse state only when the cursor is on a file header.
func TestDiffCollapseKeyboard(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -1,3 +1,3 @@\n ctx\n-old\n+new\n" +
		"diff --git a/b.go b/b.go\n+++ b/b.go\n@@ -1,1 +1,2 @@\n+x\n+y\n"
	m := Model{
		ready:    true,
		width:    100,
		height:   30,
		view:     viewLog,
		diffOpen: true,
	}
	rows := renderDiff(raw)
	m.diffRows = rows
	m.diffStatus = nil
	m.diffDigits = maxLineDigits(rows)
	m.computeDiffLayout()
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	// Chunks: [0] a.go header, [1] a.go changes, [2] b.go header, [3] b.go changes.
	// Cursor starts on a.go's file header (chunk 0).
	m.diffCurChunk, m.diffCurLine = 0, 0

	// Verify cursor is on a file header.
	if _, ok := m.cursorOnFileHeader(); !ok {
		t.Fatal("cursor should be on a.go file header at chunk 0")
	}

	// Press 'h' → should collapse a.go (cursor is on its header).
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = m2.(Model)
	if !m.diffCollapsed["a.go"] {
		t.Fatal("'h' did not collapse a.go when cursor was on its header")
	}

	// Press 'l' → should expand a.go (cursor still on its header).
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = m2.(Model)
	if m.diffCollapsed["a.go"] {
		t.Fatal("'l' did not expand a.go when cursor was on its header")
	}

	// Press left arrow → collapse again.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = m2.(Model)
	if !m.diffCollapsed["a.go"] {
		t.Fatal("left arrow did not collapse a.go")
	}

	// Press right arrow → expand.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = m2.(Model)
	if m.diffCollapsed["a.go"] {
		t.Fatal("right arrow did not expand a.go")
	}

	// Move cursor to the change chunk (chunk 1). h/l should still work from
	// within the code — they collapse/expand the file containing the cursor.
	m.diffMoveDown() // file header → chunk 1, line 0
	if _, ok := m.cursorOnFileHeader(); ok {
		t.Fatal("cursor should NOT be on a file header after moving to chunk 1")
	}
	// 'h' from within the code collapses the owning file.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = m2.(Model)
	if !m.diffCollapsed["a.go"] {
		t.Fatal("'h' should collapse a.go even when cursor is on a code line")
	}
	// 'l' from within the code expands the owning file.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = m2.(Model)
	if m.diffCollapsed["a.go"] {
		t.Fatal("'l' should expand a.go even when cursor is on a code line")
	}
}
