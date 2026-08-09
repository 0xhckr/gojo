package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// TestDiffWrapLayoutMatchesRender pins the layout's wrap accounting to the
// renderer's: both must use the same content-prefix width, or a line just past
// the wrap point is drawn on two lines while the layout counts one, shifting
// every row below it.
func TestDiffWrapLayoutMatchesRender(t *testing.T) {
	digits := 3
	scrollW := 47
	for _, splitActive := range []bool{false, true} {
		prefixW := diffContentPrefixW(digits, splitActive)
		// spansWrapCount(text, w) wraps every time width is exceeded, so a
		// text of exactly layoutW+1 cells must count as 2 lines.
		text := strings.Repeat("x", scrollW-prefixW) + "y"
		row := diffRow{kind: rowLine, lineKind: "addition", sign: "+", oldNum: 0, newNum: 7, spans: []span{{text: text}}}
		if got := diffRowWrapCount(scrollW, digits, row, false, splitActive, false); got != 2 {
			t.Errorf("splitActive=%v: wrap count = %d, want 2 (prefixW=%d, scrollW=%d)", splitActive, got, prefixW, scrollW)
		}
		// Exactly at the budget: one line.
		text2 := text[:scrollW-prefixW]
		row.spans[0].text = text2
		if got := diffRowWrapCount(scrollW, digits, row, false, splitActive, false); got != 1 {
			t.Errorf("splitActive=%v: wrap count = %d, want 1", splitActive, got)
		}
	}
}

// TestDiffPanelGeometryMatrix is a regression sweep over terminal sizes,
// head sizes, diff shapes, and scroll offsets: the panel must always emit
// exactly height lines, each exactly width cells — a wider row soft-wraps
// and scrambles the screen, a shorter one leaves the scrollbar stranded.
func TestDiffPanelGeometryMatrix(t *testing.T) {
	var longLineDiff strings.Builder
	longLineDiff.WriteString("diff --git a/one.txt b/one.txt\n--- a/one.txt\n+++ b/one.txt\n@@ -1,4 +1,4 @@\n")
	longLineDiff.WriteString("-old\n+" + strings.Repeat("x", 173) + "\n")
	longLineDiff.WriteString(" unchanged context\n-new\n+new\n")

	var manyFilesDiff strings.Builder
	for f := 0; f < 12; f++ {
		fmt.Fprintf(&manyFilesDiff, "diff --git a/f%d.txt b/f%d.txt\n--- a/f%d.txt\n+++ b/f%d.txt\n@@ -1,3 +1,3 @@\n ctx\n", f, f, f, f)
		fmt.Fprintf(&manyFilesDiff, "-o%d\n+n%d\n", f, f)
	}

	smallDiff := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n-a\n+b\n"

	statusSets := [][]jj.StatusEntry{
		nil,
		{{Path: "a.txt", Status: jj.StatusModified}},
		{{Path: "a", Status: jj.StatusModified}, {Path: "b", Status: jj.StatusAdded}},
		func() []jj.StatusEntry {
			var s []jj.StatusEntry
			for i := 0; i < 25; i++ {
				s = append(s, jj.StatusEntry{Path: fmt.Sprintf("dir/file_%d.go", i), Status: jj.StatusModified})
			}
			return s
		}(),
	}

	diffs := []string{smallDiff, longLineDiff.String(), manyFilesDiff.String()}
	descs := []string{"", "single line", "multi\nline\nbody"}

	for _, width := range []int{40, 80, 120} {
		for _, height := range []int{10, 24, 40} {
			for di, raw := range diffs {
				for si, status := range statusSets {
					for _, desc := range descs {
						rows := renderDiff(raw)
						m := Model{width: width, height: height, view: viewLog, ready: true}
						m.entries = []jj.LogEntry{{ChangeID: "abc", CommitID: "def"}}
						m.diffOpen = true
						m.diffIsRevision = true
						m.diffRev = "abc"
						m.diffDesc = desc
						m.diffSrcRaw = raw
						m.diffRows = rows
						m.diffStatus = status
						m.diffDigits = maxLineDigits(rows)
						m.computeDiffLayout()
						m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
						m.diffChunksHead = m.diffHeadLen()

						for _, sy := range []int{0, m.diffMaxScroll() / 2, m.diffMaxScroll()} {
							m.diffScrollY = sy
							out := strings.Split(m.View(), "\n")
							tag := fmt.Sprintf("w=%d h=%d diff=%d status=%d desc=%q scrollY=%d", width, height, di, si, desc, sy)
							if len(out) != height {
								t.Fatalf("%s: %d lines, want %d", tag, len(out), height)
							}
							for i, l := range out {
								if w := lipgloss.Width(l); w != width {
									t.Errorf("%s line %d: width %d, want %d", tag, i, w, width)
								}
							}
						}
					}
				}
			}
		}
	}
}

