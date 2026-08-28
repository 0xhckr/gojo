package ui

// Terminal dark/light scheme tracking.
//
// main.go enables xterm private mode 2031 (CSI ? 2031 h) before starting the
// TUI, asking terminals that support it (kitty, Ghostty, VTE, Contour, …) to
// push a device status report whenever the color scheme flips — the OS
// dark/light toggle, or a manual terminal-profile switch:
//
//	CSI ? 997 ; 1 n   (dark)
//	CSI ? 997 ; 2 n   (light)
//
// Bubble Tea v2's input parser exposes these as Ultraviolet color-scheme
// events. Terminals that never report simply emit nothing, so gojo keeps the
// scheme reported by the initial background-color query.

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// decodeColorScheme extracts the terminal's dark/light scheme report from a
// message. ok=false for anything that is not one of the two scheme events.
func decodeColorScheme(msg tea.Msg) (dark, ok bool) {
	switch msg.(type) {
	case uv.DarkColorSchemeEvent:
		return true, true
	case uv.LightColorSchemeEvent:
		return false, true
	}
	return false, false
}

// applyColorScheme switches the UI between its dark and light palette
// variants after the terminal reported a scheme change. Everything that
// resolved colors against the startup-time background detection is refreshed;
// a no-op when the reported scheme already applies.
func (m *Model) applyColorScheme(dark bool) {
	if dark == hasDarkBackground {
		return
	}

	hasDarkBackground = dark

	// Re-run the active theme so chroma syntax-style bookkeeping
	// (setChromaStyleOverride) and any detection-time consumers refresh
	// together with the adaptive palette vars.
	if i := findTheme(m.themes, m.themeName); i >= 0 {
		applyTheme(m.themes[i])
	}

	// Content parsed with chroma-resolved hex colors carries the old scheme's
	// style; rebuild it. Everything else resolves colors at render time.
	if m.diffOpen && m.diffIsRevision && m.diffSrcRaw != "" {
		m.diffRows = renderDiff(m.diffSrcRaw)
	}
	if m.view == viewFile {
		m.fileView.highlights = nil
		m.fileView.blameRows = nil
		// Re-build the blame cache now (Update-side), like the resize path —
		// otherwise every frame would recompute the O(file) layout inline.
		if m.fileView.phase == fileBlame {
			m.fileView.buildBlameCache(m.width, fileViewContentH(*m))
		}
	}
}
