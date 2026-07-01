package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

// stripView renders the model and strips ANSI for assertions.
func stripView(m Model) string {
	return ansi.Strip(m.View())
}

// step applies a message and synchronously drains plain (closure) commands
// so deterministic flows (boot → refresh) settle. Batch/Tick/Exec commands
// are not executed.
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd == nil {
		return m
	}
	// Best-effort: run the command if it is a simple producer.
	if msg := cmd(); msg != nil {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	return m
}

func bootedModel(t *testing.T) Model {
	t.Helper()
	cfg, err := jj.LoadConfig()
	if err != nil {
		t.Skipf("not in a jj repo: %v", err)
	}
	m := NewModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, bootMsg{cfg: cfg})
	return m
}

func TestFileViewPickerBlameHistory(t *testing.T) {
	m := bootedModel(t)

	// Enter the file view (synthetic file list, no subprocess needed).
	m.view = viewFile
	m = step(t, m, fileListMsg{files: []string{"a/b.go", "a/c.go", "main.go"}})

	if m.fileView.phase != filePicker {
		t.Fatalf("expected picker phase, got %v", m.fileView.phase)
	}
	// Tree: dir "a" (expanded) + b.go + c.go, then root file main.go.
	if len(m.fileView.rows) < 4 {
		t.Fatalf("expected >=4 visible rows, got %d (%+v)", len(m.fileView.rows), m.fileView.rows)
	}
	// Move down to the first file inside a/ (index 1) and open it.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.fileView.rows[m.fileView.cursor].node.full == "" {
		t.Fatalf("cursor not on a file: %+v", m.fileView.rows[m.fileView.cursor].node)
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// Annotate result arrives asynchronously; feed it directly.
	// Lines 1-2 are commit mwqwmwpp (multi-line section, has a description);
	// line 3 is commit kxmyusxx (single-line section, has a description, so it
	// expands and highlights line 4 too); line 4 is commit nwmqpxkp.
	ann := []jj.AnnotateLine{
		{ChangeID: "mwqwmwpp", CommitID: "b2fe214a", Author: "hackr@hackr.sh", LineNo: 1, Description: "Rewrite gojo", Text: "package main"},
		{ChangeID: "mwqwmwpp", CommitID: "b2fe214a", Author: "hackr@hackr.sh", LineNo: 2, Description: "Rewrite gojo", Text: ""},
		{ChangeID: "kxmyusxx", CommitID: "aa0100ff", Author: "al@ice.gg", LineNo: 3, Description: "add main", Text: "func main() {}"},
		{ChangeID: "nwmqpxkp", CommitID: "11ff00bb", Author: "bo@b.io", LineNo: 4, Description: "doc", Text: "// done"},
	}
	m = step(t, m, fileAnnotateMsg{path: "a/b.go", lines: ann})
	if m.fileView.phase != fileBlame {
		t.Fatalf("expected blame phase, got %v", m.fileView.phase)
	}

	// The rendered blame view shows the email (not the truncated username)
	// and the commit description on the line below it.
	view := stripView(m)
	if !strings.Contains(view, "hackr@hackr.sh") {
		t.Fatalf("blame view missing email: %s", view)
	}
	if !strings.Contains(view, "Rewrite gojo") {
		t.Fatalf("blame view missing description: %s", view)
	}

	// Status bar shows the focused line's commit (git-blame style).
	bar := m.renderFileStatusBar()[0]
	if !strings.Contains(bar, "mwqwmwpp") || !strings.Contains(bar, "hackr@hackr.sh") {
		t.Fatalf("status bar missing blame info: %s", bar)
	}

	// Move down to line 3 (single-line section kxmyusxx). Its description
	// ("add main") shows on the line below and the section expands.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	bar = m.renderFileStatusBar()[0]
	if !strings.Contains(bar, "kxmyusxx") {
		t.Fatalf("status bar didn't follow cursor to new commit: %s", bar)
	}
	view = stripView(m)
	if !strings.Contains(view, "add main") {
		t.Fatalf("blame view missing single-line section description: %s", view)
	}

	// 'h' opens file history.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = step(t, m, fileHistoryMsg{entries: []jj.LogEntry{{ChangeID: "kxmyusxx", CommitID: "aa0100ff", Subject: "edit b"}}})
	if m.fileView.phase != fileHistory {
		t.Fatalf("expected history phase, got %v", m.fileView.phase)
	}

	// esc returns to blame; q from blame steps back to the picker (like esc),
	// and q from the picker then leaves the file view entirely.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.fileView.phase != fileBlame {
		t.Fatalf("expected blame phase after esc from history, got %v", m.fileView.phase)
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.fileView.phase != filePicker {
		t.Fatalf("expected picker phase after q from blame, got %v", m.fileView.phase)
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.view != viewLog {
		t.Fatalf("expected to return to log view, got %v", m.view)
	}
}

func TestViewBootAndLayout(t *testing.T) {
	m := bootedModel(t)

	if !m.ready {
		t.Fatal("model not ready after boot")
	}
	if len(m.entries) == 0 {
		t.Fatal("no log entries loaded")
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 30 {
		t.Errorf("view has %d lines, want 30", len(lines))
	}

	plain := ansi.Strip(view)
	if !strings.Contains(plain, "◆ gojo") {
		t.Error("top bar missing app name")
	}
	// Help bar keybinds present.
	for _, want := range []string{"diff", "describe", "bookmark", "git", "quit"} {
		if !strings.Contains(plain, want) {
			t.Errorf("help bar missing %q", want)
		}
	}
	// First change id should render in the log.
	if !strings.Contains(plain, m.entries[0].ChangeID) {
		t.Errorf("log missing change id %q", m.entries[0].ChangeID)
	}
}

func TestNavigationAndHelp(t *testing.T) {
	m := bootedModel(t)

	// Toggle help.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.view != viewHelp {
		t.Fatal("? did not open help")
	}
	plain := stripView(m)
	// Top of the help page: title bar + first sections.
	if !strings.Contains(plain, "gojo help") || !strings.Contains(plain, "Global") || !strings.Contains(plain, "Log View") {
		t.Error("help view content missing")
	}

	// Scroll help down then close.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.view != viewLog {
		t.Error("q did not close help")
	}

	// Cursor down should move within bounds.
	start := m.cursor
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if len(m.entries) > 1 && m.cursor != start+1 {
		t.Errorf("cursor = %d, want %d", m.cursor, start+1)
	}
}

