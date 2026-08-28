package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestSegTextWidthParity pins the ASCII fast path to the grapheme-aware
// x/ansi width for a variety of segment texts.
func TestSegTextWidthParity(t *testing.T) {
	for _, s := range []string{
		"", "abc", "with  spaces", "2024-01-02 12:34", "user@example.com",
		"张雨生", "→ arrow", "é combining", "👨‍👩‍👧 emoji", "tab\tinside",
		"\x7f del", "⠋ braille spinner", "│ graph ─┤",
	} {
		if got, want := segTextWidth(s), ansi.StringWidth(s); got != want {
			t.Errorf("segTextWidth(%q) = %d, ansi.StringWidth = %d", s, got, want)
		}
	}
}

// TestStyleCacheEquivalence verifies the cached style fast path produces
// byte-identical output to a fresh lipgloss Render for a matrix of styles and
// texts.
func TestStyleCacheEquivalence(t *testing.T) {
	combos := []seg{
		{},
		{fg: colPurple},
		{bg: colPanel},
		{fg: colGreen, bg: diffAddedBg},
		{fg: colPurple, bg: colElement, bold: true},
		{fg: colTextMuted, bg: colPanel, faint: true},
		{fg: colGreen, bg: colPanel, underline: true},
		{fg: colMagenta, bold: true, underline: true, faint: true, bg: colPanel},
	}
	texts := []string{"x", "hello world", "   ", "→", "multi → rune  tail  ", "s1337 text"}
	for _, s := range combos {
		cs := styleFor(s)
		for _, text := range texts {
			st := lipgloss.NewStyle()
			if s.fg != nil {
				st = st.Foreground(s.fg)
			}
			if s.bg != nil {
				st = st.Background(s.bg)
			}
			if s.bold {
				st = st.Bold(true)
			}
			if s.underline {
				st = st.Underline(true)
			}
			if s.faint {
				st = st.Faint(true)
			}
			var b strings.Builder
			cs.apply(&b, text)
			got := b.String()
			if want := st.Render(text); got != want {
				t.Errorf("seg %+v text %q:\n got %q\nwant %q", s, text, got, want)
			}
		}
	}
}
