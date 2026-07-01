# AGENTS.md — gojo

## What

gojo is a fullscreen TUI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS),
written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
(Elm-architecture TUI framework) and [Lip Gloss](https://github.com/charmbracelet/lipgloss)
(styling). Diff syntax highlighting uses [chroma](https://github.com/alecthomas/chroma).

## Project Structure

```
main.go                 — entry point: tea.NewProgram(ui.NewModel(), WithAltScreen)
internal/
  jj/
    jj.go               — Runner: runs jj CLI commands, parses log/status output
    config.go           — Config struct, repo-root discovery, minimal TOML loader
    ai.go               — AIDescribe: OpenAI-compatible chat-completions client (net/http)
  ui/
    model.go            — Bubble Tea Model: state, Update (msgs + keys), View, commands
    render.go           — seg/renderSegs/clip/bgRow: styled-line composition helpers
    styles.go           — color palette, spinner frames, diff colors
    logview.go          — commit list rendering + variable-height scroll windowing
    diff.go             — git unified-diff parser + chroma highlighting → diffRow
    diffpanel.go        — diff viewer rendering (gutter, status, file/hunk/line rows)
    helpview.go         — keybinding reference + scroll
go.mod / go.sum         — module `gojo`, deps: bubbletea, lipgloss, chroma, x/ansi
flake.nix               — nix flake: devShell (go, gopls, jujutsu) + buildGoModule package
VERSION                 — single source of truth for the version (flake + goreleaser read it)
.envrc                  — direnv: `use flake`
```

## Nix / Dev Environment

- **Go 1.24+** (build)
- **gopls**, **go-tools** (tooling)
- **jj v0.41** (jujutsu VCS, runtime dependency — gojo shells out to it)
- `nix develop` drops you into the shell; direnv auto-loads it.

### Critical: the jj package name

nixpkgs has two packages named `jj`:

- `nixpkgs#jj` = [tidwall/jj](https://github.com/tidwall/jj) — a JSON stream editor (NOT what we want)
- `nixpkgs#jujutsu` = [jj-vcs/jj](https://github.com/jj-vcs/jj) — the VCS (what we want)

The flake uses `jujutsu`. Always ensure the correct `jj` is first in PATH.

## Running

```bash
go run .          # dev
go build -o gojo . && ./gojo
go test ./...     # unit + integration tests (TestSmoke needs a jj repo)
```

The TUI uses the alternate screen buffer. Tests drive the `Model` headlessly
by feeding messages to `Update` and asserting on `View()` (see
`internal/ui/model_test.go`) — no PTY required.

## JJ Template Syntax

`Runner.Log` uses jj's template language. Key syntax:

- String concatenation: `++`
- String literals: `"text"`; newlines `"\n"`; marker byte `"\x01"`
- Field access: `change_id.short(8)`, `commit_id.short(8)`, `author.email()`
- Conditionals: `if(condition, "yes", "no")`
- Joins: `bookmarks.join(",")`
- Date: `author.timestamp().local().format("%Y-%m-%d %H:%M")`

### Current log template (jj.go `logTemplate`)

A literal `\x01` marker byte precedes both the data line and the body line, so
the graph prefix (everything before the marker) can be separated from the
fields. Fields are `|`-separated (9 total):

```
0: change_id.short(8) | 1: change_id.shortest() | 2: commit_id.short(8) |
3: commit_id.shortest() | 4: author.email() | 5: date | 6: working_copy (Y/N) |
7: immutable (Y/N) | 8: bookmarks (comma-separated) | 9: tags (comma-separated)
```

Lines without a marker byte are graph edge lines, attached to the preceding
commit during parsing (`parseLog`).

## Architecture

### Bubble Tea model (the Elm architecture)

- **Model** (`ui.Model`) holds all state: window size, log entries + cursor +
  offset, status, diff panel, help scroll, bookmark/git/remote modes,
  autocomplete, AI-loading set, spinner frame.
- **Update** handles `tea.Msg`s. UI-blocking work (running jj, HTTP) happens in
  `tea.Cmd`s that return result messages (`refreshMsg`, `diffLoadedMsg`,
  `actionDoneMsg`, `aiDoneMsg`, `listLoadedMsg`, …). Keyboard input is a
  `tea.KeyMsg` dispatched per mode.
- **View** composes the screen as a slice of pre-styled, width-clipped lines
  (top bar, content, optional autocomplete line, status bar, help bar) joined
  to exactly the terminal height.

### Rendering helpers (`render.go`)

OpenTUI's `<box>`/`<text>`/`StyledText` are replaced by `seg` (a styled run)
plus `renderSegs`/`plainRow`/`bgRow`. Each segment carries its own background
so a filled row stays continuous across ANSI resets. `clip` truncates to width
with `x/ansi` (preserving escape codes).

### Editor suspend

`d` (jj describe) uses `tea.ExecProcess` to suspend the TUI, run
`jj describe -r <rev>` with the terminal attached for `$EDITOR`, then resume.

### Views

| View | Key | Description |
|------|-----|-------------|
| Log  | default | Commit list, 2 lines + graph edges per commit. Variable-height scroll windowing in `logWindow`. |
| Diff | `enter` | Status summary + parsed/highlighted diff, scrolled via `diffScrollY`. |
| Help | `?` | Keybinding reference, scrolled via `helpScrollY`. |

### Layout (top to bottom)

1. Top bar — `◉ gojo` + repo path (2 lines, dark-purple bg)
2. Content area — log / diff / help (`height - 4 - autocomplete`)
3. Autocomplete suggestions — only in bookmark input mode (1 line, optional)
4. Status bar — mode menus, errors, messages, or file count (1 line)
5. Help bar — global keybinding hints (1 line)

## JJ Runner (internal/jj)

All jj operations go through `Runner.run(args...)`, which executes `jj` in the
repo dir via `os/exec`, capturing stdout and surfacing stderr on error.

Operations: Log, Status, Diff (`--git`), DiffSummary, FileShow, Describe, New,
Edit, Abandon, Absorb, Undo, Redo, Bookmark{Create,Delete,Forget,List,Move,Rename,Set,
Track,Untrack}, Tag{List,Set,Delete}, GitFetch, GitPush, GitPushTags,
Remote{Add,List,Remove,Rename,SetURL}, AIDescribe.

## Color Palette (styles.go — CharmTone)

| Name | Hex | Usage |
|------|-----|-------|
| purple | #6B50FF | Change IDs, highlights |
| darkPurple | #3A3350 | Selection / top-bar background |
| blue | #00A4FF | Author names |
| green | #00FFB2 | Bookmarks |
| red | #EB4268 | Errors |
| yellow | #F5EF34 | Working copy, cursor |
| magenta | #FF60FF | Change ID prefix, AI spinner |
| cyan | #10B1AE | Bookmark mode |
| gray | #858392 | Dates, commit IDs, help text |
| darkGray | #3A3943 | Graph edges, separators |
| darkerGray | #201F26 | Status bar background |
| white | #ECEBF0 | Subjects |
| orange / darkOrange | #FF985A / #BF976F | Git mode |
| pink | #FF7EB6 | Remote mode |
