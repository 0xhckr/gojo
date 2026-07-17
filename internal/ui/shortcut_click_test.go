package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// helpBarY computes the terminal Y of the first help bar row for a model
// with suggestionsVisible == false.
func helpBarY(m Model) int {
	return m.helpBarStartY()
}

// statusY computes the terminal Y of the first status bar row.
func statusY(m Model) int {
	return m.statusBarStartY()
}

// TestShortcutClickLogHelpBar verifies that clicking a help bar shortcut in
// the log view dispatches the corresponding key press.
func TestShortcutClickLogHelpBar(t *testing.T) {
	m := mouseTestModel()
	// "describe" has key "d". Find its position in the help bar.
	helpItems := m.helpBarItems()
	spans := computeMenuSpans(m.width, " ", "  ", helpItems, m.helpBarStartY())

	var descSpan *menuSpan
	for i := range spans {
		if spans[i].keyHint == "d" {
			descSpan = &spans[i]
			break
		}
	}
	if descSpan == nil {
		t.Fatal("no 'd' shortcut found in help bar")
	}

	m2, cmd := m.Update(leftClick(descSpan.x1, descSpan.y))
	m = m2.(Model)
	if cmd == nil {
		// describe uses ExecProcess so should produce a command.
		// But without a runner, handleLogKey for "d" with a selected entry
		// returns describeCmd which is a tea.Cmd.
		t.Fatal("clicking 'describe' shortcut did not produce a command")
	}
}

// TestShortcutClickBookmarkMenu verifies that clicking a status bar menu item
// in bookmark mode dispatches the key (e.g. "c" for create).
func TestShortcutClickBookmarkMenu(t *testing.T) {
	m := mouseTestModel()
	m.bookmarkMode = true
	m.bookmarkAction = ""

	spans := computeMenuSpans(m.width, " [bookmark mode] ", " ", bookmarkMenuItems, m.statusBarStartY())

	var createSpan *menuSpan
	for i := range spans {
		if spans[i].keyHint == "c" {
			createSpan = &spans[i]
			break
		}
	}
	if createSpan == nil {
		t.Fatal("no 'c' shortcut found in bookmark menu")
	}

	m2, _ := m.Update(leftClick(createSpan.x1, createSpan.y))
	m = m2.(Model)
	if m.bookmarkAction != "c" {
		t.Fatalf("bookmarkAction = %q, want 'c'", m.bookmarkAction)
	}
}

// TestShortcutClickGitMenu verifies that clicking a git mode menu item works.
func TestShortcutClickGitMenu(t *testing.T) {
	m := mouseTestModel()
	m.gitMode = true

	spans := computeMenuSpans(m.width, " [git mode] ", " ", gitMenuItems, m.statusBarStartY())
	var fetchSpan *menuSpan
	for i := range spans {
		if spans[i].keyHint == "f" {
			fetchSpan = &spans[i]
			break
		}
	}
	if fetchSpan == nil {
		t.Fatal("no 'f' shortcut found in git menu")
	}

	m2, _ := m.Update(leftClick(fetchSpan.x1, fetchSpan.y))
	m = m2.(Model)
	// Git fetch dispatches a busyActionCmd which would need a runner. Just
	// verify the git mode was exited (handleGitKey "f" sets gitMode=false).
	if m.gitMode {
		t.Error("git mode should have been exited after fetch click")
	}
}

// TestShortcutClickMiss verifies a click that doesn't hit a shortcut is not
// intercepted.
func TestShortcutClickMiss(t *testing.T) {
	m := mouseTestModel()

	// Click in the content area (not help/status bar).
	m2, cmd := m.Update(leftClick(10, 5))
	m = m2.(Model)
	// Should select entry 1, not dispatch a shortcut.
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	if cmd != nil {
		t.Error("content-area click should not produce a command")
	}
}

// TestShortcutClickEsc verifies clicking the "cancel"/"esc" item in a
// sub-menu mode dismisses it.
func TestShortcutClickEsc(t *testing.T) {
	m := mouseTestModel()
	m.bookmarkMode = true
	m.bookmarkAction = ""

	spans := computeMenuSpans(m.width, " [bookmark mode] ", " ", bookmarkMenuItems, m.statusBarStartY())
	var escSpan *menuSpan
	for i := range spans {
		if spans[i].keyHint == "esc" {
			escSpan = &spans[i]
			break
		}
	}
	if escSpan == nil {
		t.Fatal("no 'esc' shortcut found in bookmark menu")
	}

	m2, _ := m.Update(leftClick(escSpan.x1, escSpan.y))
	m = m2.(Model)
	if m.bookmarkMode {
		t.Error("bookmark mode should have been dismissed after esc click")
	}
}

// TestShortcutClickDiffHelpBar verifies clicking a help bar shortcut in the
// diff view dispatches correctly.
func TestShortcutClickDiffHelpBar(t *testing.T) {
	m := diffClickTestModel(t)

	helpItems := m.helpBarItems()
	spans := computeMenuSpans(m.width, " ", "  ", helpItems, m.helpBarStartY())

	// "q close" should close the diff.
	var qSpan *menuSpan
	for i := range spans {
		if spans[i].keyHint == "q" {
			qSpan = &spans[i]
			break
		}
	}
	if qSpan == nil {
		t.Fatal("no 'q' shortcut found in diff help bar")
	}

	m2, _ := m.Update(leftClick(qSpan.x1, qSpan.y))
	m = m2.(Model)
	if m.diffOpen {
		t.Error("diff should have been closed after 'q' shortcut click")
	}
}

// TestKeyMsgFromHint verifies the key hint to KeyMsg conversion.
func TestKeyMsgFromHint(t *testing.T) {
	cases := []struct {
		hint string
		want string
	}{
		{"d", "d"},
		{"D", "D"},
		{"⏎", "enter"},
		{"↑", "up"},
		{"↓", "down"},
		{"esc", "esc"},
		{"space", " "},
		{"q", "q"},
	}
	for _, c := range cases {
		msg, _ := keyMsgFromHint(c.hint)
		if got := msg.String(); got != c.want {
			t.Errorf("keyMsgFromHint(%q).String() = %q, want %q", c.hint, got, c.want)
		}
	}
}

var _ tea.KeyMsg
