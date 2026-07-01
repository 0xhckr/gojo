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
}

// DefaultAIBaseURL is used when ai_base_url / openrouter_base_url is unset.
const DefaultAIBaseURL = "https://openrouter.ai/api/v1"

// DefaultAIModel is used when ai_model / openrouter_model is unset.
const DefaultAIModel = "anthropic/claude-sonnet-4"

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

		switch key {
		case "ai_api_key", "openrouter_api_key":
			cfg.AIAPIKey = val
		case "ai_base_url", "openrouter_base_url":
			cfg.AIBaseURL = val
		case "ai_model", "openrouter_model":
			cfg.AIModel = val
		case "commit_prompt":
			cfg.CommitPrompt = val
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
	cfg.GitPath, _ = findBinary("git")

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
