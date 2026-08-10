package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// repoThemeDir is the shipped themes/ directory (repo root), relative to the
// ui package's test working directory.
func repoThemeDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "themes")
}

func repoThemes(t *testing.T) []Theme {
	t.Helper()
	themes := readThemeDir(repoThemeDir(t), false)
	if len(themes) < 49 {
		t.Fatalf("themes/ has %d themes, want at least 49 files", len(themes))
	}
	return themes
}

func TestRepoThemesLoad(t *testing.T) {
	themes := repoThemes(t)
	seen := map[string]bool{}
	for _, th := range themes {
		if seen[th.ID] {
			t.Errorf("duplicate theme id %q", th.ID)
		}
		seen[th.ID] = true
		if th.Title == "" {
			t.Errorf("%s: empty title", th.ID)
		}
		if th.Dark == nil && th.Light == nil {
			t.Errorf("%s: no variants", th.ID)
		}
		for _, v := range []*paletteDef{th.Dark, th.Light} {
			if v == nil {
				continue
			}
			if v.Background == "" {
				t.Errorf("%s: background empty", th.ID)
			}
			if v.Text == "" {
				t.Errorf("%s: text empty", th.ID)
			}
			if _, _, _, ok := parseHex(v.Background); !ok {
				t.Errorf("%s: background %q is not #rrggbb", th.ID, v.Background)
			}
			// With hex anchors, resolve() must fill every single slot.
			r := resolve(*v)
			for name, idx := range tomlPaletteKeys {
				if fv := fieldByIndex(r, idx); fv == "" {
					t.Errorf("%s: resolve left %s empty", th.ID, name)
				}
			}
			// Every resolved color must be a hex color.
			for name, idx := range tomlPaletteKeys {
				fv := fieldByIndex(r, idx)
				if _, _, _, ok := parseHex(fv); !ok {
					t.Errorf("%s: resolved %s = %q, not #rrggbb", th.ID, name, fv)
				}
			}
		}
	}
}

func TestRepoThemeSyntaxNamesExist(t *testing.T) {
	for _, th := range repoThemes(t) {
		if th.SyntaxDark != "" && styles.Get(th.SyntaxDark) == nil {
			t.Errorf("%s: syntax %q not a known chroma style", th.ID, th.SyntaxDark)
		}
		if th.SyntaxLight != "" && styles.Get(th.SyntaxLight) == nil {
			t.Errorf("%s: syntax_light %q not a known chroma style", th.ID, th.SyntaxLight)
		}
	}
}

func TestLoadThemesMergeAndShadow(t *testing.T) {
	tmp := t.TempDir()
	user := filepath.Join(tmp, "home")
	// User dir overrides by id: shadow "dracula" and add "mytheme".
	themesDir := userThemesDir(user)
	writeThemeFile(t, filepath.Join(themesDir, "dracula.toml"),
		"name = \"CustomDracula\"\n\n[dark]\nbackground = \"#111111\"\ntext = \"#eeeeee\"\n")
	writeThemeFile(t, filepath.Join(themesDir, "my theme.toml"),
		"[light]\nbackground = \"#fafafa\"\ntext = \"#111111\"\n")

	themes := loadThemes(user, "", "")
	if len(themes) < 2 {
		t.Fatalf("themes = %d, want at least compiled-ins", len(themes))
	}
	// Compiled-ins first.
	if themes[0].ID != "gojo" || themes[1].ID != "terminal" {
		t.Fatalf("first themes = %q,%q; want gojo, terminal", themes[0].ID, themes[1].ID)
	}
	// The custom shadow won.
	i := findTheme(themes, "dracula")
	if i < 0 || themes[i].Title != "CustomDracula" || !themes[i].Custom {
		t.Fatalf("dracula shadow = %+v (idx %d)", themes[i:], i)
	}
	// New user theme loaded; filename spaces became dashes in the id.
	j := findTheme(themes, "my-theme")
	if j < 0 || !themes[j].Custom || themes[j].Light == nil {
		t.Fatalf("my-theme not loaded as custom light theme")
	}
	// Sorted: gojo + terminal pinned, rest by title.
	for k := 3; k < len(themes); k++ {
		if strings.ToLower(themes[k-1].Title) > strings.ToLower(themes[k].Title) {
			t.Errorf("themes not sorted: %q > %q", themes[k-1].Title, themes[k].Title)
		}
	}
}

func writeThemeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseThemeTOML(t *testing.T) {
	raw := `
# comment
name = "My Theme"
syntax = "dracula"
syntax_light = "github"

[dark]
background = "#282a36"   # inline comment
text = '#f8f8f2'
unknown_key = "ignored"
purple = "5"

[weird]
ignored = "yes"

[light]
background = "#fafafa"
text = "#111111"
`
	th, err := parseThemeTOML("mytheme", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if th.Title != "My Theme" {
		t.Errorf("Title = %q", th.Title)
	}
	if th.SyntaxDark != "dracula" || th.SyntaxLight != "github" {
		t.Errorf("syntax = %q / %q", th.SyntaxDark, th.SyntaxLight)
	}
	if th.Dark == nil || th.Dark.Background != "#282a36" || th.Dark.Purple != "5" {
		t.Errorf("dark = %+v", th.Dark)
	}
	if th.Light == nil || th.Light.Text != "#111111" {
		t.Errorf("light = %+v", th.Light)
	}

	// No palette → error.
	if _, err := parseThemeTOML("empty", "name = \"x\"\n"); err == nil {
		t.Fatal("want error for theme without [dark]/[light]")
	}
}

func TestMix(t *testing.T) {
	if got := mix("#ff0000", "#000000", 0.5); got != "#800000" {
		t.Errorf("mix = %q, want #800000", got)
	}
	if got := mix("#000000", "#ffffff", 1); got != "#000000" {
		t.Errorf("mix t=1 = %q, want #000000", got)
	}
	if got := mix("2", "#000000", 0.5); got != "" {
		t.Errorf("ANSI input must not mix, got %q", got)
	}
}

func TestApplyTheme(t *testing.T) {
	defer applyTheme(gojoTheme())

	applyTheme(gojoTheme())
	want := lipgloss.AdaptiveColor{Light: "#6b50ff", Dark: "#9d7cd8"}
	if colPurple != want {
		t.Fatalf("colPurple = %v, want %v", colPurple, want)
	}

	// Dracula has no light variant: dark pins both.
	dark := resolve(paletteDef{Background: "#282a36", Element: "#44475a", Text: "#f8f8f2", TextMuted: "#6272a4",
		Purple: "#bd93f9", Blue: "#8be9fd", Green: "#50fa7b", Red: "#ff5555", Yellow: "#f1fa8c",
		Cyan: "#8be9fd", Orange: "#ffb86c", Pink: "#ff79c6"})
	applyTheme(Theme{ID: "x", Dark: &dark})
	if colPurple != lipgloss.Color("#bd93f9") {
		t.Fatalf("colPurple = %v", colPurple)
	}
	// Derived diff tint: green mixed onto the background.
	if colText != lipgloss.Color("#f8f8f2") {
		t.Fatalf("colText = %v", colText)
	}
	if diffAddedBg != lipgloss.Color(mix("#50fa7b", "#282a36", 0.13)) {
		t.Fatalf("diffAddedBg = %v, want derived tint", diffAddedBg)
	}
}

func TestTerminalTheme(t *testing.T) {
	defer applyTheme(gojoTheme())
	applyTheme(terminalTheme())

	// Body colors fall through to the terminal defaults / ANSI palette.
	if colText != nil {
		t.Errorf("colText = %v, want nil (terminal default)", colText)
	}
	if colBackground != nil {
		t.Errorf("colBackground = %v, want nil", colBackground)
	}
	if colRed != lipgloss.Color("1") || colGreen != lipgloss.Color("2") || colBlue != lipgloss.Color("4") {
		t.Errorf("ANSI accents wrong: red=%v green=%v blue=%v", colRed, colGreen, colBlue)
	}
	if colElement != lipgloss.Color("8") {
		t.Errorf("colElement = %v, want ANSI 8", colElement)
	}
	// Tint backgrounds are not derivable from ANSI colors → terminal default.
	if diffAddedBg != nil {
		t.Errorf("diffAddedBg = %v, want nil (no mixing with ANSI)", diffAddedBg)
	}
}

func TestCompiledThemesResolve(t *testing.T) {
	// Even with no theme files installed, the picker list works.
	themes := compiledThemes()
	if len(themes) != 2 || themes[0].ID != "gojo" || themes[1].ID != "terminal" {
		t.Fatalf("compiledThemes = %v", themes)
	}
	for _, th := range themes {
		for _, dark := range []bool{true, false} {
			p := th.variant(dark)
			if p.Purple == "" || p.Green == "" {
				t.Errorf("%s: unresolved accents (dark=%v)", th.ID, dark)
			}
		}
	}
}

// fieldByIndex reads a palette string field by struct index.
func fieldByIndex(p paletteDef, idx int) string {
	return reflect.ValueOf(&p).Elem().Field(idx).String()
}
