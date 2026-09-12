package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

func stickyDiffModel(path string, width, height int) Model {
	var raw strings.Builder
	for _, name := range []string{path, "second.go"} {
		fmt.Fprintf(&raw, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -0,0 +1,60 @@\n", name, name, name, name)
		for i := 0; i < 60; i++ {
			fmt.Fprintf(&raw, "+line-%02d\n", i)
		}
	}
	m := Model{width: width, height: height, ready: true, view: viewLog, diffOpen: true, diffRev: "abc"}
	m.entries = []jj.LogEntry{{ChangeID: "abc", CommitID: "def"}}
	m.diffRows = renderDiff(raw.String())
	m.diffDigits = maxLineDigits(m.diffRows)
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), nil)
	m.diffChunksHead = m.diffHeadLen()
	m.computeDiffLayout()
	return m
}

func stickyDiffScreen(m Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

func TestDiffStickyScrollAndClick(t *testing.T) {
	m := stickyDiffModel("first.go", 80, 18)
	const top = contentTopBarHeight + 1
	step := func(msg tea.Msg) {
		t.Helper()
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	wheel := func() {
		step(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 20, Y: top + 3})
		step(wheelTickMsg{})
	}
	assertLine := func(y int, text string) {
		t.Helper()
		if line := stickyDiffScreen(m)[y]; !strings.Contains(line, text) {
			t.Fatalf("screen row %d = %q, want %q (scroll %d)", y, line, text, m.diffScrollY)
		}
	}
	assertLine(top, "status")
	for i := 0; i < m.diffHeadLen()+15; i++ {
		wheel()
	}
	assertLine(top, "▼ first.go")
	step(keyPress("pgdown"))
	assertLine(top, "▼ first.go")
	if m.diffCurChunk != 0 {
		t.Fatal("viewport scrolling moved the keyboard cursor")
	}

	// The next header stays visible as it reaches the sticky header, then
	// replaces it without duplicating the filename.
	second := m.diffHeadLen() + m.rowStartTerm(m.diffChunks[2][0]-m.diffHeadLen())
	for m.diffScrollY < second-1 {
		wheel()
	}
	assertLine(top, "▼ first.go")
	assertLine(top+1, "▼ second.go")
	wheel()
	assertLine(top, "▼ second.go")
	if strings.Contains(stickyDiffScreen(m)[top+1], "second.go") {
		t.Fatal("file header duplicated at the boundary")
	}
	for m.diffScrollY < m.diffMaxScroll() {
		wheel()
	}
	assertLine(top, "▼ second.go")
	assertLine(top+m.diffBodyHeight()-1, "line-59")

	// A code click below the overlay selects the displayed line; clicking
	// the pinned filename collapses its file, even with its original offscreen.
	step(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: top + 1})
	ri := m.diffChunks[m.diffCurChunk][m.diffCurLine] - m.diffHeadLen()
	assertLine(top+1, m.diffRows[ri].spans[0].text)
	step(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: top})
	if !m.diffCollapsed["second.go"] || m.diffCollapsed["first.go"] {
		t.Fatalf("sticky click collapsed wrong file: %v", m.diffCollapsed)
	}
	if _, ok := m.diffRowAtMouseY(top - 1); ok {
		t.Fatal("title bar mapped to an offscreen diff row")
	}
}

func TestDiffStickyWrappedHeaderAndCursor(t *testing.T) {
	path := "start/" + strings.Repeat("x", 40) + "/file.go"
	m := stickyDiffModel(path, 32, 13)
	const top = contentTopBarHeight + 1
	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyPress("j"))
		m = next.(Model)
	}
	screen := stickyDiffScreen(m)
	if !strings.Contains(screen[top], "▼ start/") || !strings.Contains(screen[top+1], "file.go") {
		t.Fatalf("wrapped sticky filename missing:\n%s", strings.Join(screen, "\n"))
	}
	for _, key := range []string{"j", "k"} {
		next, _ := m.Update(keyPress(key))
		m = next.(Model)
		ri := m.diffChunks[m.diffCurChunk][m.diffCurLine] - m.diffHeadLen()
		text := m.diffRows[ri].spans[0].text
		if !strings.Contains(m.View().Content, text) {
			t.Fatalf("%s: sticky header hid cursor text %q", key, text)
		}
	}
	if ri, ok := m.diffRowAtMouseY(top + 1); !ok || ri != 0 {
		t.Fatalf("wrapped sticky header mouse target = (%d, %v), want (0, true)", ri, ok)
	}

	// A very short viewport still leaves a row for the selected code line.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 32, Height: 11})
	m = next.(Model)
	ri := m.diffChunks[m.diffCurChunk][m.diffCurLine] - m.diffHeadLen()
	screen = stickyDiffScreen(m)
	if !strings.Contains(screen[top], "▼ start/") || !strings.Contains(screen[top+1], m.diffRows[ri].spans[0].text) {
		t.Fatalf("short viewport lost filename or cursor:\n%s", strings.Join(screen, "\n"))
	}
}
