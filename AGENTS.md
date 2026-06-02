# AGENTS.md — gojo

## What

gojo is a fullscreen TUI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS), built with [OpenTUI](https://github.com/nicetsai/opentui) and React, running on [Bun](https://bun.sh).

## Project Structure

```
src/
  main.tsx            — entry point: creates OpenTUI renderer, mounts React root
  App.tsx             — main React component: view routing, keyboard handling, state management
  jj.ts               — JJRunner class: runs jj CLI commands, parses output, loads config
  styles.ts           — color palette, spinner frames, status symbols
  hooks.ts            — custom React hooks (useAsync, useSpinner)
  views/
    LogView.tsx       — commit list view with graph rendering, cursor navigation
    DiffPanel.tsx     — diff panel: status summary + scrollable diff content
    HelpView.tsx      — keybinding reference view
package.json          — dependencies: @opentui/core, @opentui/react, react
tsconfig.json         — TypeScript config with JSX -> @opentui/react
flake.nix             — nix flake: dev shell with bun, pnpm, nodejs, jujutsu
.envrc                — direnv: `use flake`
```

## Nix / Dev Environment

- **Bun** (runtime — required for OpenTUI's native FFI)
- **Node.js 24**, **pnpm** (package management)
- **TypeScript** (type checking)
- **jj v0.41** (jujutsu VCS)
- `nix develop` drops you into the shell; direnv auto-loads it

### Critical: the jj package name

nixpkgs has two packages named `jj`:

- `nixpkgs#jj` = [tidwall/jj](https://github.com/tidwall/jj) — a JSON stream editor (NOT what we want)
- `nixpkgs#jujutsu` = [jj-vcs/jj](https://github.com/jj-vcs/jj) — the VCS (what we want)

The flake uses `jujutsu` in `packages`. Always use the full path `/home/hackr/.nix-profile/bin/jj` or ensure the correct `jj` is first in PATH.

## Running

```bash
pnpm install    # install deps
bun run src/main.tsx   # or: pnpm dev
```

OpenTUI requires Bun's FFI for its native Zig core. Node.js won't work.

## JJ Template Syntax

The `jj log` command uses jj's own template language. Key syntax:

- String concatenation: `++`
- String literals: `"text"`
- Newlines in output: `"\n"`
- Field access: `change_id.short(8)`, `commit_id.short(8)`, `author.email()`
- Conditionals: `if(condition, "yes", "no")`
- Joins: `bookmarks.join(",")`
- Date formatting: `author.timestamp().local().format("%Y-%m-%d %H:%M")`

### Current log template

```
change_id.short(8) ++ "|" ++ change_id.shortest() ++ "|" ++ commit_id.short(8) ++ "|" ++ commit_id.shortest() ++ "|" ++ author.email() ++ "|" ++
author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++
if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "\n" ++ "\x01" ++ description.first_line() ++ "\n"
```

Fields (9 total, split on `|`):
0: change_id | 1: change_id.shortest() | 2: commit_id | 3: commit_id.shortest() | 4: author | 5: date | 6: working_copy (Y/N) | 7: immutable (Y/N) | 8: bookmarks (comma-separated)

Graph prefixes are separated by `\x01` marker bytes. Edge lines between commits have no marker.

## Architecture

### OpenTUI React Components Used

| Component | Usage |
|-----------|-------|
| `<box>` | Layout containers, status/help bars |
| `<text>` | All text rendering (commits, status, help) |
| `<scrollbox>` | Log view list, diff panel scrolling |

### Hooks Used

| Hook | Usage |
|------|-------|
| `useKeyboard` | All keyboard input handling |
| `useTerminalDimensions` | Width/height for layout |
| `useRenderer` | Access to the OpenTUI renderer |

### Views

| View | Key | Description |
|------|-----|-------------|
| Log | default | Commit list, one line per commit. Cursor navigation with ↑/k ↓/j. |
| Diff | `enter` | Scrollable diff for selected commit using `<scrollbox>`. |
| Help | `?` | Keybinding reference. |

### Layout (top to bottom)

1. Content area — fills all available space (flexGrow=1)
2. Status bar — error messages, status text, or current view name (1 line, dark bg)
3. Help bar — keybinding hints (1 line, dark gray bg)

Content height = `terminal height - 2` (status bar + help bar).

### Log view specifics

- Top padding: one blank line
- Each commit = 2 lines + optional edge lines from jj's graph
- Graph styling uses OpenTUI's `StyledText` API (`fg()`, `bold()`, `dim()` functions)
- Cursor highlighting via `backgroundColor` on `<box>` wrapper

## Keybindings

### Global
- `?` — help
- `r` — refresh current view
- `q` / `ctrl+c` — quit (or go back from diff/help)

### Log View
- `↑`/`k`, `↓`/`j` — navigate commits
- `Home` — jump to first
- `G` (shift+g) — jump to last
- `enter` — show diff for selected commit
- `d` — `jj describe` (opens $EDITOR)
- `D` (shift+d) — AI generate commit message (placeholder)
- `e` — `jj edit` (set working copy to selected commit)
- `n` — `jj new` (create new change)
- `a` — `jj abandon` (remove commit)
- `b` — bookmark mode
- `g` — git mode
- `u` — `jj undo`

### Diff Panel
- `↑`/`k`, `↓`/`j` — scroll
- `enter`/`q` — close

### Bookmark Mode
- `c` — create, `d` — delete, `f` — forget, `l` — list, `m` — move, `r` — rename, `s` — set, `t` — track, `T` — untrack
- `esc` — cancel

### Git Mode
- `f` — fetch, `p` — push
- `esc` — cancel

## JJ Runner (src/jj.ts)

All jj operations go through `JJRunner.run(args)` which executes `jj` with the given args in the repo directory using `child_process.execFile`. Output is captured via stdout. Errors include the full jj stderr.

Available operations: log, status, diff, diffSummary, fileShow, describe, new, edit, abandon, undo, bookmarkCreate, bookmarkDelete, bookmarkForget, bookmarkList, bookmarkMove, bookmarkRename, bookmarkSet, bookmarkTrack, bookmarkUntrack, gitFetch, gitPush.

## Color Palette

Defined in `src/styles.ts`:

| Name | Hex | Usage |
|------|-----|-------|
| purple | #af87ff | Change IDs |
| darkPurple | #875faf | Selection background |
| blue | #5f87af | Author names |
| green | #5faf87 | Bookmarks |
| red | #d75f5f | Errors |
| yellow | #d7af5f | Working copy, cursor |
| cyan | #5fafaf | Bookmark mode |
| gray | #8a8a8a | Dates, commit IDs, help text |
| darkGray | #444444 | Graph edges |
| darkerGray | #262626 | Status bar background |
| white | #d0d0d0 | Subjects |
| orange | #ffaf5f | Git mode |
| darkOrange | #d75f00 | Git mode hint |
