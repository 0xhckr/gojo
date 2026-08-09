package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

// conflictTestModel builds a Model with a synthetic two-file conflict state
// (already loaded, view open). File "a.txt" has two conflict hunks, "b.txt"
// one.
func conflictTestModel(t *testing.T) Model {
	t.Helper()
	mkFile := func(base, left, right string) conflictFile {
		cf := conflictFile{}
		cf.blocks = merge3Strings(base, left, right)
		for i := range cf.blocks {
			if cf.blocks[i].kind == mergeConflict {
				cf.conflicts = append(cf.conflicts, i)
			}
		}
		cf.deriveRows()
		return cf
	}
	// a.txt: two overlapping hunks.
	fa := mkFile("one\ntwo\nthree\nfour\nfive\n", "one\nL2\nthree\nfour\nL5\n", "one\nR2\nthree\nfour\nR5\n")
	fa.path = "a.txt"
	if len(fa.conflicts) != 2 {
		t.Fatalf("a.txt conflicts = %d, want 2", len(fa.conflicts))
	}
	// b.txt: one hunk.
	fb := mkFile("alpha\nbeta\ngamma\n", "alpha\nLEFT\ngamma\n", "alpha\nRIGHT\ngamma\n")
	fb.path = "b.txt"
	if len(fb.conflicts) != 1 {
		t.Fatalf("b.txt conflicts = %d, want 1", len(fb.conflicts))
	}

	m := NewModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.ready = true
	m.cfg = jj.Config{}
	m.entries = []jj.LogEntry{{ChangeID: "qpwvtszz", CommitID: "01234567"}}
	m.conflict = conflictState{
		rev:   "qpwvtszz",
		files: []conflictFile{fa, fb},
	}
	m.conflictOpen = true
	m.conflictFollowCursor()
	return m
}

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestConflictViewRender(t *testing.T) {
	m := conflictTestModel(t)
	plain := stripView(m)
	for _, want := range []string{
		"conflicts", "qpwvtszz",
		"a.txt", "b.txt",
		"side 1", "side 2",
		"conflict 1 of 2",
		"L2", "R2",
		"2 of 2 hunks open",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("view missing %q:\n%s", want, plain)
		}
	}
}

func TestConflictViewKeys(t *testing.T) {
	m := conflictTestModel(t)

	// Cursor starts on the first conflict of a.txt.
	if m.conflict.cursor != 0 {
		t.Fatalf("initial cursor %d, want 0", m.conflict.cursor)
	}

	// j moves to the next hunk.
	next, _ := m.Update(keyRune('j'))
	m = next.(Model)
	if m.conflict.cursor != 1 {
		t.Fatalf("cursor after j %d, want 1", m.conflict.cursor)
	}

	// l picks left and wraps back to the first unresolved (index 0).
	next, _ = m.Update(keyRune('l'))
	m = next.(Model)
	f := m.curConflictFile()
	if got := f.blocks[f.conflicts[1]].choice; got != resolutionLeft {
		t.Fatalf("hunk 2 choice %v, want left", got)
	}
	if m.conflict.cursor != 0 {
		t.Fatalf("cursor after pick %d, want 0 (wrapped to unresolved)", m.conflict.cursor)
	}

	// u unsets the pick again.
	next, _ = m.Update(keyRune('j'))
	m = next.(Model)
	next, _ = m.Update(keyRune('u'))
	m = next.(Model)
	f = m.curConflictFile()
	if got := f.blocks[f.conflicts[1]].choice; got != resolutionUnset {
		t.Fatalf("hunk 2 choice after u %v, want unset", got)
	}
	if m.conflict.cursor != 1 {
		t.Fatalf("cursor after u %d, want 1 (undo stays put)", m.conflict.cursor)
	}

	// enter with unresolved hunks refuses and explains.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if !strings.Contains(m.errMsg, "unresolved") {
		t.Fatalf("enter errMsg = %q, want unresolved complaint", m.errMsg)
	}

	// Resolve both: l then r → cursor wraps; enter would now apply (busy
	// spinner starts). Don't run the jj subprocess here.
	next, _ = m.Update(keyRune('l'))
	m = next.(Model)
	next, _ = m.Update(keyRune('r'))
	m = next.(Model)
	if n := m.curConflictFile().unresolved(); n != 0 {
		t.Fatalf("unresolved after picks %d, want 0", n)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter on fully-resolved file should produce a command")
	}
	if len(m.busy) == 0 {
		t.Fatal("enter on fully-resolved file should start the busy spinner")
	}

	// esc closes the view.
	next, _ = m.Update(keyRune('q'))
	m = next.(Model)
	if m.conflictOpen {
		t.Fatal("q should close the conflict view")
	}
}

