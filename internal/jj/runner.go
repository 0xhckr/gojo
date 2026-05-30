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

// One line per commit, pipe-delimited.
// Fields: change_id|commit_id|author|date|wc|immutable|bookmarks|subject
const logTemplate = `change_id.short() ++ "|" ++ commit_id.short() ++ "|" ++ author.name() ++ "|" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++ if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "|" ++ description.first_line() ++ "\n"`

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
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Use SplitN so subjects containing "|" don't get split.
		fields := strings.SplitN(line, "|", 8)
		if len(fields) < 8 {
			continue
		}
		var bms []string
		if fields[6] != "" {
			bms = strings.Split(fields[6], ",")
		}
		entries = append(entries, LogEntry{
			ChangeID:      fields[0],
			CommitID:      fields[1],
			Authors:       fields[2],
			Date:          fields[3],
			IsWorkingCopy: fields[4] == "Y",
			IsImmutable:   fields[5] == "Y",
			Bookmarks:     bms,
			Subject:       fields[7],
		})
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
