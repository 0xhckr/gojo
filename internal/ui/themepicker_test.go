package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"gojo/internal/jj"
)

// themeBootModel boots headlessly with a small theme list (no jj repo needed:
// boot error path is fine for the picker — no, we want ready state).
func themeBootModel(t *testing.T, cfgTheme string, themes []Theme) Model {
	t.Helper()
	m := NewModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if len(themes) == 0 {
		themes = compiledThemes()
	}
	// Feed a bootMsg with a staged config; the runner's jj path may not
	// exist, but refresh failures only surface as errMsg text.
	m = step(t, m, bootMsg{cfg: jjConfigForTheme(cfgTheme), themes: themes})
	return m
}

func jjConfigForTheme(theme string) jj.Config {
	return jj.Config{Theme: theme}
}

func TestThemeBootAppliesConfig(t *testing.T) {
	defer applyTheme(gojoTheme())
	m := themeBootModel(t, "terminal", nil)
	if m.themeName != "terminal" {
		t.Fatalf("themeName = %q, want terminal", m.themeName)
	}
	if colText != nil {
		t.Fatalf("terminal theme should leave colText nil")
	}

	// Unknown theme falls back to gojo.
	m = themeBootModel(t, "no-such-theme", nil)
	if m.themeName != "gojo" {
		t.Fatalf("themeName = %q, want gojo fallback", m.themeName)
	}
}

func TestThemePickerFlow(t *testing.T) {
	defer applyTheme(gojoTheme())

	// Stub out persistence.
	var saved []string
	old := persistTheme
	persistTheme = func(id string) error { saved = append(saved, id); return nil }
	defer func() { persistTheme = old }()

	m := themeBootModel(t, "", nil) // gojo
	if m.themeName != "gojo" {
		t.Fatalf("themeName = %q", m.themeName)
	}

	// Open the picker from the log view.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	if !m.themeOpen {
		t.Fatal("picker did not open on T")
	}
	if !strings.Contains(stripView(m), "gojo themes") {
		t.Fatal("picker view not rendered")
	}

	// Move down: live-previews terminal (index 1) — observable via colText.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.themeCursor != 1 {
		t.Fatalf("themeCursor = %d", m.themeCursor)
	}
	if colText != nil {
		t.Fatal("preview did not apply: colText should be nil under terminal")
	}

	// Apply with enter: commits + saves.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.themeOpen {
		t.Fatal("picker should close on enter")
	}
	if m.themeName != "terminal" {
		t.Fatalf("themeName = %q, want terminal", m.themeName)
	}
	if len(saved) != 1 || saved[0] != "terminal" {
		t.Fatalf("persistTheme got %v", saved)
	}

	// Reopen, preview something else, cancel: original theme restored.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	if m.themeCursor != 1 {
		t.Fatalf("picker should open on the active theme, cursor = %d", m.themeCursor)
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}) // up → gojo
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.themeOpen {
		t.Fatal("picker should close on esc")
	}
	if m.themeName != "terminal" {
		t.Fatalf("cancel lost theme: %q", m.themeName)
	}
	if colText != nil {
		t.Fatal("cancel did not restore terminal theme")
	}
}

func TestThemePickerWheel(t *testing.T) {
	defer applyTheme(gojoTheme())
	m := themeBootModel(t, "", loadThemes("", "", filepath.Join("..", "..")))
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("T")})
	m = step(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.themeCursor != 1 {
		t.Fatalf("wheel down should move cursor, got %d", m.themeCursor)
	}
	// Further burst steps accumulate, then flush as one batch.
	m = step(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	m = step(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	m = step(t, m, wheelTickMsg{})
	if m.themeCursor != 3 {
		t.Fatalf("batched wheel burst should land at 3, got %d", m.themeCursor)
	}
}

func TestThemeKeymapOverride(t *testing.T) {
	// The picker is a configurable context like any other.
	km := newKeyMap(map[string]string{"theme.down": "x"})
	if got := km.resolve(ctxTheme, "x"); got != actDown {
		t.Fatalf("override resolve = %q, want %q", got, actDown)
	}
	if got := km.primary(ctxLog, actTheme); got != "T" {
		t.Fatalf("log.theme primary = %q, want T", got)
	}
}

func TestThemeChromaOverride(t *testing.T) {
	defer setChromaStyleOverride("", "")
	setChromaStyleOverride("dracula", "github")
	if chromaStyleOverride != "dracula" || chromaStyleOverLite != "github" {
		t.Fatalf("chroma overrides = %q/%q", chromaStyleOverride, chromaStyleOverLite)
	}
	setChromaStyleOverride("", "")
	if chromaStyleOverride != "" || chromaStyleOverLite != "" {
		t.Fatal("reset failed")
	}
}
