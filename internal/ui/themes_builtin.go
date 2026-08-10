package ui

// Built-in theme loading. The "gojo" default and the ANSI "terminal" theme
// are compiled in — they must exist even when gojo runs from an install that
// shipped no theme files. Everything else is read from *.toml files found in
// themeSearchDirs: the directory next to the binary (share/gojo/themes), the
// system share dirs, ./themes (repo-root dev builds), and finally the user
// dir ~/.config/gojo/themes (marked Custom; wins id collisions).

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// userThemesDir is ~/.config/gojo/themes ("" when the home dir is unknown).
func userThemesDir(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "gojo", "themes")
}

// themeSearchDirs lists the theme directories in load order (weakest first).
// dirs that don't exist are skipped by the reader.
func themeSearchDirs(home, exePath, cwd string) []string {
	var dirs []string
	if exePath != "" {
		dirs = append(dirs, filepath.Join(filepath.Dir(exePath), "..", "share", "gojo", "themes"))
	}
	dirs = append(dirs,
		filepath.Join("/usr", "local", "share", "gojo", "themes"),
		filepath.Join("/usr", "share", "gojo", "themes"),
	)
	if cwd != "" {
		dirs = append(dirs, filepath.Join(cwd, "themes")) // go run . / dev build
	}
	if d := userThemesDir(home); d != "" {
		dirs = append(dirs, d)
	}
	// Dedupe (e.g. /usr/bin/../share == /usr/share).
	seen := map[string]bool{}
	out := dirs[:0]
	for _, d := range dirs {
		c := filepath.Clean(d)
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// LoadThemes assembles the full picker list: compiled-in gojo/terminal first,
// then every theme file found in the search dirs (later dirs shadow by id;
// themes from the user dir are marked Custom). The gojo id always resolves,
// so config typos fall back to it.
func LoadThemes() []Theme {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cwd, _ := os.Getwd()
	return loadThemes(home, exe, cwd)
}

// compiledThemes is the always-available fallback list (no files needed).
func compiledThemes() []Theme {
	return []Theme{gojoTheme(), terminalTheme()}
}

func loadThemes(home, exePath, cwd string) []Theme {
	themes := compiledThemes()
	index := map[string]int{themes[0].ID: 0, themes[1].ID: 1}
	userDir := userThemesDir(home)
	for _, dir := range themeSearchDirs(home, exePath, cwd) {
		custom := dir == userDir
		for _, t := range readThemeDir(dir, custom) {
			if i, ok := index[t.ID]; ok {
				themes[i] = t
				continue
			}
			index[t.ID] = len(themes)
			themes = append(themes, t)
		}
	}
	// gojo, terminal pinned at the top; the rest alphabetical by title.
	rest := themes[2:]
	sort.SliceStable(rest, func(i, j int) bool {
		return strings.ToLower(rest[i].Title) < strings.ToLower(rest[j].Title)
	})
	return themes
}

// findTheme returns the index of the theme with the given id, or -1.
func findTheme(themes []Theme, id string) int {
	for i := range themes {
		if themes[i].ID == id {
			return i
		}
	}
	return -1
}

// readThemeDir parses every *.toml file in dir (one theme per file, id =
// file name stem, lowercased with spaces → '-'). Unreadable dirs, file-level
// parse errors and themes with no [dark]/[light] section are skipped.
func readThemeDir(dir string, custom bool) []Theme {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Theme
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".toml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		id := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		id = strings.ReplaceAll(id, " ", "-")
		t, err := parseThemeTOML(id, string(raw))
		if err != nil {
			continue
		}
		t.Custom = custom
		out = append(out, t)
	}
	return out
}

// parseThemeTOML parses one theme file. Top-level keys: name (display title,
// defaults to the id), syntax / syntax_dark, syntax_light (chroma style
// overrides). Palette keys live in [dark] and [light] sections and match the
// paletteDef field names; unknown keys/sections are ignored.
func parseThemeTOML(id, raw string) (Theme, error) {
	t := Theme{ID: id, Title: id}
	var dark, light paletteDef
	section := ""

	setKey := func(key, val string) {
		switch section {
		case "":
			switch key {
			case "name", "title":
				t.Title = val
			case "syntax", "syntax_dark":
				t.SyntaxDark = val
			case "syntax_light":
				t.SyntaxLight = val
			}
		case "dark", "light":
			idx, ok := tomlPaletteKeys[key]
			if !ok {
				return
			}
			if section == "dark" {
				reflectSetString(&dark, idx, val)
			} else {
				reflectSetString(&light, idx, val)
			}
		}
	}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1:strings.Index(trimmed, "]")])
			name = strings.ToLower(name)
			if name == "dark" || name == "light" {
				section = name
			} else {
				section = "!" // unknown section — ignore its keys
			}
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:eq]))
		val := strings.TrimSpace(trimmed[eq+1:])
		switch {
		case strings.HasPrefix(val, `"`):
			if end := strings.Index(val[1:], `"`); end >= 0 {
				val = val[1 : 1+end]
			} else {
				continue // unterminated string
			}
		case strings.HasPrefix(val, `'`):
			if end := strings.Index(val[1:], `'`); end >= 0 {
				val = val[1 : 1+end]
			} else {
				continue
			}
		default:
			if hash := strings.Index(val, "#"); hash >= 0 {
				val = strings.TrimSpace(val[:hash])
			}
		}
		setKey(key, val)
	}

	if dark != (paletteDef{}) {
		d := resolve(dark) // resolve once; variants are used per frame
		t.Dark = &d
	}
	if light != (paletteDef{}) {
		l := resolve(light)
		t.Light = &l
	}
	if t.Dark == nil && t.Light == nil {
		return t, errNoPalette(id)
	}
	return t, nil
}

