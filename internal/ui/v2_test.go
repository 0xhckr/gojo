package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyPress(text string) tea.KeyPressMsg {
	runes := []rune(text)
	if len(runes) == 0 {
		return tea.KeyPressMsg{}
	}
	return tea.KeyPressMsg{Code: runes[0], Text: text}
}

func keyCode(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func keyCtrl(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl}
}

func mousePress(button tea.MouseButton, x, y int) tea.MouseMsg {
	mouse := tea.Mouse{Button: button, X: x, Y: y}
	if button >= tea.MouseWheelUp && button <= tea.MouseWheelRight {
		return tea.MouseWheelMsg(mouse)
	}
	return tea.MouseClickMsg(mouse)
}

func mouseRelease(button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseReleaseMsg{Button: button, X: x, Y: y}
}

func mouseMotion(button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMotionMsg{Button: button, X: x, Y: y}
}

func TestPasteInsertsTextWithoutDispatchingBindings(t *testing.T) {
	m := mouseTestModel()
	m.searchMode = true
	m.searchFilter()

	next, _ := m.Update(tea.PasteMsg{Content: "q search"})
	m = next.(Model)
	if !m.searchMode {
		t.Fatal("pasting q cancelled search")
	}
	if m.searchQuery != "q search" {
		t.Fatalf("searchQuery = %q, want pasted text", m.searchQuery)
	}

	m.searchMode = false
	m.bookmarkMode = true
	m.bookmarkAction = actCreate
	next, _ = m.Update(tea.PasteMsg{Content: "feature/name"})
	m = next.(Model)
	if m.bookmarkInput != "feature/name" {
		t.Fatalf("bookmarkInput = %q, want pasted text", m.bookmarkInput)
	}

	next, _ = m.Update(tea.PasteMsg{Content: "\nnext\t\x1b[31m\x7f"})
	m = next.(Model)
	if m.bookmarkInput != "feature/name next [31m" {
		t.Fatalf("bookmarkInput = %q after control characters", m.bookmarkInput)
	}
}

func TestViewDeclaresTerminalCapabilities(t *testing.T) {
	v := NewModel().View()
	if !v.AltScreen || !v.ReportFocus || v.MouseMode != tea.MouseModeAllMotion {
		t.Fatalf("view capabilities: alt=%v focus=%v mouse=%v", v.AltScreen, v.ReportFocus, v.MouseMode)
	}
}

func TestKeyReleaseIsIgnored(t *testing.T) {
	m := mouseTestModel()
	next, _ := m.Update(tea.KeyReleaseMsg{Code: 'j', Text: "j"})
	if got := next.(Model).cursor; got != m.cursor {
		t.Fatalf("cursor = %d after key release, want %d", got, m.cursor)
	}
}
