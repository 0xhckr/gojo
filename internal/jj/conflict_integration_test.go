package jj

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// conflictTestRepo builds a temp jj repo with a 2-sided conflicted merge
// commit and returns the runner plus the merge commit's change ID. Skipped
// when no jj binary is available.
func conflictTestRepo(t *testing.T) (*Runner, string) {
	t.Helper()
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		t.Skip("jj not in PATH")
	}
	repoDir := filepath.Join(t.TempDir(), "repo")

	jjRaw := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(jjPath, args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "JJ_USER=test", "JJ_EMAIL=test@test")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	changeID := func() string {
		return jjRaw("log", "-r", "@", "--no-graph", "-T", "change_id.short(8)", "--color", "never")
	}

	if out, err := exec.Command(jjPath, "git", "init", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("jj git init: %v\n%s", err, out)
	}

	write("file.txt", "one\ntwo\nthree\n")
	jjRaw("describe", "-m", "base")
	baseRev := changeID()

	jjRaw("new", "-m", "left")
	write("file.txt", "one\nTWO-LEFT\nthree\n")
	leftRev := changeID()

	jjRaw("edit", baseRev)
	jjRaw("new", "-m", "right")
	write("file.txt", "one\ntwo\nTHREE-RIGHT\n")
	rightRev := changeID()

	jjRaw("new", "-m", "merge", leftRev, rightRev)
	mergeRev := changeID()

	cfg := Config{JJPath: jjPath, RepoRoot: repoDir}
	return NewRunner(cfg), mergeRev
}

func TestConflictRoundTrip(t *testing.T) {
	r, mergeRev := conflictTestRepo(t)

	entries, err := r.Conflicts(mergeRev)
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "file.txt" || entries[0].Sides != 2 {
		t.Fatalf("Conflicts = %+v, want file.txt 2-sided", entries)
	}

	sides, err := r.FetchConflictSides(mergeRev, "file.txt")
	if err != nil {
		t.Fatalf("FetchConflictSides: %v", err)
	}
	if sides.Left != "one\nTWO-LEFT\nthree\n" {
		t.Errorf("left = %q", sides.Left)
	}
	if sides.Base != "one\ntwo\nthree\n" {
		t.Errorf("base = %q", sides.Base)
	}
	if sides.Right != "one\ntwo\nTHREE-RIGHT\n" {
		t.Errorf("right = %q", sides.Right)
	}

	// The probe must leave the conflict intact.
	entries, err = r.Conflicts(mergeRev)
	if err != nil {
		t.Fatalf("Conflicts after probe: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("probe destroyed the conflict: %+v", entries)
	}

	// Resolve to the left side, then confirm the conflict is gone and the
	// file content matches.
	if err := r.Resolve(mergeRev, "file.txt", "one\nTWO-LEFT\nthree\n"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	entries, err = r.Conflicts(mergeRev)
	if err != nil {
		t.Fatalf("Conflicts after resolve: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("resolve did not clear the conflict: %+v", entries)
	}
	got, err := r.FileShow(mergeRev, "file.txt")
	if err != nil {
		t.Fatalf("FileShow: %v", err)
	}
	if got != "one\nTWO-LEFT\nthree\n" {
		t.Errorf("resolved content = %q", got)
	}
}
