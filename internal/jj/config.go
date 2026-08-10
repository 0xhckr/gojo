package jj

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config holds resolved runtime configuration.
type Config struct {
	JJPath   string
	GitPath  string
	RepoRoot string

	// AI configuration — any OpenAI-compatible chat-completions endpoint.
	// AIAPIKey is the bearer token, AIBaseURL is the API root (defaults to
	// OpenRouter), AIModel is the model name.
	AIAPIKey     string
	AIBaseURL    string
	AIModel      string
	CommitPrompt string

	// Keymap carries keybinding overrides keyed by "<context>.<action>"
	// (e.g. "log.down") with comma-separated key names as values. It is
	// populated from the [keymap] section of gojo.toml (or
	// [tools.gojo.keymap] in jj's config.toml) and consumed by the ui
	// package. An empty value unbinds the action.
	Keymap map[string]string

	// Theme is the selected color scheme id ("gojo", "terminal", "dracula",
	// a custom theme from ~/.config/gojo/themes, …). Empty = default theme.
	Theme string
}

// DefaultAIBaseURL is used when ai_base_url / openrouter_base_url is unset.
const DefaultAIBaseURL = "https://openrouter.ai/api/v1"

// DefaultAIModel is used when ai_model / openrouter_model is unset.
const DefaultAIModel = "anthropic/claude-sonnet-4"

// applyTOMLConfig parses a minimal subset of TOML, optionally restricted to a
// single section (e.g. "tools.gojo"). Only the keys gojo cares about are read.
// Keybindings are collected from a "<section>.keymap" sub-section (or the
// top-level [keymap] section when section is "") into cfg.Keymap.
func applyTOMLConfig(cfg *Config, raw string, section string) {
	inSection := section == "" // no section filter → parse all top-level lines
	keymapSection := section + ".keymap"
	if section == "" {
		keymapSection = "keymap"
	}
	inKeymap := false

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Section header like [tools.gojo]
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inSection = name == section
			inKeymap = name == keymapSection
			continue
		}

		if !inSection && !inKeymap {
			continue
		}

		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := strings.TrimSpace(trimmed[eq+1:])
		// Strip inline comments and matching quotes. TOML allows `# comment`
		// after a value; the old code only stripped quotes when the entire
		// remainder was quoted, so `key = "value" # comment` left the quote
		// and comment baked into the value.
		switch {
		case strings.HasPrefix(val, `"`):
			if end := strings.Index(val[1:], `"`); end >= 0 {
				val = val[1 : 1+end]
			}
		case strings.HasPrefix(val, `'`):
			if end := strings.Index(val[1:], `'`); end >= 0 {
				val = val[1 : 1+end]
			}
		default:
			if hash := strings.Index(val, "#"); hash >= 0 {
				val = strings.TrimSpace(val[:hash])
			}
		}

		if inKeymap {
			// Keybinding override: "log.down" = "j,down" etc. Quoted key
			// names (TOML dotted keys) are unwrapped by the quote stripping
			// above; the map key is used verbatim by the ui keymap.
			if cfg.Keymap == nil {
				cfg.Keymap = map[string]string{}
			}
			cfg.Keymap[key] = val
			continue
		}

		switch key {
		case "ai_api_key", "openrouter_api_key":
			cfg.AIAPIKey = val
		case "ai_base_url", "openrouter_base_url":
			cfg.AIBaseURL = val
		case "ai_model", "openrouter_model":
			cfg.AIModel = val
		case "commit_prompt":
			cfg.CommitPrompt = val
		case "theme":
			cfg.Theme = val
		}
	}
}

// LoadConfig resolves the jj binary, repo root, and overlays config from
// ~/.config/jj/config.toml ([tools.gojo]) then ~/.config/gojo/gojo.toml.
// When only the repo-root discovery fails, the returned Config still carries
// JJPath so the caller can run repo-creating commands (jj git init).
func LoadConfig() (Config, error) {
	jjPath, err := findBinary("jj")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{JJPath: jjPath}

	home, _ := os.UserHomeDir()

	// 1. jj user config [tools.gojo] (lower priority)
	if home != "" {
		if raw, err := os.ReadFile(filepath.Join(home, ".config", "jj", "config.toml")); err == nil {
			applyTOMLConfig(&cfg, string(raw), "tools.gojo")
		}
	}

	// 2. standalone gojo config (higher priority, overrides jj config)
	if raw, err := os.ReadFile(ConfigPath()); err == nil {
		applyTOMLConfig(&cfg, string(raw), "")
	}

	// Repo discovery happens last so user-level config (key bindings, AI
	// keys) still loads when gojo starts outside a repo and shows the boot
	// init prompt.
	repoRoot, err := findRepoRoot()
	if err != nil {
		return cfg, err
	}
	cfg.RepoRoot = repoRoot
	cfg.GitPath, _ = findBinary("git")

	return cfg, nil
}

func findBinary(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New(name + " not found in PATH")
	}
	return path, nil
}

// ConfigDir returns the gojo configuration directory ~/.config/gojo
// ("" when the home directory can't be determined).
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "gojo")
}

// ConfigPath returns the standalone gojo config file
// ~/.config/gojo/gojo.toml ("" when the home directory can't be determined).
func ConfigPath() string {
	if dir := ConfigDir(); dir != "" {
		return filepath.Join(dir, "gojo.toml")
	}
	return ""
}

// ThemesDir returns the custom theme directory ~/.config/gojo/themes.
func ThemesDir() string {
	if dir := ConfigDir(); dir != "" {
		return filepath.Join(dir, "themes")
	}
	return ""
}

// SaveTheme sets theme = "<id>" at the top level of the standalone gojo
// config, preserving everything else in the file. The directory and file are
// created when missing. The line is inserted before the first [section]
// (top-level keys must precede sections in TOML) or appended at the end.
func SaveTheme(id string) error {
	path := ConfigPath()
	if path == "" {
		return errors.New("no home directory")
	}
	raw, _ := os.ReadFile(path) // missing file is fine

	want := "theme = \"" + tomlStringEscape(id) + "\""
	lines := strings.Split(string(raw), "\n")
	var out []string
	replaced, wrote := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			// First section header: any not-yet-written theme key goes above it.
			if !replaced && !wrote {
				out = append(out, want)
				wrote = true
			}
			out = append(out, line)
			continue
		}
		// Replace an existing top-level theme key (only before any section).
		if !wrote && !replaced && !strings.HasPrefix(trimmed, "#") {
			if eq := strings.Index(trimmed, "="); eq > 0 {
				if strings.TrimSpace(trimmed[:eq]) == "theme" {
					out = append(out, want)
					replaced = true
					continue
				}
			}
		}
		out = append(out, line)
	}
	if !replaced && !wrote {
		// No sections, no existing key — append after trailing content.
		if n := len(out); n > 0 && out[n-1] == "" {
			out = out[:n-1]
		}
		out = append(out, want, "")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// tomlStringEscape escapes a string for a double-quoted TOML value.
func tomlStringEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// ErrNoRepo signals that no enclosing jj repo was found (no .jj directory in
// cwd or any ancestor). Callers can branch on it with errors.Is.
var ErrNoRepo = errors.New("no .jj directory found")

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".jj")); err == nil && info != nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoRepo
		}
		dir = parent
	}
}
