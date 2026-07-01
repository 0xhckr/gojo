// Package jj wraps the jj (Jujutsu VCS) CLI: command execution, output
// parsing, configuration loading, and AI-assisted commit messages.
package jj

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// logTemplate is a two-line jj template using a \x01 marker byte to separate
// the graph prefix from structured field data. See parseLog for the layout.
const logTemplate = `"\x01" ++ change_id.short(8) ++ "|" ++ change_id.shortest() ++ "|" ++ commit_id.short(8) ++ "|" ++ commit_id.shortest() ++ "|" ++ author.email() ++ "|" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++ if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "|" ++ tags.join(",") ++ "\n" ++ "\x01" ++ description.first_line() ++ "\n"`

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
	Tags              []string
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

// Description returns the full commit-message text for a revision (may be
// empty). The rev is any valid revset (change ID, commit ID, "@", …).
func (r *Runner) Description(rev string) (string, error) {
	out, err := r.run("log", "-r", rev, "--no-graph", "-T", "description", "--color", "never")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// FileShow returns the contents of a file at a revision.
func (r *Runner) FileShow(rev, path string) (string, error) {
	return r.run("file", "show", "-r", rev, path)
}

// annotateTemplate emits one record per source line: blame fields joined by
// '|', a \x01 marker, then the line's content (which jj includes with its
// trailing newline). Bare keywords (commit, line_number, content) are the
// AnnotationLine's no-arg methods; the Commit methods take parens. The
// description (5th field) is parsed with a split limit so a '|' inside it is
// preserved.
const annotateTemplate = `commit.change_id().short(8) ++ "|" ++ commit.commit_id().short(8) ++ "|" ++ commit.author().email() ++ "|" ++ line_number ++ "|" ++ commit.description().first_line() ++ "\x01" ++ content`

// AnnotateLine is one line of a file plus the commit that last touched it.
type AnnotateLine struct {
	ChangeID    string
	CommitID    string
	Author      string
	LineNo      int
	Description string // first line of the commit message (may be empty)
	Text        string
}

// FileList lists the tracked files in the working-copy revision (@).
func (r *Runner) FileList() ([]string, error) {
	out, err := r.run("file", "list")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// FileAnnotate returns per-line blame for a file at a revision ("" = @).
func (r *Runner) FileAnnotate(path, rev string) ([]AnnotateLine, error) {
	args := []string{"file", "annotate", "-T", annotateTemplate}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	args = append(args, path)
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	return parseAnnotate(out), nil
}

// FileLog returns the commit log restricted to revisions that touched path.
// revset defaults to "all()". The path filters the revset down to touching
// commits. limit <= 0 streams every matching revision.
func (r *Runner) FileLog(path, revset string, limit int) ([]LogEntry, error) {
	if revset == "" {
		revset = "all()"
	}
	args := []string{"log", "--color", "never", "-T", logTemplate}
	if limit > 0 {
		args = append(args, "-n", fmt.Sprint(limit))
	}
	args = append(args, "-r", revset, "--", path)
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// appendExtra appends only non-empty entries from extra to args. Empty
// strings would otherwise become stray positional arguments (breaking
// variadic-name subcommands like `bookmark set <NAMES>...`).
func appendExtra(args []string, extra []string) []string {
	for _, e := range extra {
		if e != "" {
			args = append(args, e)
		}
	}
	return args
}

// Describe sets a revision's description. Extra flags (e.g.
// "--ignore-immutable") are appended for elevation retries.
func (r *Runner) Describe(rev, message string, extra ...string) error {
	args := appendExtra([]string{"describe", "-r", rev, "-m", message}, extra)
	_, err := r.run(args...)
	return err
}

// Edit makes a revision the working copy. Extra flags are appended for
// elevation retries.
func (r *Runner) Edit(rev string, extra ...string) error {
	args := appendExtra([]string{"edit", "-r", rev}, extra)
	_, err := r.run(args...)
	return err
}

// New creates a new change, optionally on top of rev. Extra flags are appended
// for elevation retries.
func (r *Runner) New(rev string, extra ...string) error {
	args := []string{"new"}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	args = appendExtra(args, extra)
	_, err := r.run(args...)
	return err
}

// WorkingCopyEntry returns the log entry for the working copy (@). Used after
// `jj new` to discover the newly created revision's change/commit IDs.
func (r *Runner) WorkingCopyEntry() (*LogEntry, error) {
	out, err := r.run("log", "-r", "@", "--no-graph", "-T", logTemplate, "--color", "never")
	if err != nil {
		return nil, err
	}
	entries := parseLog(out)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no working copy found")
	}
	return &entries[0], nil
}

// Abandon removes a revision. Extra flags are appended for elevation retries.
func (r *Runner) Abandon(rev string, extra ...string) error {
	args := appendExtra([]string{"abandon", "-r", rev}, extra)
	_, err := r.run(args...)
	return err
}

// Absorb moves changes from a source revision into the closest mutable
// ancestors where the corresponding lines were modified last. If from is
// empty, jj defaults to the working copy (@). Extra flags are appended for
// elevation retries.
func (r *Runner) Absorb(from string, extra ...string) error {
	args := []string{"absorb"}
	if from != "" {
		args = append(args, "--from", from)
	}
	args = appendExtra(args, extra)
	_, err := r.run(args...)
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
// Extra flags are appended for elevation retries.
func (r *Runner) Rebase(srcFlag, src, placeFlag, dest string, extra ...string) error {
	args := appendExtra([]string{"rebase", srcFlag, src, placeFlag, dest}, extra)
	_, err := r.run(args...)
	return err
}

// BookmarkCreate creates a bookmark, optionally at rev. Extra flags are
// appended for elevation retries.
func (r *Runner) BookmarkCreate(name, rev string, extra ...string) error {
	args := []string{"bookmark", "create", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	args = appendExtra(args, extra)
	_, err := r.run(args...)
	return err
}

// BookmarkDelete deletes a bookmark. Extra flags are appended for elevation
// retries.
func (r *Runner) BookmarkDelete(name string, extra ...string) error {
	args := appendExtra([]string{"bookmark", "delete", name}, extra)
	_, err := r.run(args...)
	return err
}

// BookmarkForget forgets a bookmark. Extra flags are appended for elevation
// retries.
func (r *Runner) BookmarkForget(name string, extra ...string) error {
	args := appendExtra([]string{"bookmark", "forget", name}, extra)
	_, err := r.run(args...)
	return err
}

// BookmarkList lists bookmarks (raw text).
func (r *Runner) BookmarkList() (string, error) {
	return r.run("bookmark", "list")
}

// BookmarkMove moves a bookmark to rev. Extra flags (e.g.
// "--allow-backwards") are appended for elevation retries.
func (r *Runner) BookmarkMove(name, rev string, extra ...string) error {
	args := appendExtra([]string{"bookmark", "move", name, "--to", rev}, extra)
	_, err := r.run(args...)
	return err
}

// BookmarkRename renames a bookmark.
func (r *Runner) BookmarkRename(oldName, newName string) error {
	_, err := r.run("bookmark", "rename", oldName, newName)
	return err
}

// BookmarkSet sets a bookmark, optionally at rev. Extra flags (e.g.
// "--allow-backwards") are appended for elevation retries.
func (r *Runner) BookmarkSet(name, rev string, extra ...string) error {
	args := []string{"bookmark", "set", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	args = appendExtra(args, extra)
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

// TagList lists tags (raw text).
func (r *Runner) TagList() (string, error) {
	return r.run("tag", "list")
}

// TagSet creates or updates a tag pointing at rev. Extra flags (e.g.
// "--allow-move") are appended for elevation retries.
func (r *Runner) TagSet(name, rev string, extra ...string) error {
	args := []string{"tag", "set", name}
	if rev != "" {
		args = append(args, "-r", rev)
	}
	args = appendExtra(args, extra)
	_, err := r.run(args...)
	return err
}

// TagDelete deletes a tag. Extra flags are appended for elevation retries.
func (r *Runner) TagDelete(name string, extra ...string) error {
	args := appendExtra([]string{"tag", "delete", name}, extra)
	_, err := r.run(args...)
	return err
}

// GitPushTags pushes all tags to the default remote using git directly. jj's
// `git push` doesn't support pushing new tags in 0.41, so this shells out to
// `git push --tags` as a workaround.
func (r *Runner) GitPushTags() error {
	if r.cfg.GitPath == "" {
		return fmt.Errorf("git not found in PATH")
	}
	cmd := exec.Command(r.cfg.GitPath, "push", "--tags")
	cmd.Dir = r.cfg.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git push --tags: %s", msg)
	}
	return nil
}

// GitFetch fetches from the git remote. Extra flags are appended for elevation
// retries.
func (r *Runner) GitFetch(extra ...string) error {
	args := appendExtra([]string{"git", "fetch"}, extra)
	_, err := r.run(args...)
	return err
}

// GitPush pushes to the git remote. Extra flags are appended for elevation
// retries.
func (r *Runner) GitPush(extra ...string) error {
	args := appendExtra([]string{"git", "push"}, extra)
	_, err := r.run(args...)
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

// SplitPaths splits a revision by moving changes to the specified paths into a
// new preceding revision. A -m flag is passed with an empty message so jj does
// not open $EDITOR for the split commit's description. Returns the change ID
// of the newly created (selected) revision. Extra flags are appended for
// elevation retries.
func (r *Runner) SplitPaths(rev string, paths []string, extra ...string) (string, error) {
	args := []string{"split", "-r", rev, "-m", ""}
	args = append(args, paths...)
	args = appendExtra(args, extra)
	out, err := r.run(args...)
	if err != nil {
		return "", err
	}
	return parseSplitSelected(out), nil
}

// ParentCommit returns the commit ID of the parent of the given revision.
// Used by split to fetch parent file contents for intermediate-version
// computation.
func (r *Runner) ParentCommit(rev string) (string, error) {
	out, err := r.run("log", "-r", "parents("+rev+")", "--no-graph", "-T", "commit_id", "--color", "never")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SplitInteractive splits a revision using a custom diff-editor tool. The tool
// receives $LEFT (parent tree), $RIGHT (current tree), and $OUTPUT (where to
// write the preceding revision's content). The intermediateDir contains
// pre-computed file versions for the preceding revision; oldPaths are paths to
// remove from $OUTPUT (for renamed files whose old path should not appear in
// the preceding revision). Returns the change ID of the newly created
// (selected) revision. Extra flags are appended for elevation retries.
func (r *Runner) SplitInteractive(rev, toolPath, intermediateDir string, oldPaths []string, extra ...string) (string, error) {
	cfgStrs := []string{
		`merge-tools.gojo-split.program="` + toolPath + `"`,
		`merge-tools.gojo-split.edit-args=["$left", "$right", "$output"]`,
	}
	args := []string{}
	for _, c := range cfgStrs {
		args = append(args, "--config", c)
	}
	args = append(args, "split", "-r", rev, "--interactive", "--tool", "gojo-split", "-m", "")
	args = appendExtra(args, extra)

	cmd := exec.Command(r.cfg.JJPath, args...)
	cmd.Dir = r.cfg.RepoRoot
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GOJO_INTERMEDIATE="+intermediateDir)
	if len(oldPaths) > 0 {
		cmd.Env = append(cmd.Env, "GOJO_OLD_PATHS="+strings.Join(oldPaths, "\n"))
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("jj split: %s", msg)
	}
	return parseSplitSelected(stdout.String()), nil
}

// parseSplitSelected extracts the change ID of the "Selected changes" revision
// from `jj split` stdout. The output line looks like:
//
//	Selected changes : <change_id> <commit_id> ...
//
// Returns "" if the line is not found.
func parseSplitSelected(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Selected changes :") {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				return fields[3]
			}
		}
	}
	return ""
}

// ── Elevation ─────────────────────────────────────────────────────────────

// DetectElevation inspects a jj error message. If it matches a known
// "operation refused — use --flag" pattern, it returns the flag an elevated
// retry should append plus a short human reason. Otherwise it returns "".
//
// Currently recognized:
//   - "... is immutable"        → --ignore-immutable
//   - "backwards or sideways"   → --allow-backwards
//   - "refusing to move tag"    → --allow-move
//
// Matching is on lowercased substrings so it survives jj rewording the
// surrounding text across versions.
func DetectElevation(errStr string) (flag, reason string) {
	s := strings.ToLower(errStr)
	switch {
	case strings.Contains(s, "is immutable"):
		return "--ignore-immutable", "target is immutable"
	case strings.Contains(s, "backwards or sideways"):
		return "--allow-backwards", "bookmark moves backwards/sideways"
	case strings.Contains(s, "refusing to move tag"):
		return "--allow-move", "tag already exists"
	}
	return "", ""
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
		if len(fields) < 10 {
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
				b = strings.TrimSuffix(b, "*")
				b = strings.TrimSuffix(b, "??")
				bookmarks = append(bookmarks, b)
			}
		}

		var tags []string
		if fields[9] != "" {
			for _, tg := range strings.Split(fields[9], ",") {
				tg = strings.TrimSuffix(tg, "*")
				tg = strings.TrimSuffix(tg, "??")
				tags = append(tags, tg)
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
			Tags:              tags,
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

func parseAnnotate(raw string) []AnnotateLine {
	var lines []AnnotateLine
	for _, rec := range strings.Split(raw, "\n") {
		// Each record carries its source line's own trailing newline, so
		// splitting on "\n" yields one record per element (plus a trailing ""
		// when the file ends with a newline).
		if rec == "" {
			continue
		}
		idx := strings.IndexByte(rec, '\x01')
		if idx < 0 {
			continue
		}
		meta := rec[:idx]
		text := rec[idx+1:]
		// SplitN with a limit of 5 so the description (last field) keeps any
		// '|' it contains.
		fields := strings.SplitN(meta, "|", 5)
		if len(fields) < 5 {
			continue
		}
		ln, _ := strconv.Atoi(fields[3])
		lines = append(lines, AnnotateLine{
			ChangeID:    fields[0],
			CommitID:    fields[1],
			Author:      fields[2],
			LineNo:      ln,
			Description: fields[4],
			Text:        text,
		})
	}
	return lines
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
