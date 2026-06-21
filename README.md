# gojo

A fullscreen terminal UI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS), built in [Go](https://go.dev) with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

<p align="center">
  <img src="https://img.shields.io/badge/go-1.24+-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/bubbletea-charm-FF75B7?style=flat" alt="Bubble Tea">
  <img src="https://img.shields.io/badge/jj-v0.41+-orange?style=flat" alt="jj v0.41+">
</p>

> ⚠️ **This project was developed using AI assistance.** It's been reviewed by a developer now and should be generally safe to use.

<img width="2880" height="946" alt="image" src="https://github.com/user-attachments/assets/ffe4f80c-6ab5-4c8e-865a-48c5c00c3eca" />


## Features

- **Log view** — scrollable commit graph with change IDs, authors, dates, bookmarks, and working copy highlighting
- **Diff panel** — file status summary + syntax-highlighted diff for any commit
- **Bookmark management** — create, delete, move, rename, set, track, untrack, and list bookmarks
- **Git integration** — fetch, push, and remote management from within the TUI
- **AI commit messages** — generate a description from a commit's diff via OpenRouter
- **Undo / redo** — one-key `jj undo` / `jj redo`
- **Graph rendering** — native jj graph output with styled nodes (@/○/◆) and edges

## Installation

### Nix (recommended)

```sh
nix run github:0xhckr/gojo      # run directly
# or, for development:
nix develop                     # drops you into a shell with go + jujutsu
go run .
```

### From source

Requires Go 1.24+ and `jj` in `$PATH`.

```sh
go build -o gojo .
./gojo
```

## Requirements

- [Go](https://go.dev) 1.24+ (to build)
- [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS) v0.41+ in `$PATH`
- A jj repository (run `gojo` inside any `.jj` directory)

## Configuration

Gojo reads an optional TOML config file at `~/.config/gojo/gojo.toml`. Values
may also be placed under a `[tools.gojo]` section in `~/.config/jj/config.toml`
(the standalone gojo file takes precedence).

```toml
# OpenRouter API key for AI-generated commit messages (optional)
openrouter_api_key = "sk-or-..."

# Model to use
openrouter_model = "anthropic/claude-sonnet-4"

# Custom prompt template for AI commit messages (optional)
commit_prompt = "You are a software developer. Write a clear, concise commit message given the diff: "
```

## Keybindings

### Global

| Key | Action |
|-----|--------|
| `?` | Help |
| `q` | Quit / close panel |
| `ctrl+c` | Force quit |

### Log view

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Navigate commits |
| `G` | Jump to last commit |
| `Home` | Jump to first commit |
| `enter` | Open diff panel |
| `d` | `jj describe` (opens `$EDITOR`) |
| `D` | AI-generate commit message |
| `e` | `jj edit` (set working copy) |
| `n` | `jj new` (create change) |
| `a` | `jj abandon` (remove commit) |
| `b` | Bookmark mode |
| `g` | Git mode |
| `u` | `jj undo` |
| `r` | `jj redo` |

### Diff panel

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Scroll |
| `enter` / `q` | Close panel |

### Bookmark mode

Press `b` to enter, then:

| Key | Action |
|-----|--------|
| `c <name>` | Create bookmark |
| `d <name>` | Delete bookmark |
| `f <name>` | Forget bookmark |
| `l` | List bookmarks |
| `m <name>` | Move bookmark to selected commit |
| `r <old> <new>` | Rename bookmark |
| `s <name>` | Set bookmark to selected commit |
| `t <name>` | Track remote bookmark |
| `T <name>` | Untrack remote bookmark |
| `tab` | Autocomplete (cycle suggestions) |
| `esc` | Cancel |

### Git mode

Press `g` to enter, then:

| Key | Action |
|-----|--------|
| `f` | `jj git fetch` |
| `p` | `jj git push` |
| `r` | Remote mode |
| `esc` | Cancel |

### Remote mode

Press `r` in git mode, then:

| Key | Action |
|-----|--------|
| `a <name> <url>` | Add remote |
| `l` | List remotes |
| `r <name>` | Remove remote |
| `m <old> <new>` | Rename remote |
| `s <name> <url>` | Set remote URL |
| `esc` | Cancel |

## Project structure

```
main.go                 Entry point — starts the Bubble Tea program
internal/
  jj/
    jj.go               jj CLI wrapper + log/status parsers
    config.go           config + TOML loader
    ai.go               OpenRouter commit-message generation
  ui/
    model.go            Bubble Tea model: state, update, view, keybindings
    render.go           styled-line rendering helpers (Lip Gloss)
    styles.go           color palette and constants
    logview.go          commit list with graph
    diff.go             git-diff parser + chroma syntax highlighting
    diffpanel.go        diff viewer
    helpview.go         keybinding reference
go.mod                  Go module + dependencies
flake.nix              Nix dev shell + package
```

## Dependencies

Everything is pure Go — no native FFI, no Node runtime:

- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) — styling/layout
- [chroma](https://github.com/alecthomas/chroma) — diff syntax highlighting

The TOML config parser, unified-diff parser, and OpenRouter client are
implemented in-tree with the standard library.

## License

MIT
