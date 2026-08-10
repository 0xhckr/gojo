package jj

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordArgsJJ writes an executable fake jj that saves its argv (one arg per
// line) into argsFile before exiting 0.
func recordArgsJJ(t *testing.T) (script, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script = filepath.Join(dir, "jj")
	body := "#!/bin/sh\nprintf '%s\n' \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, argsFile
}

func TestGitInitDirArgs(t *testing.T) {
	jjPath, argsFile := recordArgsJJ(t)
	targetDir := t.TempDir()

	if err := GitInitDir(jjPath, targetDir, true); err != nil {
		t.Fatalf("GitInitDir colocate: %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(args)); strings.Join(got, " ") != "git init --colocate" {
		t.Fatalf("colocate argv = %v, want [git init --colocate]", got)
	}

	if err := GitInitDir(jjPath, targetDir, false); err != nil {
		t.Fatalf("GitInitDir no-colocate: %v", err)
	}
	args, err = os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(args)); strings.Join(got, " ") != "git init --no-colocate" {
		t.Fatalf("no-colocate argv = %v, want [git init --no-colocate]", got)
	}
}

func TestGitInitDirError(t *testing.T) {
	script := filepath.Join(t.TempDir(), "jj")
	body := "#!/bin/sh\necho 'not happy' >&2\nexit 2\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	err := GitInitDir(script, t.TempDir(), false)
	if err == nil {
		t.Fatal("expected an error from failing jj")
	}
	if !strings.Contains(err.Error(), "not happy") {
		t.Fatalf("error should surface stderr, got %v", err)
	}
	if !strings.Contains(err.Error(), "jj git init") {
		t.Fatalf("error should name the command, got %v", err)
	}
}

// TestGitInitReal exercises the prompt's end-to-end result against the real
// jj binary: after GitInitDir the directory is a working jj repo, and the
// colocate flag decides whether a plain top-level .git appears.
func TestGitInitReal(t *testing.T) {
	jjPath, err := findBinary("jj")
	if err != nil {
		t.Skipf("jj not in PATH: %v", err)
	}
	entries := func(dir string) map[string]bool {
		out, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, e := range out {
			names[e.Name()] = true
		}
		return names
	}

	dir := t.TempDir()
	if err := GitInitDir(jjPath, dir, false); err != nil {
		t.Fatalf("real jj git init --no-colocate failed: %v", err)
	}
	names := entries(dir)
	if !names[".jj"] {
		t.Fatalf(".jj directory missing in %s after init", dir)
	}
	if names[".git"] {
		t.Fatalf("--no-colocate still created a top-level .git in %s", dir)
	}

	dir2 := t.TempDir()
	if err := GitInitDir(jjPath, dir2, true); err != nil {
		t.Fatalf("real jj git init --colocate failed: %v", err)
	}
	if names := entries(dir2); !names[".jj"] || !names[".git"] {
		t.Fatalf("--colocate should create .jj and .git in %s, got %v", dir2, names)
	}
}
