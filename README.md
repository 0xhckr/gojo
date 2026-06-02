# gojo

A fullscreen terminal UI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS), written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lipgloss](https://github.com/charmbracelet/lipgloss), and [Bubbles](https://github.com/charmbracelet/bubbles).

<p align="center">
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?style=flat&logo=go" alt="Go 1.26">
  <img src="https://img.shields.io/badge/jj-v0.41+-orange?style=flat" alt="jj v0.41+">
</p>

> ⚠️ **This project was developed using AI assistance.** It's been reviewed by a developer now and should be generally safe to use.

<img width="1935" height="1231" alt="image" src="https://github.com/user-attachments/assets/659f1efb-373b-4f0a-aa74-555dfc7affc9" />
<img width="1935" height="1231" alt="image" src="https://github.com/user-attachments/assets/7631d356-fffa-45d7-9c8e-2f282d000fa1" />
<img width="1935" height="1231" alt="image" src="https://github.com/user-attachments/assets/930ffc83-a286-41ae-9b07-3a50516e8143" />


## Features

- **Log view** — scrollable commit graph with change IDs, authors, dates, bookmarks, and working copy highlighting
- **Diff panel** — file status summary + syntax-highlighted diff for any commit
- **Bookmark management** — create, delete, move, rename, set, track, untrack, and list bookmarks
- **Git integration** — fetch and push from within the TUI
- **AI commit messages** — generate descriptions via OpenRouter (any model)
- **Undo** — one-key `jj undo`
- **Graph rendering** — native jj graph output with styled nodes (@/○/◆) and edges

## Installation

### Nix (recommended)

```sh
nix build github:0xhckr/gojo
./result/bin/gojo
```

### From source

```sh
go install github.com/0xhckr/gojo/cmd/gojo@latest
```

### Nix dev shell

```sh
nix develop
go build ./cmd/gojo
```

## Requirements

- [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS) v0.41+ in `$PATH`
- A jj repository (run `gojo` inside any `.jj` directory)

## Configuration

Gojo reads an optional TOML config file at `~/.config/gojo/gojo.toml`:

```toml
# OpenRouter API key for AI-generated commit messages (optional)
openrouter_api_key = "sk-or-..."

# Model to use (default: google/gemini-2.0-flash-001)
openrouter_model = "anthropic/claude-sonnet-4-20250514"

# Custom prompt template for AI commit messages (optional)
commit_prompt = "You are a software developer. Write a clear, concise commit message given the diff: "
```

## Keybindings

### Global

| Key | Action |
|-----|--------|
| `?` | Help |
| `r` | Refresh |
| `q` | Quit / close panel |

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

### Diff panel

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Scroll |
| `pgup`/`b`, `pgdn`/`f` | Half-page scroll |
| `g` / `G` | Jump to top / bottom |
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
| `esc` | Cancel |

### Git mode

Press `g` to enter, then:

| Key | Action |
|-----|--------|
| `f` | `jj git fetch` |
| `p` | `jj git push` |
| `esc` | Cancel |

## Project structure

```
cmd/gojo/main.go         Entry point
internal/
  ai/ai.go               OpenRouter client for AI commit messages
  config/config.go       Config loading, jj binary & repo discovery
  jj/runner.go           jj CLI wrapper (log, diff, status, bookmarks, …)
  ui/
    model.go             Bubble Tea model, views, key handling
    styles.go            Lipgloss color palette and styles
flake.nix                Nix flake (dev shell + package build)
```

## License

MIT
