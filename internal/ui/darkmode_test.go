package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// restoreDarkBackground preserves the UI's shared color-scheme state.
func restoreDarkBackground(t *testing.T) {
	t.Helper()
	old := hasDarkBackground
	t.Cleanup(func() {
		hasDarkBackground = old
		applyTheme(gojoTheme())
	})
}

// The bubbletea msg carrying the DSR is unexported but structurally a named
// []byte; plain []byte msgs exercise the same decode path.
func TestDecodeColorScheme(t *testing.T) {
	cases := []struct {
		name     string
		msg      tea.Msg
		wantDark bool
		wantOK   bool
	}{
		{"dark report", uv.DarkColorSchemeEvent{}, true, true},
		{"light report", uv.LightColorSchemeEvent{}, false, true},
		{"other event", uv.UnknownCsiEvent("\x1b[?997;3n"), false, false},
		{"key msg", keyPress("x"), false, false},
		{"nil", nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dark, ok := decodeColorScheme(tc.msg)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && dark != tc.wantDark {
				t.Fatalf("dark = %v, want %v", dark, tc.wantDark)
			}
		})
	}
}

func TestApplyColorSchemeSwitchesBackground(t *testing.T) {
	restoreDarkBackground(t)
	defer applyTheme(gojoTheme())

	hasDarkBackground = true
	m := NewModel()
	m.themes = []Theme{gojoTheme()}
	m.themeName = "gojo"

	m.applyColorScheme(false)
	if hasDarkBackground {
		t.Fatal("still dark after light scheme report")
	}
	if got := defaultChromaStyleName(); got != "github" {
		t.Fatalf("chroma default = %q, want github", got)
	}

	m.applyColorScheme(true)
	if !hasDarkBackground {
		t.Fatal("still light after dark scheme report")
	}
	if got := defaultChromaStyleName(); got != "github-dark" {
		t.Fatalf("chroma default = %q, want github-dark", got)
	}
}

// Same-scheme reports must leave state — in particular the parsed diff rows —
// untouched.
func TestApplyColorSchemeNoopForCurrentScheme(t *testing.T) {
	restoreDarkBackground(t)
	defer applyTheme(gojoTheme())

	hasDarkBackground = false
	m := NewModel()
	m.themes = []Theme{gojoTheme()}
	m.themeName = "gojo"
	m.diffOpen = true
	m.diffIsRevision = true
	m.diffSrcRaw = "raw"
	sentinel := []diffRow{{kind: rowLine, sign: "+"}}
	m.diffRows = sentinel

	m.applyColorScheme(false)
	if hasDarkBackground {
		t.Fatal("background flipped on same-scheme report")
	}
	if !reflect.DeepEqual(m.diffRows, sentinel) {
		t.Fatal("diff rows rebuilt on same-scheme report")
	}
}

func TestApplyColorSchemeReHighlightsOpenDiff(t *testing.T) {
	restoreDarkBackground(t)
	defer applyTheme(gojoTheme())

	const raw = "diff --git a/main.go b/main.go\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		" package main\n" +
		"-func old() int\n" +
		"+func new() int\n"

	hasDarkBackground = true
	applyTheme(gojoTheme())

	m := NewModel()
	m.themes = []Theme{gojoTheme()}
	m.themeName = "gojo"
	m.diffOpen = true
	m.diffIsRevision = true
	m.diffSrcRaw = raw
	m.diffRows = renderDiff(raw)

	before := spanColors(m.diffRows)
	beforeBg := spanBackgrounds(m.diffRows)
	if len(before) == 0 {
		t.Fatal("expected chroma spans in the parsed diff")
	}

	m.applyColorScheme(false)
	after := spanColors(m.diffRows)
	afterBg := spanBackgrounds(m.diffRows)
	if len(after) == 0 {
		t.Fatal("spans lost after scheme switch")
	}
	if reflect.DeepEqual(before, after) {
		t.Fatalf("span colors unchanged after switch: %v", before)
	}
	if len(beforeBg) > 0 && reflect.DeepEqual(beforeBg, afterBg) {
		t.Fatalf("word-diff backgrounds unchanged after switch: %v", beforeBg)
	}
}

func spanBackgrounds(rows []diffRow) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		for _, s := range r.spans {
			if s.bg != "" {
				out[s.bg]++
			}
		}
	}
	return out
}

func spanColors(rows []diffRow) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		for _, s := range r.spans {
			if s.fg != "" {
				out[s.fg]++
			}
		}
	}
	return out
}

func TestUpdateHandlesColorSchemeReport(t *testing.T) {
	restoreDarkBackground(t)
	defer applyTheme(gojoTheme())

	hasDarkBackground = true
	m := NewModel()
	m = step(t, m, uv.LightColorSchemeEvent{})
	if hasDarkBackground {
		t.Fatal("Update did not apply the light scheme report")
	}
}

// File-view caches carry chroma colors; a scheme switch must drop them.
func TestApplyColorSchemeInvalidatesFileViewHighlights(t *testing.T) {
	restoreDarkBackground(t)
	defer applyTheme(gojoTheme())

	hasDarkBackground = true
	m := NewModel()
	m.themes = []Theme{gojoTheme()}
	m.themeName = "gojo"
	m.view = viewFile
	m.fileView.highlights = [][]span{{{text: "x", fg: "#112233"}}}
	m.fileView.blameRows = []diffRow{{kind: rowLine}}

	m.applyColorScheme(false)
	if m.fileView.highlights != nil {
		t.Fatal("file highlights not invalidated")
	}
	if m.fileView.blameRows != nil {
		t.Fatal("blame rows not invalidated")
	}
}
