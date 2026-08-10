package jj

import "testing"

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
