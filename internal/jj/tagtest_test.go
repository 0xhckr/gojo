package jj

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tagTestEnv builds a temp jj repo with a bare git remote ("origin") and
// returns a runner for it. Skips when jj or git is missing, or when the jj
// version predates native tag pushing (jj ≥ 0.44: `jj git push --tag`).
func tagTestEnv(t *testing.T) (r *Runner, remoteDir string) {
	t.Helper()
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		t.Skip("jj not in PATH")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not in PATH")
	}

	out, err := exec.Command(jjPath, "git", "push", "--help").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--tag") {
		t.Skip("jj < 0.44: no native tag push support")
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "repo")
	remoteDir = filepath.Join(tmp, "remote.git")

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

	if out, err := exec.Command(gitPath, "init", "--bare", remoteDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	if out, err := exec.Command(jjPath, "git", "init", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("jj git init: %v\n%s", err, out)
	}
	jjRaw("git", "remote", "add", "origin", remoteDir)

	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	jjRaw("describe", "-m", "base")

	cfg := Config{JJPath: jjPath, RepoRoot: repoDir}
	return NewRunner(cfg), remoteDir
}

// remoteTags lists <name>=<commit> refs in the bare remote.
func remoteTags(t *testing.T, remoteDir string) map[string]string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remoteDir, "for-each-ref",
		"refs/tags", "--format=%(refname:strip=2) %(objectname)").CombinedOutput()
	if err != nil {
		t.Fatalf("git for-each-ref: %v\n%s", err, out)
	}
	tags := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			tags[f[0]] = f[1]
		}
	}
	return tags
}

// TestGitPushTags drives the jj-native tag push: an unpushed tag lands on the
// remote, a moved tag updates it, and the no-tags case is a silent no-op.
func TestGitPushTags(t *testing.T) {
	r, remoteDir := tagTestEnv(t)

	// No tags at all → no-op, no error.
	if err := r.GitPushTags(); err != nil {
		t.Fatalf("push with no tags: %v", err)
	}

	// Local tag → pushed.
	if err := r.TagSet("v1.0", "@"); err != nil {
		t.Fatalf("tag set: %v", err)
	}
	if err := r.GitPushTags(); err != nil {
		t.Fatalf("push tags: %v", err)
	}
	before := remoteTags(t, remoteDir)["v1.0"]
	if before == "" {
		t.Fatal("remote missing v1.0 after push")
	}

	// Update the tag target → push moves the remote ref.
	if err := os.WriteFile(filepath.Join(r.cfg.RepoRoot, "f.txt"), []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.TagSet("v1.0", "@", "--allow-move"); err != nil {
		t.Fatalf("tag move: %v", err)
	}
	if err := r.GitPushTags(); err != nil {
		t.Fatalf("push moved tag: %v", err)
	}
	after := remoteTags(t, remoteDir)["v1.0"]
	if after == "" {
		t.Fatal("remote lost v1.0 after move push")
	}
	if after == before {
		t.Fatal("remote v1.0 did not move after tag update")
	}

	// Up to date → no-op, no error.
	if err := r.GitPushTags(); err != nil {
		t.Fatalf("re-push tags: %v", err)
	}
}
