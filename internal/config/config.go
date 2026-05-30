package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Config holds application configuration.
type Config struct {
	JJPath    string // path to jj binary
	RepoRoot  string // repository root directory
	DebugFile string // optional debug log file
}

// Load discovers the jj binary and repo root from the environment.
func Load() (*Config, error) {
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		return nil, fmt.Errorf("jj not found in PATH: %w", err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("could not find jj repo: %w", err)
	}

	return &Config{
		JJPath:   jjPath,
		RepoRoot: repoRoot,
	}, nil
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
