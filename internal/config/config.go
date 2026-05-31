package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds application configuration.
type Config struct {
	JJPath           string `toml:"-"`
	RepoRoot         string `toml:"-"`
	DebugFile        string `toml:"-"`
	OpenRouterAPIKey string `toml:"openrouter_api_key"`
	OpenRouterModel  string `toml:"openrouter_model"`
	CommitPrompt     string `toml:"commit_prompt"`
}

// Load discovers the jj binary and repo root, then overlays the TOML config.
func Load() (*Config, error) {
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		return nil, fmt.Errorf("jj not found in PATH: %w", err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("could not find jj repo: %w", err)
	}

	cfg := &Config{
		JJPath:   jjPath,
		RepoRoot: repoRoot,
	}

	// Overlay TOML config (optional — missing file is OK).
	home, err := os.UserHomeDir()
	if err == nil {
		configPath := filepath.Join(home, ".config", "gojo", "gojo.toml")
		if _, err := os.Stat(configPath); err == nil {
			if _, err := toml.DecodeFile(configPath, cfg); err != nil {
				return nil, fmt.Errorf("parse %s: %w", configPath, err)
			}
		}
	}

	return cfg, nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".jj")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .jj directory found")
		}
		dir = parent
	}
}
