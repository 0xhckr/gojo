// Package jj wraps the jj (Jujutsu VCS) CLI: command execution, output
// parsing, configuration loading, and AI-assisted commit messages.
package jj

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// logTemplate is a two-line jj template using a \x01 marker byte to separate
// the graph prefix from structured field data. See parseLog for the layout.
const logTemplate = `"\x01" ++ change_id.short(8) ++ "|" ++ change_id.shortest() ++ "|" ++ commit_id.short(8) ++ "|" ++ commit_id.shortest() ++ "|" ++ author.email() ++ "|" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++ if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "\n" ++ "\x01" ++ description.first_line() ++ "\n"`

// LogEntry is one commit in the log, plus the surrounding graph rendering.
type LogEntry struct {
	ChangeID          string
	ChangeIDPrefixLen int
	CommitID          string
	CommitIDPrefixLen int
	Authors           string
	Date              string
	Subject           string
	Bookmarks         []string
	IsWorkingCopy     bool
	IsImmutable       bool
	HeaderPrefix      string
	BodyPrefix        string
	EdgeLines         []string
}

// StatusKind enumerates working-copy / diff file statuses.
type StatusKind int

const (
	StatusModified StatusKind = iota
	StatusAdded
	StatusRemoved
	StatusConflicted
)

// StatusEntry is a single changed file.
type StatusEntry struct {
	Path   string
	Status StatusKind
}

// Runner executes jj commands inside a repository.
type Runner struct {
	cfg Config
}

// NewRunner builds a Runner from resolved config.
func NewRunner(cfg Config) *Runner {
	return &Runner{cfg: cfg}
}

// Config returns the runner's configuration.
func (r *Runner) Config() Config { return r.cfg }

