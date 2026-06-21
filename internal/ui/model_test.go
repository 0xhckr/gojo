package ui

import (
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
	if !strings.Contains(plain, "◉ gojo") {
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
