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
// bubbletea's input parser doesn't know these sequences, but surfaces each
// unrecognized CSI as one unexported unknownCSISequenceMsg (a named []byte
// holding the full escape). decodeColorScheme finds it structurally instead
// of by type. Terminals that never report simply emit nothing — gojo then
// keeps the scheme detected at startup, exactly as before.

import (
	"bytes"
	"reflect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	schemeDarkDSR  = []byte("\x1b[?997;1n")
	schemeLightDSR = []byte("\x1b[?997;2n")
)

// decodeColorScheme extracts the terminal's dark/light scheme report from a
// message. ok=false for anything that is not one of the two DSR sequences.
// Matching is exact, so byte-stream corruption never produces the opposite
// scheme — at worst a report is dropped (the next toggle recovers).
func decodeColorScheme(msg tea.Msg) (dark, ok bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return false, false
	}
	switch b := v.Bytes(); {
	case bytes.Equal(b, schemeDarkDSR):
		return true, true
	case bytes.Equal(b, schemeLightDSR):
		return false, true
	}
	return false, false
}

// applyColorScheme switches the UI between its dark and light palette
// variants after the terminal reported a scheme change. Everything that
// resolved colors against the startup-time background detection is refreshed;
// a no-op when the reported scheme already applies.
func (m *Model) applyColorScheme(dark bool) {
	if dark == lipgloss.HasDarkBackground() {
		return
	}

	// lipgloss caches the auto-detected background on first use and consults
	// it at render time for AdaptiveColor resolution; the style and blank-row
	// caches key on it, so the next repaint picks the other palette half.
	lipgloss.SetHasDarkBackground(dark)

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
