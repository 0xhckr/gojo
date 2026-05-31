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

	// Shortest unique prefix lengths for shortcut highlighting.
	ChangeIDPrefixLen int
	CommitIDPrefixLen int

	// Graph rendering (from jj's graph output).
	// HeaderPrefix is the graph prefix for the metadata line (e.g. "│ ○  ").
	// The node character is always the last @ or ○ in this prefix.
	HeaderPrefix string
	// BodyPrefix is the graph prefix for the subject line (e.g. "│ │  ").
	BodyPrefix string
	// EdgeLines are pure graph edge lines rendered between this commit and the next.
	EdgeLines []string
}

// Two-line template with \x01 marker to separate graph prefix from data.
// Line 1: \x01 + pipe-delimited metadata
// Line 2: \x01 + subject
const logTemplate = `"\x01" ++ change_id.short() ++ "|" ++ change_id.shortest() ++ "|" ++ commit_id.short() ++ "|" ++ commit_id.shortest() ++ "|" ++ author.email() ++ "|" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++ if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "\n" ++ "\x01" ++ description.first_line() ++ "\n"`

// Log returns the revlog parsed into entries with graph info.
func (r *Runner) Log(ctx context.Context, revset string, limit int) ([]LogEntry, error) {
	args := []string{"log", "--color", "never", "-T", logTemplate}
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

	type parsedLine struct {
		prefix string // graph prefix before \x01
		data   string // data after \x01
		isData bool
	}

	var parsed []parsedLine
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		idx := strings.IndexByte(line, '\x01')
		if idx >= 0 {
			parsed = append(parsed, parsedLine{
				prefix: line[:idx],
				data:   line[idx+1:],
				isData: true,
			})
		} else {
			// Pure graph edge line (no data).
			parsed = append(parsed, parsedLine{prefix: line})
		}
	}

	// Group into commits.
	// Pattern: [edge_lines...] header_data body_data [edge_lines...] header_data body_data ...
	// Edge lines between commits are attached to the preceding commit.
	var entries []LogEntry
	var pendingEdges []string
	i := 0
	for i < len(parsed) {
		p := parsed[i]
		if !p.isData {
			pendingEdges = append(pendingEdges, p.prefix)
			i++
			continue
		}

		// Header line — parse pipe-delimited fields (9 fields, no subject).
		fields := strings.SplitN(p.data, "|", 9)
		if len(fields) < 9 {
			i++
			continue
		}

		// Attach pending edge lines to the previous commit.
		if len(entries) > 0 {
			entries[len(entries)-1].EdgeLines = pendingEdges
		}
		pendingEdges = nil

		var bms []string
		if fields[8] != "" {
			bms = strings.Split(fields[8], ",")
		}

		// shortest() returns the prefix string itself (e.g. "w"), not a number.
		changeIDPrefixLen := len(fields[1])
		commitIDPrefixLen := len(fields[3])

		entry := LogEntry{
			HeaderPrefix:      p.prefix,
			ChangeID:          fields[0],
			ChangeIDPrefixLen: changeIDPrefixLen,
			CommitID:          fields[2],
			CommitIDPrefixLen: commitIDPrefixLen,
			Authors:           fields[4],
			Date:              fields[5],
			IsWorkingCopy:     fields[6] == "Y",
			IsImmutable:       fields[7] == "Y",
			Bookmarks:         bms,
		}

		// Next line should be the body (subject).
		i++
		if i < len(parsed) && parsed[i].isData {
			entry.BodyPrefix = parsed[i].prefix
			entry.Subject = parsed[i].data
			i++
		}

		entries = append(entries, entry)
	}

	// Attach any trailing edge lines to the last commit.
	if len(pendingEdges) > 0 && len(entries) > 0 {
		entries[len(entries)-1].EdgeLines = append(entries[len(entries)-1].EdgeLines, pendingEdges...)
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
// DiffSummary returns the changed files for a given revision (like jj diff --summary -r <rev>).
func (r *Runner) DiffSummary(ctx context.Context, rev string) ([]StatusEntry, error) {
	args := []string{"diff", "--summary", "-r", rev}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

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

// BookmarkCreate creates a new bookmark at the given revision.
func (r *Runner) BookmarkCreate(ctx context.Context, name, rev string) error {
	args := []string{"bookmark", "create", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	_, err := r.run(ctx, args...)
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

// BookmarkForget forgets a bookmark without marking it for deletion.
func (r *Runner) BookmarkForget(ctx context.Context, name string) error {
	_, err := r.run(ctx, "bookmark", "forget", name)
	return err
}

// BookmarkList lists all bookmarks.
func (r *Runner) BookmarkList(ctx context.Context) (string, error) {
	return r.run(ctx, "bookmark", "list")
}

// BookmarkMove moves a bookmark to a target revision.
func (r *Runner) BookmarkMove(ctx context.Context, name, rev string) error {
	args := []string{"bookmark", "move", name}
	if rev != "" {
		args = append(args, "--to", rev)
	}
	_, err := r.run(ctx, args...)
	return err
}

// BookmarkRename renames a bookmark.
func (r *Runner) BookmarkRename(ctx context.Context, oldName, newName string) error {
	_, err := r.run(ctx, "bookmark", "rename", oldName, newName)
	return err
}

// BookmarkTrack starts tracking a remote bookmark.
func (r *Runner) BookmarkTrack(ctx context.Context, name string) error {
	_, err := r.run(ctx, "bookmark", "track", name)
	return err
}

// BookmarkUntrack stops tracking a remote bookmark.
func (r *Runner) BookmarkUntrack(ctx context.Context, name string) error {
	_, err := r.run(ctx, "bookmark", "untrack", name)
	return err
}

// Undo undoes the most recent operation.
func (r *Runner) Undo(ctx context.Context) error {
	_, err := r.run(ctx, "undo")
	return err
}
