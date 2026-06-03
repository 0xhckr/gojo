# gojo

A fullscreen terminal UI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS), built with [OpenTUI](https://github.com/nicetsai/opentui) and React, running on [Bun](https://bun.sh).

<p align="center">
  <img src="https://img.shields.io/badge/bun-1.3-000?style=flat&logo=bun" alt="Bun">
  <img src="https://img.shields.io/badge/typescript-5.9-3178C6?style=flat&logo=typescript" alt="TypeScript">
  <img src="https://img.shields.io/badge/jj-v0.41+-orange?style=flat" alt="jj v0.41+">
</p>

> ⚠️ **This project was developed using AI assistance.** It's been reviewed by a developer now and should be generally safe to use.

<img width="2880" height="946" alt="image" src="https://github.com/user-attachments/assets/ffe4f80c-6ab5-4c8e-865a-48c5c00c3eca" />


## Features

- **Log view** — scrollable commit graph with change IDs, authors, dates, bookmarks, and working copy highlighting
- **Diff panel** — file status summary + diff for any commit
- **Bookmark management** — create, delete, move, rename, set, track, untrack, and list bookmarks
- **Git integration** — fetch and push from within the TUI
- **Undo** — one-key `jj undo`
- **Graph rendering** — native jj graph output with styled nodes (@/○/◆) and edges

## Installation

### Nix dev shell (recommended)

```sh
nix develop
pnpm install
bun run src/main.tsx
```

### From source

```sh
pnpm install
bun run src/main.tsx
```

## Requirements

- [Bun](https://bun.sh) runtime (required for OpenTUI's native FFI)
- [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS) v0.41+ in `$PATH`
- A jj repository (run `gojo` inside any `.jj` directory)

## Configuration

Gojo reads an optional TOML config file at `~/.config/gojo/gojo.toml`:

```toml
# OpenRouter API key for AI-generated commit messages (optional)
openrouter_api_key = "sk-or-..."

# Model to use
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
src/
  main.tsx              Entry point — creates renderer, mounts React
  App.tsx               Main component — views, keyboard, state
  jj.ts                 jj CLI wrapper, parser, config loader
  styles.ts             Color palette and constants
  hooks.ts              Custom React hooks
  views/
    LogView.tsx         Commit list with graph
    DiffPanel.tsx       Diff viewer
    HelpView.tsx        Keybinding reference
package.json            Dependencies
tsconfig.json           TypeScript + JSX config
flake.nix               Nix dev shell
```

## License

MIT
