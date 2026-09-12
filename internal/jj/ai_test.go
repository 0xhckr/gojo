package jj

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const aiTestDiff = "diff --git a/hello.txt b/hello.txt\n+hello\n"

func aiTestRunner(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	jjPath := filepath.Join(dir, "jj")
	if err := os.WriteFile(jjPath, []byte("#!/bin/sh\nprintf '%s' '"+aiTestDiff+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewRunner(Config{JJPath: jjPath, RepoRoot: dir})
}

// Only the external CLI is simulated: AIDescribe still fetches the diff,
// selects the provider, launches the process, and reads its final-message file.
func fakeCodex(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOJO_CODEX_RECORD", dir)
	script := `#!/bin/sh
printf '%s\n' "$@" > "$GOJO_CODEX_RECORD/args"
pwd > "$GOJO_CODEX_RECORD/cwd"
cat > "$GOJO_CODEX_RECORD/input"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    output="$1"
  fi
  shift
done
` + body
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAIDescribeCodex(t *testing.T) {
	for _, model := range []string{"", "codex-test-model"} {
		t.Run("model="+model, func(t *testing.T) {
			dir := fakeCodex(t, "echo 'progress, not a description'\necho 'diagnostic' >&2\nprintf ' Add greeting\\n' > \"$output\"\n")
			r := aiTestRunner(t)
			r.cfg.AIProvider = "codex"
			r.cfg.AIModel = model
			r.cfg.CommitPrompt = "Custom commit prompt:\n"
			message, err := r.AIDescribe("selected-revision")
			if err != nil || message != "Add greeting" {
				t.Fatalf("AIDescribe = %q, %v", message, err)
			}
			input, err := os.ReadFile(filepath.Join(dir, "input"))
			if err != nil || !strings.Contains(string(input), r.cfg.CommitPrompt+aiTestDiff) {
				t.Fatalf("prompt/diff not sent on stdin: %q, %v", input, err)
			}
			args, err := os.ReadFile(filepath.Join(dir, "args"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"exec\n", "--ephemeral\n", "--skip-git-repo-check\n", "--sandbox\nread-only\n", "--output-last-message\n"} {
				if !strings.Contains(string(args), want) {
					t.Errorf("argv missing %q: %s", want, args)
				}
			}
			if model == "" && strings.Contains(string(args), "--model") {
				t.Error("unset model must use Codex's default, not the API default")
			}
			if model != "" && !strings.Contains(string(args), "--model\n"+model+"\n") {
				t.Errorf("explicit model missing: %s", args)
			}
			cwd, err := os.ReadFile(filepath.Join(dir, "cwd"))
			if err != nil {
				t.Fatal(err)
			}
			workDir := strings.TrimSpace(string(cwd))
			if workDir == r.cfg.RepoRoot {
				t.Fatal("Codex should run outside the repository")
			}
			if _, err := os.Stat(workDir); !os.IsNotExist(err) {
				t.Errorf("temporary directory not removed: %s (%v)", workDir, err)
			}
		})
	}
}

func TestAIDescribeCodexFailures(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"login", "echo 'Please sign in with ChatGPT' >&2\nexit 1\n", "Please sign in with ChatGPT"},
		{"empty", "printf ' \\n' > \"$output\"\n", "Empty response from Codex"},
		{"missing-output", "echo 'progress only'\n", "did not produce a commit message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeCodex(t, tc.script)
			r := aiTestRunner(t)
			r.cfg.AIProvider = "codex"
			message, err := r.AIDescribe("@")
			if message != "" || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("AIDescribe = %q, %v; want %q", message, err, tc.want)
			}
		})
	}
	t.Run("missing-binary", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		r := aiTestRunner(t)
		r.cfg.AIProvider = "codex"
		_, err := r.AIDescribe("@")
		if err == nil || !strings.Contains(err.Error(), "Install Codex CLI") {
			t.Fatalf("missing binary: %v", err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		fakeCodex(t, "exec sleep 30\n")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := aiTestRunner(t).codexDescribe(ctx, "test")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline: %v", err)
		}
	})
}

func TestAIDescribeAPICompatibility(t *testing.T) {
	for _, provider := range []string{"", "api"} {
		t.Run("provider="+provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "POST" || req.URL.Path != "/chat/completions" || req.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
				}
				var body chatRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Model != DefaultAIModel || len(body.Messages) != 1 || body.Messages[0].Content != defaultCommitPrompt+aiTestDiff {
					t.Errorf("unexpected request body: %+v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Add greeting"}}]}`))
			}))
			defer server.Close()
			r := aiTestRunner(t)
			r.cfg.AIProvider = provider
			r.cfg.AIAPIKey = "test-key"
			r.cfg.AIBaseURL = server.URL
			message, err := r.AIDescribe("@")
			if err != nil || message != "Add greeting" {
				t.Fatalf("AIDescribe = %q, %v", message, err)
			}
		})
	}
}

func TestAIDescribeValidation(t *testing.T) {
	for _, tc := range []struct{ provider, want string }{
		{"", "No AI API key"},
		{"api", "No AI API key"},
		{"typo", "Unknown AI provider"},
	} {
		r := NewRunner(Config{AIProvider: tc.provider})
		_, err := r.AIDescribe("@")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("provider %q: %v; want %s", tc.provider, err, tc.want)
		}
	}
	r := aiTestRunner(t)
	r.cfg.AIProvider = "codex"
	if err := os.WriteFile(r.cfg.JJPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := r.AIDescribe("@")
	if err == nil || !strings.Contains(err.Error(), "No diff available") {
		t.Fatalf("empty diff: %v", err)
	}
}
