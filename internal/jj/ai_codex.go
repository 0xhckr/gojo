package jj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// codexDescribe reuses Codex's saved login; Codex owns credential storage and
// refresh. A private working directory avoids loading the repository's agent
// instructions, and the final-message file keeps progress output out of the
// commit description. Each generation has its own directory for concurrent use.
func (r *Runner) codexDescribe(ctx context.Context, prompt string) (string, error) {
	codex, err := exec.LookPath("codex")
	if err != nil {
		return "", errors.New("Codex not found in PATH. Install Codex CLI, then run codex login and sign in with ChatGPT")
	}
	dir, err := os.MkdirTemp("", "gojo-codex-*")
	if err != nil {
		return "", fmt.Errorf("Codex: %w", err)
	}
	defer os.RemoveAll(dir)
	output := filepath.Join(dir, "message.txt")
	args := []string{
		"exec", "--ephemeral", "--skip-git-repo-check",
		"--sandbox", "read-only", "--color", "never",
		"--output-last-message", output,
	}
	if r.cfg.AIModel != "" {
		args = append(args, "--model", r.cfg.AIModel)
	}
	args = append(args, "-")
	cmd := exec.CommandContext(ctx, codex, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("Use only the supplied diff to write the commit message. Do not use tools or run commands.\n\n" + prompt)
	cmd.WaitDelay = 2 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("Codex generation: %w", ctx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		// Keep the tail: Codex prints startup/progress information before errors.
		if len(detail) > 600 {
			detail = "…" + detail[len(detail)-600:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("Codex failed (check codex login status): %s", detail)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		return "", fmt.Errorf("Codex did not produce a commit message: %w", err)
	}
	message := strings.TrimSpace(string(raw))
	if message == "" {
		return "", errors.New("Empty response from Codex")
	}
	return message, nil
}