func TestBookmarkModeRendering(t *testing.T) {
	m := bootedModel(t)

	// Enter bookmark mode.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if !m.bookmarkMode {
		t.Fatal("b did not enter bookmark mode")
	}
	if !strings.Contains(stripView(m), "[bookmark mode]") {
		t.Error("status bar missing bookmark menu")
	}

	// Choose create, type a name.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	for _, r := range "feat" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.bookmarkInput != "feat" {
		t.Errorf("bookmark input = %q, want feat", m.bookmarkInput)
	}
	if !strings.Contains(stripView(m), "create: feat") {
		t.Error("status bar missing create prompt with input")
	}

	// Escape clears the action, escape again exits bookmark mode.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.bookmarkAction != "" {
		t.Error("escape did not clear action")
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.bookmarkMode {
		t.Error("escape did not exit bookmark mode")
	}
}

func TestRebaseModeFlow(t *testing.T) {
	m := bootedModel(t)
	if len(m.entries) < 2 {
		t.Skip("need at least two revisions")
	}

	// Pick up the selected commit as the rebase source.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !m.rebaseMode {
		t.Fatal("r did not enter rebase mode")
	}
	if m.rebaseSource != 0 {
		t.Errorf("rebaseSource = %d, want 0", m.rebaseSource)
	}
	if m.rebaseDest == m.rebaseSource {
		t.Error("destination should not start equal to source")
	}
	plain := stripView(m)
	if !strings.Contains(plain, "[rebase]") {
		t.Error("status bar missing rebase menu")
	}
	if !strings.Contains(plain, "● moving") || !strings.Contains(plain, "◀ onto") {
		t.Error("log missing source/destination markers")
	}

	// Toggle scope (-r → -s) and cycle placement (onto → after).
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m.rebaseSubtree {
		t.Error("s did not toggle subtree scope")
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.rebasePlace != 1 {
		t.Errorf("rebasePlace = %d, want 1 (after)", m.rebasePlace)
	}
	if !strings.Contains(stripView(m), "◀ after") {
		t.Error("destination marker did not update to 'after'")
	}

	// Escape cancels without leaving rebase mode active.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.rebaseMode {
		t.Error("esc did not exit rebase mode")
	}
}

func TestSquashModeFlow(t *testing.T) {
	m := bootedModel(t)
	if len(m.entries) < 2 {
		t.Skip("need at least two revisions")
	}

	// Pick up the selected commit as the squash source.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m.squashMode {
		t.Fatal("s did not enter squash mode")
	}
	if m.squashSource != 0 {
		t.Errorf("squashSource = %d, want 0", m.squashSource)
	}
	if m.squashDest == m.squashSource {
		t.Error("destination should not start equal to source")
	}
	plain := stripView(m)
	if !strings.Contains(plain, "[squash]") {
		t.Error("status bar missing squash menu")
	}
	if !strings.Contains(plain, "● squashing") || !strings.Contains(plain, "◀ into") {
		t.Error("log missing source/destination markers")
	}

	// Destination moves within bounds.
	dest := m.squashDest
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.squashDest < 0 || m.squashDest >= len(m.entries) {
		t.Errorf("squashDest out of bounds: %d", m.squashDest)
	}
	_ = dest

	// Escape cancels without leaving squash mode active.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.squashMode {
		t.Error("esc did not exit squash mode")
	}
}