// TestDiffTabsExpanded ensures tab-indented diff content (all Go source) can
// never reach the terminal raw: the UI expands tabs to tabStop spaces.
// A raw tab is counted as 0 cells by the width libraries but expanded to the
// next tab stop by the terminal, producing lines wider than the screen that
// soft-wrap and corrupt the whole alt screen.
func TestDiffTabsExpanded(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,3 +1,3 @@\n-func a() int {\n+\treturn 1\n+func a() int {\n \treturn 2\n"
	rows := renderDiff(raw)

	// The parsed spans keep the original bytes (split reconstruction needs
	// them); only rendering expands.
	foundTab := false
	for _, r := range rows {
		for _, sp := range r.spans {
			if strings.Contains(sp.text, "\t") {
				foundTab = true
			}
		}
	}
	if !foundTab {
		t.Fatal("test setup: no tab found in parsed spans")
	}

	// Render every row's sub-lines; none may contain a raw tab, and each must
	// measure exactly scrollW cells.
	for _, splitActive := range []bool{false, true} {
		digits := maxLineDigits(rows)
		scrollW := 100
		for ri, r := range rows {
			maxSub := diffRowWrapCount(scrollW, digits, r, false, splitActive, false)
			for sub := 0; sub < maxSub; sub++ {
				out := renderDiffRowSubLine(scrollW, digits, r, sub, nil, false, false, "", splitActive, false)
				if strings.Contains(out, "\t") {
					t.Errorf("row %d sub %d (split=%v) contains a raw tab", ri, sub, splitActive)
				}
				if w := lipgloss.Width(out); w != scrollW {
					t.Errorf("row %d sub %d (split=%v): width %d, want %d", ri, sub, splitActive, w, scrollW)
				}
			}
		}
	}
}

// TestBoundaryScrollbar exercises the geometry boundary where the
// diff body alone fits the viewport but head + body overflow it: the reserved
// scrollbar width must match the scrollbar's visibility. Rows wider than the
// terminal here mean the screen scrambles.
func TestBoundaryScrollbar(t *testing.T) {
	m := Model{width: 100, height: 30, view: viewLog, ready: true}
	m.entries = []jj.LogEntry{{ChangeID: "abc", CommitID: "def"}}

	// 10 status entries + single-line description → headLen = 16.
	var status []jj.StatusEntry
	for i := 0; i < 10; i++ {
		status = append(status, jj.StatusEntry{Path: fmt.Sprintf("dir/file_%02d.txt", i), Status: jj.StatusModified})
	}

	// Body rows land in (bodyH-headLen, bodyH]: the body alone fits (no
	// scrollbar reservation by the old probe) but head+body overflow
	// (scrollbar visible).
	var sb strings.Builder
	sb.WriteString("diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,10 +1,10 @@\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "-old %d\n+new %d\n", i, i)
	}
	rows := renderDiff(sb.String())

	m.diffOpen = true
	m.diffIsRevision = true
	m.diffRev = "abc"
	m.diffDesc = "subject"
	m.diffSrcRaw = sb.String()
	m.diffRows = rows
	m.diffStatus = status
	m.diffDigits = maxLineDigits(rows)
	m.computeDiffLayout()
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	m.diffChunksHead = m.diffHeadLen()

	bodyH := m.contentHeight() - 1
	headLen := m.diffHeadLen()
	total := m.diffLayout.total
	t.Logf("contentH=%d bodyH=%d headLen=%d layout.total=%d layout.scrollW=%d",
		m.contentHeight(), bodyH, headLen, total, m.diffLayout.scrollW)

	// The test only exercises anything if we're really in the boundary zone.
	if total > bodyH || total+headLen <= bodyH {
		t.Fatalf("not in boundary zone: total=%d headLen=%d bodyH=%d", total, headLen, bodyH)
	}

	out := m.View()
	for i, l := range strings.Split(out, "\n") {
		if got, want := lipgloss.Width(l), m.width; got != want {
			t.Errorf("line %d width %d, want %d: %q", i, got, want, l)
		}
	}
}

// TestLargeDiffLayout builds a real jj repo with a large changeset,
// opens its diff headlessly, and renders frames while scrolling, checking
// geometry invariants on every line.
func TestLargeDiffLayout(t *testing.T) {
	// Needs the jj binary (skipped in sandboxes like the nix checkPhase).
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skipf("jj not on PATH: %v", err)
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("jj", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	for i := 0; i < 15; i++ {
		content := strings.Repeat(fmt.Sprintf("line %d of file\n", i), 40)
		if err := os.WriteFile(fmt.Sprintf("%s/file_%02d.txt", dir, i), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("describe", "-m", "large changeset with many files\n\nand a multi-line description")

	cfg := jj.Config{JJPath: "jj", RepoRoot: dir}
	r := jj.NewRunner(cfg)

	m := NewModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	nm, _ = m.Update(bootMsg{cfg: cfg})
	m = nm.(Model)

	entries, err := r.Log(50)
	if err != nil {
		t.Fatal(err)
	}
	status, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	nm, _ = m.Update(refreshMsg{entries: entries, status: status})
	m = nm.(Model)

	e := &m.entries[0]
	nm, cmd := m.openRevisionDiff(e.ChangeID, e.CommitID, e.ChangeIDPrefixLen, e.Subject)
	m = nm.(Model)
	msg := cmd()
	nm, _ = m.Update(msg)
	m = nm.(Model)

	check := func(tag string) {
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) != m.height {
			t.Errorf("%s: rendered %d lines, want %d", tag, len(lines), m.height)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != m.width {
				t.Errorf("%s: line %d width %d, want %d: %q", tag, i, w, m.width, l)
			}
		}
	}

	check("open")

	for i := 0; i < 60; i++ {
		nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = nm.(Model)
		check(fmt.Sprintf("j%d", i))
	}

	// Simulate two poll reloads: one unchanged, then a fresh-render path.
	cmd2 := m.openDiffCmd(e.CommitID, e.ChangeID)
	nm, _ = m.Update(cmd2())
	m = nm.(Model)
	check("poll-unchanged")

	// Close and reopen. Render once while still loading (before the load
	// message lands) — this is what panics when a stale layout survives the
	// close.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = nm.(Model)
	nm, cmd3 := m.openRevisionDiff(e.ChangeID, e.CommitID, e.ChangeIDPrefixLen, e.Subject)
	m = nm.(Model)
	_ = m.View() // loading frame
	nm, _ = m.Update(cmd3())
	m = nm.(Model)
	check("reopen")
	if t.Failed() {
		out := m.View()
		t.Logf("frame:\n%s", out)
	}
}