// run executes jj with the given args in the repo directory, returning stdout.
func (r *Runner) run(args ...string) (string, error) {
	cmd := exec.Command(r.cfg.JJPath, args...)
	cmd.Dir = r.cfg.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("jj %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// Log returns commits for jj's default revset, newest first, with graph
// data. A non-positive limit defaults to 50.
func (r *Runner) Log(limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	return r.LogRevset("", limit)
}

// LogRevset returns commits for the given revset (empty = jj's default
// revsets.log revset), newest first, with graph data.
//
// If limit > 0, a "-n <limit>" cap is sent. If limit <= 0, no cap is sent and
// jj streams every matching revision down to the root — use this only for
// bounded views (e.g. "all()" in modest repos), since the result set is held
// in memory. (jj 0.41 has no --skip flag and revsets have no offset operator,
// so true server-side pagination isn't available; rendering is windowed by the
// caller, so only visible rows are styled.)
func (r *Runner) LogRevset(revset string, limit int) ([]LogEntry, error) {
	args := []string{"log", "--color", "never", "-T", logTemplate}
	if limit > 0 {
		args = append(args, "-n", fmt.Sprint(limit))
	}
	if revset != "" {
		args = append(args, "-r", revset)
	}
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// Status returns the working-copy changed files.
func (r *Runner) Status() ([]StatusEntry, error) {
	out, err := r.run("status")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// Diff returns the unified (git-format) diff for a revision (or working copy).
func (r *Runner) Diff(rev string) (string, error) {
	args := []string{"diff", "--git", "--color", "never"}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	return r.run(args...)
}

// DiffSummary returns the changed-file summary for a revision.
func (r *Runner) DiffSummary(rev string) ([]StatusEntry, error) {
	out, err := r.run("diff", "--summary", "-r", rev)
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// FileShow returns the contents of a file at a revision.
func (r *Runner) FileShow(rev, path string) (string, error) {
	return r.run("file", "show", "-r", rev, path)
}

// Describe sets a revision's description.
func (r *Runner) Describe(rev, message string) error {
	_, err := r.run("describe", "-r", rev, "-m", message)
	return err
}

// Edit makes a revision the working copy.
func (r *Runner) Edit(rev string) error {
	_, err := r.run("edit", "-r", rev)
	return err
}

// New creates a new change, optionally on top of rev.
func (r *Runner) New(rev string) error {
	args := []string{"new"}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	_, err := r.run(args...)
	return err
}

// Abandon removes a revision.
func (r *Runner) Abandon(rev string) error {
	_, err := r.run("abandon", "-r", rev)
	return err
}

// Undo reverts the last operation.
func (r *Runner) Undo() error {
	_, err := r.run("undo")
	return err
}

// Redo re-applies an undone operation.
func (r *Runner) Redo() error {
	_, err := r.run("redo")
	return err
}

// Rebase moves a revision to a new location in the graph.
//
// srcFlag selects what moves: "-r" (the single revision) or "-s" (the revision
// and all of its descendants). placeFlag selects where it lands relative to
// dest: "--onto" (onto dest, as a child), "--insert-after", or "--insert-before".
func (r *Runner) Rebase(srcFlag, src, placeFlag, dest string) error {
	_, err := r.run("rebase", srcFlag, src, placeFlag, dest)
	return err
}

// BookmarkCreate creates a bookmark, optionally at rev.
func (r *Runner) BookmarkCreate(name, rev string) error {
	args := []string{"bookmark", "create", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	_, err := r.run(args...)
	return err
}

// BookmarkDelete deletes a bookmark.
func (r *Runner) BookmarkDelete(name string) error {
	_, err := r.run("bookmark", "delete", name)
	return err
}

// BookmarkForget forgets a bookmark.
func (r *Runner) BookmarkForget(name string) error {
	_, err := r.run("bookmark", "forget", name)
	return err
}

// BookmarkList lists bookmarks (raw text).
func (r *Runner) BookmarkList() (string, error) {
	return r.run("bookmark", "list")
}

// BookmarkMove moves a bookmark to rev.
func (r *Runner) BookmarkMove(name, rev string) error {
	_, err := r.run("bookmark", "move", name, "--to", rev)
	return err
}

// BookmarkRename renames a bookmark.
func (r *Runner) BookmarkRename(oldName, newName string) error {
	_, err := r.run("bookmark", "rename", oldName, newName)
	return err
}

// BookmarkSet sets a bookmark, optionally at rev.
func (r *Runner) BookmarkSet(name, rev string) error {
	args := []string{"bookmark", "set", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	_, err := r.run(args...)
	return err
}

// BookmarkTrack starts tracking a remote bookmark.
func (r *Runner) BookmarkTrack(name string) error {
	_, err := r.run("bookmark", "track", name)
	return err
}

// BookmarkUntrack stops tracking a remote bookmark.
func (r *Runner) BookmarkUntrack(name string) error {
	_, err := r.run("bookmark", "untrack", name)
	return err
}

// GitFetch fetches from the git remote.
func (r *Runner) GitFetch() error {
	_, err := r.run("git", "fetch")
	return err
}

// GitPush pushes to the git remote.
func (r *Runner) GitPush() error {
	_, err := r.run("git", "push")
	return err
}

// RemoteAdd adds a git remote.
func (r *Runner) RemoteAdd(name, url string) error {
	_, err := r.run("git", "remote", "add", name, url)
	return err
}

// RemoteList lists git remotes (raw text).
func (r *Runner) RemoteList() (string, error) {
	return r.run("git", "remote", "list")
}

// RemoteRemove removes a git remote.
func (r *Runner) RemoteRemove(name string) error {
	_, err := r.run("git", "remote", "remove", name)
	return err
}

// RemoteRename renames a git remote.
func (r *Runner) RemoteRename(oldName, newName string) error {
	_, err := r.run("git", "remote", "rename", oldName, newName)
	return err
}

// RemoteSetURL changes a git remote's URL.
func (r *Runner) RemoteSetURL(name, url string) error {
	_, err := r.run("git", "remote", "set-url", name, url)
	return err
}

// ── Parsers ───────────────────────────────────────────────────────────────

type parsedLine struct {
	prefix string
	data   string
	isData bool
}

func parseLog(raw string) []LogEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var parsed []parsedLine
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "" {
			continue
		}
		if idx := strings.IndexByte(trimmed, '\x01'); idx >= 0 {
			parsed = append(parsed, parsedLine{prefix: trimmed[:idx], data: trimmed[idx+1:], isData: true})
		} else {
			parsed = append(parsed, parsedLine{prefix: trimmed, isData: false})
		}
	}

	var entries []LogEntry
	var pendingEdges []string

	for i := 0; i < len(parsed); {
		p := parsed[i]
		if !p.isData {
			pendingEdges = append(pendingEdges, p.prefix)
			i++
			continue
		}

		fields := strings.Split(p.data, "|")
		if len(fields) < 9 {
			i++
			continue
		}

		// Attach pending edge lines to the previous commit.
		if len(entries) > 0 {
			entries[len(entries)-1].EdgeLines = pendingEdges
		}
		pendingEdges = nil

		var bookmarks []string
		if fields[8] != "" {
			for _, b := range strings.Split(fields[8], ",") {
				bookmarks = append(bookmarks, strings.TrimSuffix(b, "*"))
			}
		}

		entry := LogEntry{
			HeaderPrefix:      p.prefix,
			ChangeID:          fields[0],
			ChangeIDPrefixLen: len(fields[1]),
			CommitID:          fields[2],
			CommitIDPrefixLen: len(fields[3]),
			Authors:           fields[4],
			Date:              fields[5],
			IsWorkingCopy:     fields[6] == "Y",
			IsImmutable:       fields[7] == "Y",
			Bookmarks:         bookmarks,
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

	// Attach trailing edge lines to the last commit.
	if len(pendingEdges) > 0 && len(entries) > 0 {
		entries[len(entries)-1].EdgeLines = append(entries[len(entries)-1].EdgeLines, pendingEdges...)
	}

	return entries
}

func parseStatus(raw string) []StatusEntry {
	var entries []StatusEntry
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Working copy") || strings.HasPrefix(trimmed, "Parent commit") {
			continue
		}
		if len(trimmed) < 3 {
			continue
		}
		statusChar := trimmed[0]
		path := strings.TrimSpace(trimmed[1:])
		if path == "" {
			continue
		}
		switch statusChar {
		case 'M':
			entries = append(entries, StatusEntry{Path: path, Status: StatusModified})
		case 'A':
			entries = append(entries, StatusEntry{Path: path, Status: StatusAdded})
		case 'D':
			entries = append(entries, StatusEntry{Path: path, Status: StatusRemoved})
		case 'C':
			entries = append(entries, StatusEntry{Path: path, Status: StatusConflicted})
		}
	}
	return entries
}
