package ui

// The theme picker: a scrollable list of all known themes with a live
// preview — moving the cursor applies the theme immediately so the whole UI
// repaints in it, ⏎ keeps the choice (and persists it to gojo.toml), esc
// restores the theme that was active when the picker opened.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// persistTheme saves the chosen theme id to the config; a var so tests can
// stub out the filesystem write.
var persistTheme = func(id string) error {
	return jj.SaveTheme(id)
}

func (m Model) themeIndex() int {
	return findTheme(m.themes, m.themeName)
}

// openThemePicker enters the theme picker, remembering the active theme so
// cancel can restore it.
func (m Model) openThemePicker() Model {
	m.themeOpen = true
	m.themeReturn = m.themeName
	idx := m.themeIndex()
	if idx < 0 {
		idx = 0
	}
	m.themeCursor = idx
	m.themeEnsureVisible()
	return m
}

// applyThemeByName applies a known theme id. Unknown / empty ids fall back
// to the compiled-in default ("gojo", always themes[0]).
func (m *Model) applyThemeByName(name string) {
	if i := findTheme(m.themes, name); i >= 0 {
		applyTheme(m.themes[i])
		m.themeName = m.themes[i].ID
		return
	}
	if len(m.themes) > 0 {
		applyTheme(m.themes[0])
		m.themeName = m.themes[0].ID
	}
}

// previewTheme applies the theme under the picker cursor without changing
// the active theme record (that's what ⏎ commits).
func (m *Model) previewTheme() {
	if m.themeCursor >= 0 && m.themeCursor < len(m.themes) {
		applyTheme(m.themes[m.themeCursor])
	}
}

// themeEnsureVisible scrolls themeOffset so the cursor row is inside the
// visible window of the current content height.
func (m *Model) themeEnsureVisible() {
	contentH := m.themeContentHeight()
	if contentH < 1 {
		contentH = 1
	}
	if m.themeCursor < m.themeOffset {
		m.themeOffset = m.themeCursor
	}
	if m.themeCursor >= m.themeOffset+contentH {
		m.themeOffset = m.themeCursor - contentH + 1
	}
}

func (m Model) themeContentHeight() int {
	h := m.contentHeight() - 1 // minus the picker title bar
	if h < 1 {
		h = 1
	}
	return h
}

func (m Model) themeMaxOffset() int {
	if n := len(m.themes) - m.themeContentHeight(); n > 0 {
		return n
	}
	return 0
}

// themeMove moves the picker cursor and live-previews the theme there.
func (m *Model) themeMove(idx int) {
	if len(m.themes) == 0 {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.themes) {
		idx = len(m.themes) - 1
	}
	if idx == m.themeCursor {
		return
	}
	m.themeCursor = idx
	m.themeEnsureVisible()
	m.previewTheme()
}

// handleThemeKey drives the picker list. Movement live-previews; ⏎ commits
// (and writes gojo.toml); esc cancels and restores the original theme.
func (m Model) handleThemeKey(k string) (Model, tea.Cmd) {
	switch m.keys.resolve(ctxTheme, k) {
	case actCancel:
		m.themeOpen = false
		m.applyThemeByName(m.themeReturn)
		return m, nil
	case actApply:
		if len(m.themes) == 0 {
			m.themeOpen = false
			return m, nil
		}
		t := m.themes[m.themeCursor]
		m.themeOpen = false
		m.themeName = t.ID
		m.themeReturn = t.ID
		applyTheme(t)
		if err := persistTheme(t.ID); err != nil {
			m.errMsg = "theme applied but not saved: " + err.Error()
		} else {
			m.message = "theme: " + t.Title + " (saved to gojo.toml)"
		}
		return m, nil
	case actUp:
		m.themeMove(m.themeCursor - 1)
	case actDown:
		m.themeMove(m.themeCursor + 1)
	case actTop:
		m.themeMove(0)
	case actBottom:
		m.themeMove(len(m.themes) - 1)
	case actPageUp:
		m.themeMove(m.themeCursor - m.themeContentHeight())
	case actPageDown:
		m.themeMove(m.themeCursor + m.themeContentHeight())
	}
	return m, nil
}

// renderThemePicker produces exactly height lines (title bar + list window).
func (m Model) renderThemePicker(width, height int) []string {
	contentH := height - 1
	if contentH < 1 {
		contentH = 1
	}
	total := len(m.themes)

	// Title bar.
	titleLeft := fmt.Sprintf(" gojo themes (%d)", total)
	titleRight := fmt.Sprintf("%s apply · %s cancel ",
		m.hk(ctxTheme, actApply), m.hk(ctxTheme, actCancel))
	titlePad := max(1, width-len(titleLeft)-len(titleRight))
	title := bgRow(width, colElement,
		seg{text: titleLeft, fg: colPurple, bg: colElement},
		seg{text: strings.Repeat(" ", titlePad), bg: colElement},
		seg{text: titleRight, fg: colGray, bg: colElement})

	out := []string{title}

	// Scrollbar when the list overflows.
	offset := min(max(0, m.themeOffset), m.themeMaxOffset())
	end := min(total, offset+contentH)
	scrollW := width
	thumbStart, thumbEnd := scrollbarThumb(total, offset, end-offset, contentH)
	hasBar := thumbStart >= 0
	if hasBar {
		scrollW -= scrollbarWidth
	}

	// Widest title for name-column alignment (capped so the meta stays on).
	nameW := 0
	for i := range m.themes {
		if w := segTextWidth(m.themes[i].Title); w > nameW {
			nameW = w
		}
	}

	dark := lipgloss.HasDarkBackground()
	for i := offset; i < end; i++ {
		t := m.themes[i]
		rowBg := colPanel
		switch {
		case i == m.themeCursor:
			rowBg = colElement
		case i == m.hover.themeRow:
			rowBg = colHover
		}

		segs := []seg{}
		if t.ID == m.themeName {
			segs = append(segs, seg{text: "●", fg: colGreen, bg: rowBg})
		} else {
			segs = append(segs, seg{text: " ", bg: rowBg})
		}
		segs = append(segs, seg{text: " ", bg: rowBg})
		// Palette swatch in the theme's own colors.
		for _, c := range t.previewColors(dark) {
			segs = append(segs, seg{text: "█", fg: c, bg: rowBg})
		}
		segs = append(segs, seg{text: " ", bg: rowBg})
		namePad := max(0, nameW-segTextWidth(t.Title))
		segs = append(segs, seg{text: t.Title + strings.Repeat(" ", namePad), fg: colText, bold: i == m.themeCursor, bg: rowBg})
		meta := "  " + t.variantLabel()
		if t.Custom {
			meta += " · user"
		}
		segs = append(segs, seg{text: meta, fg: colTextMuted, bg: rowBg})

		out = append(out, renderRowWithBarFromString(scrollW, width, rowBg, hasBar, i-offset, thumbStart, thumbEnd, bgRow(scrollW, rowBg, segs...)))
	}

	// Pad to full height.
	for len(out) < height {
		out = append(out, blankRow(width, colPanel))
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}
