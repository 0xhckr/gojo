package jj

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// ImageFile reads a literal repository path without allowing large binary blobs
// to consume unbounded memory. rev is pinned by the diff loader.
func (r *Runner) ImageFile(rev, path string) ([]byte, error) {
	const limit = 16 << 20
	cmd := exec.Command(r.cfg.JJPath, "file", "show", "-r", rev, "--", "root:"+path)
	cmd.Dir = r.cfg.RepoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if len(data) > limit || readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("image exceeds 16 MiB")
	}
	if err = cmd.Wait(); err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

// ImageParent only returns an unambiguous parent. A merge's before image must
// never silently come from an arbitrary parent instead of its merged tree.
func (r *Runner) ImageParent(rev string) (string, error) {
	out, err := r.run("log", "--no-graph", "--color", "never", "-r", "parents("+strconv.Quote(rev)+")", "-T", `commit_id ++ "\n"`)
	if err != nil {
		return "", err
	}
	parents := strings.Fields(out)
	if len(parents) != 1 {
		return "", fmt.Errorf("merged parent preview unavailable")
	}
	return parents[0], nil
}