// reflectSetString sets palette field idx (a paletteDef field index) to val.
func reflectSetString(p *paletteDef, idx int, val string) {
	reflect.ValueOf(p).Elem().Field(idx).SetString(val)
}

// errNoPalette reports a theme file without any [dark]/[light] palette.
type themeParseError string

func (e themeParseError) Error() string { return "theme: no palette section in " + string(e) }

func errNoPalette(id string) error { return themeParseError(id) }

// ── Compiled-in themes ──────────────────────────────────────────────────────

// gojoTheme is the default palette — the historical gojo color scheme, fully
// explicit in both variants so the default look never depends on derivation.
func gojoTheme() Theme {
	dark := &paletteDef{
		Background: "#0d0d12", Panel: "#14141c", Element: "#1c1c26", Hover: "#181824",
		Border: "#343440", BorderActive: "#545462", BorderSubtle: "#24242e", Graph: "#5a5a72",
		Text: "#e2e2ec", TextMuted: "#787886",
		Purple: "#9d7cd8", Magenta: "#c487f0", Blue: "#5c9cf5", Green: "#7fd88f",
		Red: "#e06c75", Yellow: "#f5a742", Cyan: "#56b6c2", Orange: "#f5a742",
		DarkOrange: "#b08030", Pink: "#ff7eb6", DarkPink: "#b85a90", Teal: "#4db6ac", DarkTeal: "#2e7d72",
		DiffAddedSign: "#7fd88f", DiffRemovedSign: "#e06c75", DiffContextFg: "#e2e2ec",
		DiffHunkHeaderFg: "#828bb8", DiffFileHeaderFg: "#f5a742", DiffLineNumber: "#8f8f8f",
		DiffAddedBg: "#1a2a22", DiffRemovedBg: "#2a1a22", DiffHunkHeaderBg: "#1a2230", DiffFileHeaderBg: "#24221a",
		DiffAddedGutterBg: "#171f1f", DiffRemovedGutterBg: "#1f171f",
		DiffCursorAddBright: "#7fd88f", DiffCursorDelBright: "#e06c75", DiffCursorAddDim: "#2e4a2e", DiffCursorDelDim: "#4a2e2e",
		SplitMarked: "#7fd88f", SplitPartial: "#f5a742", SplitUnmarked: "#555560",
		ConfLeftBg: "#192230", ConfLeftFocusBg: "#213144", ConfRightBg: "#1a291d", ConfRightFocusBg: "#233a28", ConfLoserBg: "#17171d",
		SectionBgA: "#1a1a2e", SectionBgB: "#241a26", SectionBarDimA: "#2e2e48", SectionBarDimB: "#3e2840",
		SectionBarBrightA: "#8a8cf5", SectionBarBrightB: "#e08ad8",
	}
	light := &paletteDef{
		Background: "#f6f6f8", Panel: "#ffffff", Element: "#ececf0", Hover: "#f0f0f5",
		Border: "#c4c4cc", BorderActive: "#9898a4", BorderSubtle: "#dadde0", Graph: "#9494a4",
		Text: "#1a1a22", TextMuted: "#787884",
		Purple: "#6b50ff", Magenta: "#7b3fb5", Blue: "#2563eb", Green: "#3d9a57",
		Red: "#d1383d", Yellow: "#b0851f", Cyan: "#318795", Orange: "#d68c27",
		DarkOrange: "#a06b1a", Pink: "#c44b8a", DarkPink: "#9a3868", Teal: "#00897b", DarkTeal: "#00695c",
		DiffAddedSign: "#3d9a57", DiffRemovedSign: "#d1383d", DiffContextFg: "#1a1a22",
		DiffHunkHeaderFg: "#7086b5", DiffFileHeaderFg: "#b0851f", DiffLineNumber: "#595959",
		DiffAddedBg: "#d8edd8", DiffRemovedBg: "#f0d8dc", DiffHunkHeaderBg: "#d8e4ec", DiffFileHeaderBg: "#eee4cc",
		DiffAddedGutterBg: "#ebf6eb", DiffRemovedGutterBg: "#f7ebed",
		DiffCursorAddBright: "#3d9a57", DiffCursorDelBright: "#d1383d", DiffCursorAddDim: "#a8d8a8", DiffCursorDelDim: "#d8a8a8",
		SplitMarked: "#3d9a57", SplitPartial: "#b0851f", SplitUnmarked: "#999999",
		ConfLeftBg: "#dde5f4", ConfLeftFocusBg: "#ccdaf2", ConfRightBg: "#dcf0dc", ConfRightFocusBg: "#cbeacc", ConfLoserBg: "#eaeaef",
		SectionBgA: "#eae6f6", SectionBgB: "#f6e8f0", SectionBarDimA: "#c4bbe0", SectionBarDimB: "#e0bcd0",
		SectionBarBrightA: "#6b50ff", SectionBarBrightB: "#c44b8a",
	}
	d, l := resolve(*dark), resolve(*light)
	return Theme{ID: "gojo", Title: "gojo (default)", Dark: &d, Light: &l}
}

