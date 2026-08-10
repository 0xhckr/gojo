# AGENTS.md — gojo

## What

gojo is a fullscreen TUI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS),
written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
(Elm-architecture TUI framework) and [Lip Gloss](https://github.com/charmbracelet/lipgloss)
(styling). Diff syntax highlighting uses [chroma](https://github.com/alecthomas/chroma).

## Project Structure

```
main.go                 — entry point: tea.NewProgram(ui.NewModel(), WithAltScreen,
                          WithReportFocus, WithMouse{Cell,All}Motion,
                          WithANSICompressor)
internal/
  jj/
    jj.go               — Runner: runs jj CLI commands, parses log/status output;
                          conflict ops: Conflicts (resolve --list),
                          FetchConflictSides (throwaway probe merge tool steals
                          $left/$base/$right, leaves $output empty so jj aborts
                          — the conflict stays intact), Resolve (tool copies the
                          gojo-composed content into $output)
    config.go           — Config struct, repo-root discovery (ErrNoRepo sentinel
                          when no .jj dir; Config still carries JJPath so the
                          boot prompt can init), minimal TOML loader (incl. the
                          [keymap] / [tools.gojo.keymap] section → Config.Keymap,
                          and the top-level `theme` key → Config.Theme),
                          ConfigDir/ConfigPath/ThemesDir helpers, SaveTheme
                          (upserts `theme = "<id>"` into gojo.toml for the picker)
    ai.go               — AIDescribe: OpenAI-compatible chat-completions client (net/http)
  ui/
    model.go            — Bubble Tea Model: state, Update (msgs + keys), View, commands
    keys.go             — configurable keybindings: key contexts/actions, default
                          table, KeyMap (defaults + [keymap] overrides), resolve/
                          hk/hkN hint helpers, keyMsg synthesis for menu clicks
    render.go           — seg/renderSegs/clip/bgRow + the lipgloss style cache
                          (probe-once per style combo; bounded RWMutex map —
                          themes introduce new color combos at runtime),
                          blankRow cache, ASCII fast path for segTextWidth,
                          expandTabs (raw control chars never hit the terminal:
                          tabs render as tabStop=4 spaces everywhere)
    styles.go           — color palette vars (defaults = the compiled-in "gojo"
                          theme; rewritten at boot by applyTheme), spinner frames
    themes.go           — Theme + paletteDef (every color slot), resolve()
                          derivation recipe (tints mix toward bg, dark_* dim
                          accents, accent fallback chains), applyTheme
    themes_builtin.go   — compiled-in "gojo"/"terminal" themes, theme file
                          discovery (exe-relative share/gojo/themes →
                          /usr/{local,}/share → ./themes for dev →
                          ~/.config/gojo/themes; user dir shadows by id),
                          minimal TOML theme parser
    themepicker.go      — theme picker view (key T): live preview on move,
                          ⏎ applies + saves via jj.SaveTheme, esc restores
    logview.go          — commit list rendering + variable-height scroll windowing
    diff.go             — git unified-diff parser + chroma highlighting → diffRow
                          (lexer + token-fg caches; LCS word diff with prefix/
                          suffix trim, flat matrix, cell-count budget)
    diffpanel.go        — diff viewer rendering (gutter, status, file/hunk/line rows)
    merge3.go           — 3-way merge on line slices: LCS line diff (prefix/suffix
                          trim + cell budget) → context/auto/conflict blocks;
                          composeResolved emits the file from per-block choices
    conflictview.go     — side-by-side conflict resolution view (key `c`):
                          left pane = side 1, right = side 2, per-hunk pick
                          l/r/b/u (or click the pane), [ ] file tabs, ⏎ applies
                          via jj resolve; rows cached per file, no wrapping
    split.go            — split mode + intermediate-file computation + jj split tool
    helpview.go         — keybinding reference + scroll
go.mod / go.sum         — module `gojo`, deps: bubbletea, lipgloss, chroma, x/ansi
themes/*.toml           — built-in color themes, one file per theme (~50:
                          [dark]/[light] palette sections + name/syntax keys).
                          Installed to <prefix>/share/gojo/themes by the flake
                          and by release packages; users override/add in
                          ~/.config/gojo/themes (file-stem id shadows builtin).
flake.nix               — nix flake: devShell (go, gopls, jujutsu),
                          buildGoModule package, `nix run .#bump` —
                          version bump app (major|minor|patch|X.Y.Z) wrapping
                          scripts/bump.go from the repo root
scripts/bump.go         — version bumper: writes VERSION, refreshes the
                          flake vendorHash via `nix build .#default`
.goreleaser.yaml        — release pipeline: tarballs, distro packages (nfpm:
                          archlinux pacman pkg + deb + rpm; one entry per
                          format so jujutsu is a hard dep on Arch, recommended
                          on deb, weak recommend on rpm), Homebrew cask
                          commit-back to Casks/. Validates VERSION == tag in a
                          before hook.
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
fields. Fields are `|`-separated (11 total):

```
0: change_id.short(8) | 1: change_id.shortest() | 2: commit_id.short(8) |
3: commit_id.shortest() | 4: author.email() | 5: date | 6: working_copy (Y/N) |
7: immutable (Y/N) | 8: bookmarks (comma-separated) | 9: tags (comma-separated) |
10: conflict (Y/N)
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
  `tea.KeyMsg` dispatched per mode. Mouse wheel events are coalesced: the
  first step of a burst applies immediately, further steps accumulate and
  flush as one batch per 16 ms `wheelTickMsg` — macOS trackpads emit wheel
  events far faster than the terminal can repaint during momentum scrolling,
  and per-event handling made the message queue back up (the app kept
  repainting stale scroll states after the fingers lifted).
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
| Diff | `enter` | Status summary + parsed/highlighted diff. Cursor moves (j/k, g/G) center the cursor in the viewport (clamped at the page ends); wheel/pgup·pgdn scroll only via `diffScrollY` without touching the chunk cursor. |
| Conflicts | `c` (log/diff) | Side-by-side 3-way conflict resolution; per-hunk l/r/b/u or pane click, per-file ⏎ apply via `jj resolve` with a probe/apply merge tool. |
| Themes | `T` (log) | Theme picker over all installed + user themes; live preview on move, ⏎ applies and upserts `theme = "<id>"` into ~/.config/gojo/gojo.toml, esc restores the previous theme. |
| Help | `?` | Keybinding reference, scrolled via `helpScrollY`. |

### Layout (top to bottom)

1. Top bar — `◉ gojo` + repo path (2 lines, dark-purple bg)
2. Content area — log / diff / help (`height - 4 - autocomplete`)
3. Autocomplete suggestions — only in bookmark input mode (1 line, optional)
4. Status bar — mode menus, errors, messages, or file count (1 line)
5. Help bar — global keybinding hints (1 line)

## Configurable keybindings

All keys route through `KeyMap` (keys.go): every handler resolves
`tea.KeyMsg.String()` through a *context* (`global`, `log`, `diff`, `split`,
`conflict`, `input`, `menu`, `bookmark`, `tag`, `git`, `remote`, …) to a named
*action*, then switches on the action. The default table in `keys.go` declares
each context's actions with their default keys.

Users override in `~/.config/gojo/gojo.toml` (or `[tools.gojo.keymap]` in jj's
config.toml):

```toml
[keymap]
log.down = "j,down"     # comma-separated alternates; replaces the whole binding
global.quit = "Q"
diff.absorb = ""        # empty = unbind
```

Key names follow `tea.KeyMsg.String()` ("enter", "esc", "tab", "up", "pgup",
"ctrl+u", single runes…); `normalizeKeyName` accepts friendly spellings
("space", "escape", "pgdn"…). When two actions in a context are bound to the
same key, the one declared first in the default table wins. Overrides apply at
boot (bootMsg → `newKeyMap(cfg.Keymap)`); the boot init/error screens read the
user keymap too since config loads before repo discovery.

Every UI hint (help bar, status-bar menus, view title bars, help view,
context menus) renders from the keymap via `m.hk` / `m.hkN` / `m.hkRaw` /
`m.hkLast` helpers, so rebinding is reflected everywhere. Synthetic dispatch
(context-menu clicks, shortcut-bar clicks → `keyMsgFromName`) also resolves
against the active bindings.

## JJ Runner (internal/jj)

All jj operations go through `Runner.run(args...)`, which executes `jj` in the
repo dir via `os/exec`, capturing stdout and surfacing stderr on error.

Operations: Log, Status, Diff (`--git`), DiffSummary, FileShow, Describe, New,
Edit, Abandon, Absorb, Undo, Redo, Bookmark{Create,Delete,Forget,List,Move,Rename,Set,
Track,Untrack}, Tag{List,Set,Delete}, GitFetch, GitPush, GitPushTags,
Remote{Add,List,Remove,Rename,SetURL}, AIDescribe. `GitInitDir` (not a Runner
method — no repo exists yet) wraps `jj git init [--colocate]` for the boot
prompt: outside any repo gojo asks "initialize here?" then "colocate with
git?", inits, and re-runs the boot.

## Themes

Colors are themeable. `styles.go` initializes the palette vars to the
"gojo" default; at boot `applyTheme` rewrites them from the selected theme.
The picker (`T` in the log view) lists all themes, live-previews the one
under the cursor, and persists the choice with `⏎` (`jj.SaveTheme` upserts
`theme = "<id>"` into ~/.config/gojo/gojo.toml); `esc` restores the previous
theme. Picking `terminal` makes gojo follow the terminal's own ANSI scheme.

Theme sources (later shadows earlier by id):
1. compiled-in: `gojo` (default), `terminal`
2. `<exe>/../share/gojo/themes` (nix store / /usr installs)
3. `/usr/local/share/gojo/themes`, `/usr/share/gojo/themes`
4. `./themes` (repo root, for `go run .` dev)
5. `~/.config/gojo/themes` (user custom; tagged "user" in the picker)

File format (one theme per file, id = file name stem, `name` = display
title, optional `syntax`/`syntax_light` set the chroma diff-highlighting
style). A theme lists `[dark]` and/or `[light]` sections with core slots
(background, element, text, text_muted, purple, blue, green, red, yellow,
cyan, orange, pink, teal, …); every omitted slot is derived deterministically
by `resolve()` (tints = accent mixed onto background; `dark_*` = dimmed
accent; magenta → purple, pink → magenta, teal → cyan chains):

```toml
name = "Dracula"
syntax = "dracula"          # optional

[dark]
background = "#282a36"
element    = "#44475a"
text       = "#f8f8f2"
text_muted = "#6272a4"
purple     = "#bd93f9"
blue       = "#8be9fd"
green      = "#50fa7b"
red        = "#ff5555"
yellow     = "#f1fa8c"
cyan       = "#8be9fd"
orange     = "#ffb86c"
pink       = "#ff79c6"
```

Values are `"#rrggbb"` hex or ANSI indices ("1"…"15" — the terminal
theme uses these to follow the terminal scheme). Empty/nil slots mean
"terminal default" once derivation is exhausted (mixing ANSI values yields
nil). See `themes/` for the ~50 shipped themes.

## Color Palette (styles.go — "gojo" theme, dark variant)

| Name | Hex (dark) | Usage |
|------|------------|-------|
| purple | #9d7cd8 | Change IDs, highlights |
| element | #1c1c26 | Selection / top-bar background |
| blue | #5c9cf5 | Author names |
| green | #7fd88f | Bookmarks, additions |
| red | #e06c75 | Errors, deletions |
| yellow | #f5a742 | Working copy, cursor |
| magenta | #c487f0 | Change ID prefix, AI spinner |
| cyan | #56b6c2 | Bookmark mode, hunk headers |
| textMuted ("gray") | #787886 | Dates, commit IDs, help text |
| border ("darkGray") | #343440 | Separators |
| panel ("darkerGray") | #14141c | Status bar background |
| text ("white") | #e2e2ec | Subjects |
| orange / darkOrange | #f5a742 / #b08030 | Git mode |
| pink / darkPink | #ff7eb6 / #b85a90 | Remote mode |
| teal / darkTeal | #4db6ac / #2e7d72 | Tag mode |
