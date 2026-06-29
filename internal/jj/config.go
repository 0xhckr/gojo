package jj

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds resolved runtime configuration.
type Config struct {
	JJPath           string
	RepoRoot         string
	OpenRouterAPIKey string
	OpenRouterModel  string
	CommitPrompt     string

	// BlameScrollMargin is the minimum number of lines kept between the
	// cursor and the bottom of the file-view blame viewport. 0 lets the
	// cursor reach the last visible line; the default is 8.
	BlameScrollMargin int
}

// DefaultBlameScrollMargin is used when blame_scroll_margin is unset.
const DefaultBlameScrollMargin = 8

// applyTOMLConfig parses a minimal subset of TOML, optionally restricted to a
// single section (e.g. "tools.gojo"). Only the keys gojo cares about are read.
func applyTOMLConfig(cfg *Config, raw string, section string) {
	inSection := section == "" // no section filter → parse all top-level lines

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Section header like [tools.gojo]
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inSection = name == section
			continue
		}

		if !inSection {
			continue
		}

		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := strings.TrimSpace(trimmed[eq+1:])
		// Strip matching quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		switch key {
		case "openrouter_api_key":
			cfg.OpenRouterAPIKey = val
		case "openrouter_model":
			cfg.OpenRouterModel = val
		case "commit_prompt":
			cfg.CommitPrompt = val
		case "blame_scroll_margin":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n >= 0 {
				cfg.BlameScrollMargin = n
			}
		}
	}
}

// LoadConfig resolves the jj binary, repo root, and overlays config from
// ~/.config/jj/config.toml ([tools.gojo]) then ~/.config/gojo/gojo.toml.
func LoadConfig() (Config, error) {
	jjPath, err := findBinary("jj")
	if err != nil {
		return Config{}, err
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{JJPath: jjPath, RepoRoot: repoRoot}

	home, _ := os.UserHomeDir()

	// 1. jj user config [tools.gojo] (lower priority)
	if home != "" {
		if raw, err := os.ReadFile(filepath.Join(home, ".config", "jj", "config.toml")); err == nil {
			applyTOMLConfig(&cfg, string(raw), "tools.gojo")
		}
	}

	// 2. standalone gojo config (higher priority, overrides jj config)
	if home != "" {
		if raw, err := os.ReadFile(filepath.Join(home, ".config", "gojo", "gojo.toml")); err == nil {
			applyTOMLConfig(&cfg, string(raw), "")
		}
	}

	return cfg, nil
}

func findBinary(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New(name + " not found in PATH")
	}
	return path, nil
}

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
			return "", errors.New("no .jj directory found")
		}
		dir = parent
	}
}