func TestConflictViewFileSwitch(t *testing.T) {
	m := conflictTestModel(t)

	next, _ := m.Update(keyRune(']'))
	m = next.(Model)
	if m.conflict.cur != 1 {
		t.Fatalf("] cur %d, want 1", m.conflict.cur)
	}
	if m.conflict.cursor != 0 {
		t.Fatalf("cursor on switch %d, want 0", m.conflict.cursor)
	}
	plain := stripView(m)
	if !strings.Contains(plain, "conflict 1 of 1") {
		t.Fatalf("b.txt view should show conflict 1 of 1:\n%s", plain)
	}
	if !strings.Contains(plain, "LEFT") || !strings.Contains(plain, "RIGHT") {
		t.Fatalf("b.txt hunk contents missing:\n%s", plain)
	}

	// [ wraps back.
	next, _ = m.Update(keyRune('['))
	m = next.(Model)
	if m.conflict.cur != 0 {
		t.Fatalf("[ cur %d, want 0", m.conflict.cur)
	}
}

func TestConflictViewMouse(t *testing.T) {
	m := conflictTestModel(t)
	f := m.curConflictFile()

	// Find the first conflict-line row and click its LEFT pane → picks left.
	lineRow := -1
	for i, r := range f.rows {
		if r.kind == cConflictLine {
			lineRow = i
			break
		}
	}
	if lineRow < 0 {
		t.Fatal("no conflict line row in a.txt")
	}
	y := contentTopBarHeight + 2 + lineRow - m.conflict.scrollY
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: y}
	next, _ := m.Update(click)
	m = next.(Model)
	f = m.curConflictFile()
	if got := f.blocks[f.conflicts[0]].choice; got != resolutionLeft {
		t.Fatalf("left-pane click: choice %v, want left", got)
	}

	// Second hunk: click its RIGHT pane → picks right.
	lineRow2 := -1
	for i, r := range m.curConflictFile().rows {
		if r.kind == cConflictLine && r.block == m.curConflictFile().conflicts[1] {
			lineRow2 = i
			break
		}
	}
	y = contentTopBarHeight + 2 + lineRow2 - m.conflict.scrollY
	click = tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 60, Y: y}
	next, _ = m.Update(click)
	m = next.(Model)
	f = m.curConflictFile()
	if got := f.blocks[f.conflicts[1]].choice; got != resolutionRight {
		t.Fatalf("right-pane click: choice %v, want right", got)
	}

	// Tab bar: click the b.txt chip.
	_, spans := m.conflictTabBar()
	if len(spans) != 2 {
		t.Fatalf("tab spans %v, want 2", spans)
	}
	tx := (spans[1][0] + spans[1][1]) / 2
	click = tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: tx, Y: contentTopBarHeight}
	next, _ = m.Update(click)
	m = next.(Model)
	if m.conflict.cur != 1 {
		t.Fatalf("tab click: cur %d, want 1", m.conflict.cur)
	}
}

func TestConflictViewWheel(t *testing.T) {
	m := conflictTestModel(t)
	// Make the file tall enough to scroll.
	f := m.curConflictFile()
	var big []string
	for i := 0; i < 50; i++ {
		big = append(big, "ctx\n")
	}
	f.blocks = append([]mergeBlock{{kind: mergeContext, base: big, left: big, right: big}}, f.blocks...)
	f.conflicts = nil
	for i := range f.blocks {
		if f.blocks[i].kind == mergeConflict {
			f.conflicts = append(f.conflicts, i)
		}
	}
	f.deriveRows()
	m.conflict.files[0] = *f

	next, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 10, Y: 10})
	m = next.(Model)
	next, _ = m.Update(wheelTickMsg{}) // flush the coalescing tick
	m = next.(Model)
	if m.conflict.scrollY == 0 {
		t.Fatal("wheel down should scroll the conflict view")
	}
	next, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 10, Y: 10})
	m = next.(Model)
	next, _ = m.Update(wheelTickMsg{})
	m = next.(Model)
	if m.conflict.scrollY != 0 {
		t.Fatalf("wheel up should scroll back to top, got %d", m.conflict.scrollY)
	}
}

