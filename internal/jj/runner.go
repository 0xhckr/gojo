package jj

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes jj commands in a repo.
type Runner struct {
	jjPath  string
	repoDir string
}

// NewRunner creates a new jj command runner.
func NewRunner(jjPath, repoDir string) *Runner {
	return &Runner{jjPath: jjPath, repoDir: repoDir}
}

func (r *Runner) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.jjPath, args...)
	cmd.Dir = r.repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("jj %s: %w: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// LogEntry represents a single commit from jj log.
type LogEntry struct {
	ChangeID      string
	CommitID      string
	Authors       string
	Date          string
	Subject       string
	Bookmarks     []string
	IsWorkingCopy bool
	IsImmutable   bool
}

// Each field is separated by a record separator (\x1e) and entries by \x1f.
const logTemplate = `change_id.short() ++ "\x1e" ++ commit_id.short() ++ "\x1e" ++ author.name() ++ "\x1e" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "\x1e" ++ if(current_working_copy, "Y", "N") ++ "\x1e" ++ if(immutable, "Y", "N") ++ "\x1e" ++ bookmarks.join(",") ++ "\x1e" ++ description.first_line() ++ "\x1f"` + "\n"

// Log returns the revlog parsed into entries.
func (r *Runner) Log(ctx context.Context, revset string, limit int) ([]LogEntry, error) {
	args := []string{"log", "--no-graph", "-T", logTemplate}
	if revset != "" {
		args = append(args, "-r", revset)
	}
	if limit > 0 {
		args = append(args, "-n", fmt.Sprintf("%d", limit))
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

func parseLog(raw string) []LogEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var entries []LogEntry
	// Split by the separator we know jj will output literally: \x1f
	// But jj outputs the literal characters from the template. Since we have \x1f
	// as a Go escape in the template string, let's just split by the actual separator.
	// Actually, jj templates output literal \x1f bytes from the "\x1f" in the template.
	// Wait - jj templates don't interpret Go escapes. We need to use jj's own escaping.
	// Let's use a simple approach: split by newlines, then split fields by tab.
	// Actually, the \x1f is interpreted by jj template engine as a literal byte? Let me check.
	// The template has "\x1f" which jj would render as the literal string \x1f or the byte?
	// In jj templates, "\x1f" is a string literal containing the byte 0x1F.
	// So we can split on that byte.
	blocks := strings.Split(raw, "\x1f")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		fields := strings.Split(block, "\x1e")
		if len(fields) < 8 {
			continue
		}
		var bms []string
		if fields[6] != "" {
			bms = strings.Split(fields[6], ",")
		}
		entry := LogEntry{
			ChangeID:      fields[0],
			CommitID:      fields[1],
			Authors:       fields[2],
			Date:          fields[3],
			IsWorkingCopy: fields[4] == "Y",
			IsImmutable:   fields[5] == "Y",
			Bookmarks:     bms,
			Subject:       fields[7],
		}
		entries = append(entries, entry)
	}
	return entries
}

// StatusEntry represents a changed file.
type StatusEntry struct {
	Path   string
	Status string // Added, Modified, Removed, Conflicted
}

// Status returns working copy file status.
func (r *Runner) Status(ctx context.Context) ([]StatusEntry, error) {
	out, err := r.run(ctx, "status")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// jj status output looks like:
//
//	Working copy changes:
//	A file1
//	M file2
//	D file3
//
// But actually jj outputs just the file lines. Let's parse robustly.
func parseStatus(raw string) []StatusEntry {
	var entries []StatusEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip header lines
		if strings.HasPrefix(line, "Working copy") || strings.HasPrefix(line, "Parent commit") {
			continue
		}
		if len(line) < 3 {
			continue
		}
		// Format: "A path/to/file"
		statusChar := line[0]
		path := strings.TrimSpace(line[1:])
		if path == "" {
			continue
		}
		switch statusChar {
		case 'M':
			entries = append(entries, StatusEntry{Path: path, Status: "Modified"})
		case 'A':
			entries = append(entries, StatusEntry{Path: path, Status: "Added"})
		case 'D':
			entries = append(entries, StatusEntry{Path: path, Status: "Removed"})
		case 'C':
			entries = append(entries, StatusEntry{Path: path, Status: "Conflicted"})
		}
	}
	return entries
}

// Diff returns the diff output for a given revision.
func (r *Runner) Diff(ctx context.Context, rev string) (string, error) {
	args := []string{"diff", "--color", "always"}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	return r.run(ctx, args...)
}

// Describe updates the description of a revision.
func (r *Runner) Describe(ctx context.Context, rev, message string) error {
	_, err := r.run(ctx, "describe", "-r", rev, "-m", message)
	return err
}

// New creates a new change on top of the given revision.
func (r *Runner) New(ctx context.Context, rev string) error {
	args := []string{"new"}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	_, err := r.run(ctx, args...)
	return err
}

// Edit sets the working copy to a given revision.
func (r *Runner) Edit(ctx context.Context, rev string) error {
	_, err := r.run(ctx, "edit", "-r", rev)
	return err
}

// Abandon abandons a revision.
func (r *Runner) Abandon(ctx context.Context, rev string) error {
	_, err := r.run(ctx, "abandon", "-r", rev)
	return err
}

// Squash squashes a revision into its parent.
func (r *Runner) Squash(ctx context.Context, rev string) error {
	_, err := r.run(ctx, "squash", "-r", rev)
	return err
}

// BookmarkSet creates or moves a bookmark.
func (r *Runner) BookmarkSet(ctx context.Context, name, rev string) error {
	args := []string{"bookmark", "set", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	_, err := r.run(ctx, args...)
	return err
}

// BookmarkDelete deletes a bookmark.
func (r *Runner) BookmarkDelete(ctx context.Context, name string) error {
	_, err := r.run(ctx, "bookmark", "delete", name)
	return err
}
