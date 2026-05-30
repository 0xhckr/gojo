# AGENTS.md — gojo

## What

gojo is a fullscreen TUI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS), written in Go using [Charm's Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lipgloss](https://github.com/charmbracelet/lipgloss), and [Bubbles](https://github.com/charmbracelet/bubbles).

## Project Structure

```
cmd/gojo/main.go        — entry point: loads config, starts Bubble Tea program
internal/
  config/config.go      — discovers jj binary and .jj repo root
  jj/runner.go          — runs jj CLI commands (log, status, diff, edit, new, etc.)
  ui/
    styles.go           — lipgloss color palette and style definitions
    model.go            — Bubble Tea model with 4 views
flake.nix               — nix flake: dev shell + package build
.envrc                  — direnv: `use flake`
.gitignore              — .direnv/, result, compiled binary
```

## Nix / Dev Environment

- **Go 1.26**, gopls, gotools, jujutsu (jj v0.41)
- `nix develop` drops you into the shell; direnv auto-loads it
- `nix build` produces the `gojo` binary

### Critical: the jj package name

nixpkgs has two packages named `jj`:

- `nixpkgs#jj` = [tidwall/jj](https://github.com/tidwall/jj) — a JSON stream editor (NOT what we want)
- `nixpkgs#jujutsu` = [jj-vcs/jj](https://github.com/jj-vcs/jj) — the VCS (what we want)

The flake uses `jujutsu` in `packages`. The user's `~/.nix-profile/bin/jj` is the VCS one (symlink to the jujutsu derivation). Always use the full path `/home/hackr/.nix-profile/bin/jj` or ensure the correct `jj` is first in PATH.

### Vendor hash

`vendorHash` in flake.nix must be updated whenever `go.mod` changes. To get the correct hash:

1. Set `vendorHash = pkgs.lib.fakeHash;`
2. Run `nix build 2>&1 | grep "got:"`
3. Copy the `got:` sha256 into `vendorHash`

## JJ Template Syntax

The `jj log` command uses jj's own template language (NOT Go templates). Key syntax:

- String concatenation: `++`
- String literals: `"text"`
- Newlines in output: `"\n"` (jj interprets this as a real newline)
- Field access: `change_id.short()`, `commit_id.short()`, `author.name()`
- Conditionals: `if(condition, "yes", "no")`
- Joins: `bookmarks.join(",")`
- Date formatting: `author.timestamp().local().format("%Y-%m-%d %H:%M")`

### Current log template (pipe-delimited, one line per commit)

```
change_id.short() ++ "|" ++ commit_id.short() ++ "|" ++ author.name() ++ "|" ++
author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++
if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++
bookmarks.join(",") ++ "|" ++ description.first_line() ++ "\n"
```

Fields (8 total, split on `|` with SplitN to preserve `|` in subjects):
0: change_id | 1: commit_id | 2: author | 3: date | 4: working_copy (Y/N) | 5: immutable (Y/N) | 6: bookmarks (comma-separated) | 7: subject

### JJ status output format

```
Working copy changes:
A file1
M file2
D file3
Working copy  (@) : <change_id> <commit_id> (<description>)
Parent commit (@-): ...
```

Parser skips lines starting with "Working copy" or "Parent commit". First char is status (A/M/D/C), rest is the path.

## Architecture

### Views

| View | Key | Description |
|------|-----|-------------|
| Log | `1` | Commit list, one line per commit. Cursor navigation with ↑/k ↓/j. |
| Status | `2` | Working copy file changes (A/M/D/C). |
| Diff | `enter`/`d` | Scrollable diff for selected commit using `viewport.Model`. |
| Help | `?` | Keybinding reference. |

### Layout (top to bottom)

1. Content area — fills all available space, padded with blank lines via `padToHeight()`
2. Status bar — error messages, status text, or current view name (full width, dark gray bg)
3. Help bar — keybinding hints (full width, dark gray bg)

Content height = `terminal height - help bar height - status bar height`.

### Log view specifics

- Top padding: one blank line at the top so the first commit isn't hidden behind chrome
- Each commit = one line: `▸ @ changeID subject  author date commitID bookmarks`
- `visibleEntries = contentHeight - 1` (accounting for the top padding line)

## Known Lipgloss Pitfalls

### ESC[0m resets background

**The problem:** lipgloss `Style.Render()` wraps each styled segment with `ESC[<codes>m<text>ESC[0m`. The `ESC[0m` is a full reset — it clears ALL attributes including any background color set by a parent style. So wrapping pre-styled text with `Background(color).Inline(true).Render()` does NOT preserve the background between styled segments.

**The fix:** `highlightLine()` in `model.go`:
1. Prepend the background escape code: `\x1b[48;5;<color>m`
2. After every `\x1b[0m` in the line, re-inject the background code
3. Pad to full width with spaces
4. Append a final `\x1b[0m` reset

This ensures the background color persists behind every styled segment.

### Width() on pre-styled text

`lipgloss.NewStyle().Width(w).Render(alreadyStyledText)` strips inner ANSI codes (even with `Inline(true)` the inner styles' `ESC[0m` kills the outer bg). Always compose styles before rendering, or use the `highlightLine` approach.

### No TTY = no colors

lipgloss detects the terminal at startup. When running outside a TTY (e.g. in tests, pipes), it strips all ANSI codes. To force color output:

```go
import "github.com/muesli/termenv"
lipgloss.SetColorProfile(termenv.TrueColor)
```

## Keybindings

### Global
- `1` — log view
- `2` — status view
- `?` — help
- `r` — refresh current view
- `q` / `ctrl+c` — quit (or go back from diff/help)

### Log View
- `↑`/`k`, `↓`/`j` — navigate commits
- `g` — jump to first
- `G` — jump to last
- `enter`/`d` — show diff for selected commit
- `e` — `jj edit` (set working copy to selected commit)
- `n` — `jj new` (create new change)

### Diff View
- `↑`/`k`, `↓`/`j` — scroll
- `pgup`/`b`, `pgdn`/`f` — half-page scroll
- `g`/`G` — scroll to top/bottom
- `q` — back to log

## JJ Runner (internal/jj)

All jj operations go through `Runner.run(ctx, args...)` which executes `jj` with the given args in the repo directory using `exec.CommandContext`. Output is captured via `CombinedOutput`. Errors include the full jj stderr.

Available operations: Log, Status, Diff, Describe, New, Edit, Abandon, Squash, BookmarkSet, BookmarkDelete.

## Color Palette

Defined in `internal/ui/styles.go`:

| Name | 256-color | Usage |
|------|-----------|-------|
| colorPurple | 135 | Change IDs, titles |
| colorDarkPurple | 91 | Selection background |
| colorBlue | 69 | Author names, diff headers |
| colorGreen | 78 | Bookmarks, "added" status |
| colorRed | 167 | Errors, "removed" status |
| colorYellow | 179 | Working copy, cursor |
| colorGray | 245 | Dates, commit IDs, help text |
| colorDarkGray | 238 | Help/status bar background |
| colorDarkerGray | 235 | Status bar background |
| colorWhite | 252 | Subjects, paths |