func TestConflictViewGeometry(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {80, 24}, {137, 40}, {20, 8}} {
		m := conflictTestModel(t)
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		view := m.View()
		got := strings.Count(view, "\n") + 1
		if got != size[1] {
			t.Fatalf("size %v: view has %d lines, want %d", size, got, size[1])
		}
		for _, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(ansi.Strip(line)); w > size[0] {
				t.Fatalf("size %v: row overflows width %d: %q", size, size[0], ansi.Strip(line))
			}
		}
	}
}

// ── End-to-end against a real jj repo ───────────────────────────────────────

// conflictUITestRepo builds a temp jj repo with one conflicted merge commit
// and returns a booted model pointed at it plus the merge change ID.
func conflictUITestRepo(t *testing.T) (Model, string) {
	t.Helper()
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		t.Skip("jj not in PATH")
	}
	repoDir := filepath.Join(t.TempDir(), "repo")
	jjRaw := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(jjPath, args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "JJ_USER=test", "JJ_EMAIL=test@test")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	changeID := func() string {
		return jjRaw("log", "-r", "@", "--no-graph", "-T", "change_id.short(8)", "--color", "never")
	}
	if out, err := exec.Command(jjPath, "git", "init", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("jj git init: %v\n%s", err, out)
	}
	write("file.txt", "one\ntwo\nthree\n")
	jjRaw("describe", "-m", "base")
	base := changeID()
	jjRaw("new", "-m", "left")
	write("file.txt", "one\nTWO-LEFT\nthree\n")
	left := changeID()
	jjRaw("edit", base)
	jjRaw("new", "-m", "right")
	write("file.txt", "one\nTWO-RIGHT\nthree\n")
	right := changeID()
	jjRaw("new", "-m", "merge", left, right)
	merge := changeID()

	m := NewModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, bootMsg{cfg: jj.Config{JJPath: jjPath, RepoRoot: repoDir}})
	return m, merge
}

func TestConflictEndToEnd(t *testing.T) {
	m, merge := conflictUITestRepo(t)

	// The refreshed log should flag the conflicted rev.
	if !strings.Contains(m.entries[0].ChangeID, merge[:3]) && !m.entries[0].HasConflict {
		// merge is newest (top of log) because it's the working copy.
		if len(m.entries) == 0 || !m.entries[0].HasConflict {
			t.Fatalf("top entry should carry the conflict badge: %+v", m.entries[0])
		}
	}

	// Open the conflict view through the real load command.
	msg := m.openConflictCmd(merge, 3)()
	next, _ := m.Update(msg)
	m = next.(Model)
	if !m.conflictOpen {
		t.Fatalf("conflict view did not open: errMsg=%q message=%q", m.errMsg, m.message)
	}
	f := m.curConflictFile()
	if f == nil || f.path != "file.txt" {
		t.Fatalf("current file %+v, want file.txt", f)
	}
	if len(f.conflicts) != 1 {
		t.Fatalf("conflicts %d, want 1", len(f.conflicts))
	}
	plain := stripView(m)
	if !strings.Contains(plain, "TWO-LEFT") || !strings.Contains(plain, "TWO-RIGHT") {
		t.Fatalf("view missing side contents:\n%s", plain)
	}

	// Pick left, then apply through the real resolve command.
	next, _ = m.Update(keyRune('l'))
	m = next.(Model)
	if n := m.curConflictFile().unresolved(); n != 0 {
		t.Fatalf("unresolved = %d", n)
	}
	msg = m.applyConflictCmd(*m.curConflictFile())()
	next, _ = m.Update(msg)
	m = next.(Model)
	if m.errMsg != "" {
		t.Fatalf("apply failed: %s", m.errMsg)
	}
	if m.conflictOpen {
		t.Fatal("view should close after the last file resolves")
	}
	if !strings.Contains(m.message, "all conflicts done") {
		t.Fatalf("message %q", m.message)
	}

	// The conflict is really gone and the content is the left side.
	r := jj.NewRunner(m.cfg)
	entries, err := r.Conflicts(merge)
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("conflicts remain: %v", entries)
	}
	got, err := r.FileShow(merge, "file.txt")
	if err != nil {
		t.Fatalf("FileShow: %v", err)
	}
	if got != "one\nTWO-LEFT\nthree\n" {
		t.Fatalf("resolved content %q", got)
	}
}