func TestGitModeRendering(t *testing.T) {
	m := bootedModel(t)
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !m.gitMode {
		t.Fatal("g did not enter git mode")
	}
	if !strings.Contains(stripView(m), "[git mode]") {
		t.Error("status bar missing git menu")
	}
	// Enter remote submode.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !m.remoteMode {
		t.Fatal("r did not enter remote mode")
	}
	if !strings.Contains(stripView(m), "[git > remote]") {
		t.Error("status bar missing remote menu")
	}
}

// TestElevationPromptFlow checks that an elevatable failure surfaces a
// "retry with --flag?" prompt, that confirming runs the elevated retry, and
// that cancelling clears it.
func TestElevationPromptFlow(t *testing.T) {
	m := bootedModel(t)

	// Simulate an action failing with an immutability error that carries an
	// elevation request.
	retried := false
	req := &elevReq{
		flag:   "--ignore-immutable",
		reason: "target is immutable",
		retry:  func() tea.Cmd { retried = true; return nil },
	}
	m = step(t, m, actionDoneMsg{err: errors.New("is immutable"), elev: req})
	if m.pendingElev == nil {
		t.Fatal("elevation failure did not set pendingElev")
	}
	plain := stripView(m)
	if !strings.Contains(plain, "retry with") || !strings.Contains(plain, "--ignore-immutable") {
		t.Errorf("status bar missing elevation prompt: %q", plain)
	}
	if !strings.Contains(plain, "y confirm") || !strings.Contains(plain, "cancel") {
		t.Errorf("status bar missing confirm/cancel hints: %q", plain)
	}

	// Confirming runs the elevated retry.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.pendingElev != nil {
		t.Error("confirm did not clear pendingElev")
	}
	if !retried {
		t.Error("confirm did not run the elevated retry closure")
	}
}

// TestElevationCancel checks that any non-confirm key cancels the prompt
// without running the retry.
func TestElevationCancel(t *testing.T) {
	m := bootedModel(t)
	retried := false
	req := &elevReq{
		flag:   "--allow-backwards",
		reason: "backwards",
		retry:  func() tea.Cmd { retried = true; return nil },
	}
	m = step(t, m, actionDoneMsg{err: errors.New("is immutable"), elev: req})
	if m.pendingElev == nil {
		t.Fatal("elevation failure did not set pendingElev")
	}

	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.pendingElev != nil {
		t.Error("esc did not cancel pendingElev")
	}
	if retried {
		t.Error("cancel should not run the retry closure")
	}
}

// TestDescribeImmutablePromptsElevation checks that pressing 'd' (editor
// describe) on an immutable commit offers an elevation retry instead of
// launching the editor and failing with an uncapturable error.
func TestDescribeImmutablePromptsElevation(t *testing.T) {
	m := bootedModel(t)
	if len(m.entries) == 0 {
		t.Skip("no entries")
	}
	// Force the selected entry to look immutable (the editor flow can't read
	// jj's error back, so detection relies on this flag).
	m.entries[m.cursor].IsImmutable = true
	changeID := m.entries[m.cursor].ChangeID

	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.pendingElev == nil {
		t.Fatal("'d' on immutable commit did not surface an elevation prompt")
	}
	if m.pendingElev.flag != "--ignore-immutable" {
		t.Errorf("elev flag = %q, want --ignore-immutable", m.pendingElev.flag)
	}
	plain := stripView(m)
	if !strings.Contains(plain, "retry with") || !strings.Contains(plain, "--ignore-immutable") {
		t.Errorf("status bar missing elevation prompt: %q", plain)
	}

	// Confirming builds the elevated describe command (ExecProcess) for the
	// same change id.
	var cmd tea.Cmd
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	mm := m2.(Model)
	if mm.pendingElev != nil {
		t.Error("confirm did not clear pendingElev")
	}
	if cmd == nil {
		t.Fatal("confirm did not produce an elevated describe command")
	}
	// We can't run the ExecProcess in a headless test; just confirm a command
	// was issued. The change id is baked into describeCmd, not inspectable here.
	_ = changeID
}