// terminalTheme follows the terminal's own color scheme: body text and
// surfaces stay at the terminal defaults (nil), accents map to the 16 standard
// ANSI colors, and surfaces that need to contrast (selection, focus markers)
// use bright black. With this theme gojo looks native in any terminal scheme.
func terminalTheme() Theme {
	p := &paletteDef{
		Element: "8", Border: "8", BorderActive: "7", BorderSubtle: "8", Graph: "8",
		TextMuted: "8",
		Purple:    "5", Magenta: "13", Blue: "4", Green: "2",
		Red: "1", Yellow: "3", Cyan: "6", Orange: "11", Pink: "9", Teal: "14",
		DiffAddedSign: "2", DiffRemovedSign: "1", DiffHunkHeaderFg: "4", DiffFileHeaderFg: "3", DiffLineNumber: "8",
		DiffCursorAddBright: "2", DiffCursorDelBright: "1", DiffCursorAddDim: "8", DiffCursorDelDim: "8",
		SplitMarked: "2", SplitPartial: "3", SplitUnmarked: "8",
		ConfLeftFocusBg: "8", ConfRightFocusBg: "8",
		SectionBarDimA: "8", SectionBarDimB: "8", SectionBarBrightA: "4", SectionBarBrightB: "5",
	}
	r := resolve(*p)
	return Theme{ID: "terminal", Title: "terminal (follow terminal)", Dark: &r, Light: &r}
}
