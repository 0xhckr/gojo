package jj

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyTOMLConfigKeymap(t *testing.T) {
	var cfg Config
	raw := `
ai_model = "test/model"

[keymap]
log.down = "n"
diff.absorb = "" # unbind
bookmark.create = "C"

[other]
log.up = "ignored"

[tools.gojo]
ai_api_key = "ignored-in-this-section"
`
	// Standalone gojo.toml mode: top-level keys and [keymap] are read.
	applyTOMLConfig(&cfg, raw, "")
	if cfg.AIModel != "test/model" {
		t.Errorf("AIModel = %q", cfg.AIModel)
	}
	if cfg.Keymap["log.down"] != "n" {
		t.Errorf("Keymap[log.down] = %q", cfg.Keymap["log.down"])
	}
	if v, ok := cfg.Keymap["diff.absorb"]; !ok || v != "" {
		t.Errorf("Keymap[diff.absorb] = %q,%v; want empty-but-present", v, ok)
	}
	if cfg.Keymap["bookmark.create"] != "C" {
		t.Errorf("Keymap[bookmark.create] = %q", cfg.Keymap["bookmark.create"])
	}
	if _, ok := cfg.Keymap["log.up"]; ok {
		t.Error("keys under [other] must not leak into Keymap")
	}
	if cfg.AIAPIKey != "" {
		t.Errorf("keys inside [tools.gojo] must not leak into top-level parse, got %q", cfg.AIAPIKey)
	}
}

func TestApplyTOMLConfigKeymapJJSection(t *testing.T) {
	var cfg Config
	raw := `
[user]
name = "x"

[tools.gojo]
ai_model = "nested/model"

[tools.gojo.keymap]
global.quit = "Q"
`
	applyTOMLConfig(&cfg, raw, "tools.gojo")
	if cfg.AIModel != "nested/model" {
		t.Errorf("AIModel = %q", cfg.AIModel)
	}
	if cfg.Keymap["global.quit"] != "Q" {
		t.Errorf("Keymap[global.quit] = %q", cfg.Keymap["global.quit"])
	}
	if _, ok := cfg.Keymap["ai_model"]; ok {
		t.Error("non-keymap keys must not land in Keymap")
	}
}

func TestApplyTOMLConfigTheme(t *testing.T) {
	var cfg Config
	applyTOMLConfig(&cfg, `theme = "dracula"`, "")
	if cfg.Theme != "dracula" {
		t.Fatalf("Theme = %q, want dracula", cfg.Theme)
	}

	// Same, via jj's [tools.gojo] section.
	cfg = Config{}
	applyTOMLConfig(&cfg, "[tools.gojo]\ntheme = \"nord\"\n", "tools.gojo")
	if cfg.Theme != "nord" {
		t.Fatalf("Theme = %q, want nord", cfg.Theme)
	}
}

func TestSaveTheme(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Fresh: directory + file don't exist yet.
	if err := SaveTheme("dracula"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	path := filepath.Join(tmp, ".config", "gojo", "gojo.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got := string(raw); got != "theme = \"dracula\"\n" {
		t.Fatalf("file = %q", got)
	}

	// Existing key on a lonely IM line gets replaced in place.
	if err := SaveTheme("nord"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	raw, _ = os.ReadFile(path)
	if got := string(raw); got != "theme = \"nord\"\n" {
		t.Fatalf("file = %q", got)
	}

	// Existing key among other content is replaced; sections survive.
	orig := "# my config\ntheme = \"gojo\"\nai_model = \"x/y\"\n\n[keymap]\nlog.down = \"n\"\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme("tokyonight"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	raw, _ = os.ReadFile(path)
	got := string(raw)
	if !strings.Contains(got, "theme = \"tokyonight\"\n") || strings.Contains(got, `theme = "gojo"`) {
		t.Fatalf("theme key not replaced:\n%s", got)
	}
	if !strings.Contains(got, "[keymap]") || !strings.Contains(got, `ai_model = "x/y"`) {
		t.Fatalf("other content lost:\n%s", got)
	}

	// No existing top-level key: inserted before the first section.
	orig = "# comment\n\n[keymap]\nlog.down = \"n\"\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme("gruvbox"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	raw, _ = os.ReadFile(path)
	got = string(raw)
	themeIdx := strings.Index(got, `theme = "gruvbox"`)
	keymapIdx := strings.Index(got, "[keymap]")
	if themeIdx < 0 || keymapIdx < 0 || themeIdx > keymapIdx {
		t.Fatalf("theme key must come before first section:\n%s", got)
	}
}
