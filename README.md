# gojo

A fullscreen terminal UI for [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS), built in [Go](https://go.dev) with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss). Commit graph, diffs, bookmarks, git remotes, and AI commit messages — without leaving your terminal.

**Website: [gojo.rocks](https://gojo.rocks)**

<p align="center">
  <img src="https://img.shields.io/badge/go-1.26+-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/bubbletea-charm-FF75B7?style=flat" alt="Bubble Tea">
  <img src="https://img.shields.io/badge/jj-v0.41+-orange?style=flat" alt="jj v0.41+">
  <img src="https://img.shields.io/badge/license-MIT-blue?style=flat" alt="MIT">
</p>

> ⚠️ **This project was developed using AI assistance.** It's been reviewed by a developer now and should be generally safe to use.

<img width="1800" height="940" alt="gojo log view: commit graph with change IDs, authors, dates, bookmarks, a conflicted merge marked ×, and a dirty working copy" src="https://gojo.rocks/screenshots/01-log.png" />


## Features

- **Log view** — scrollable commit graph with change IDs, authors, dates, bookmarks, and working copy highlighting
- **Diff panel** — file status summary + syntax-highlighted diff for any commit
- **Conflict resolution** — side-by-side 3-way merge view: pick left / right / both per hunk, apply with `jj resolve`
- **Bookmark management** — create, delete, move, rename, set, track, untrack, and list bookmarks
- **Git integration** — fetch, push, and remote management from within the TUI
- **AI commit messages** — generate a description from a commit's diff via any OpenAI-compatible API
- **Undo / redo** — one-key `jj undo` / `jj redo`
- **Graph rendering** — native jj graph output with styled nodes (@/○/◆) and edges

## Views

Captured live with [VHS](https://github.com/charmbracelet/vhs) — see the
[gallery on gojo.rocks](https://gojo.rocks#views).

| Diff panel | Conflict resolution |
|:---:|:---:|
| ![diff panel — file summary + chroma-highlighted diff with word-level changes](https://gojo.rocks/screenshots/03-diff.png) | ![conflict resolution — side-by-side 3-way merge, pick l / r / both per hunk and apply with ⏎](https://gojo.rocks/screenshots/02-conflict.png) |
| **Bookmark mode** | **Help** |
| ![bookmark mode — create, move, rename, track, with tab-completion](https://gojo.rocks/screenshots/05-bookmark.png) | ![help — the full keybinding reference, one ? away](https://gojo.rocks/screenshots/04-help.png) |

## Installation

### Install script (auto-detect)

```sh
curl -fsSL https://gojo.rocks/install.sh | sh
```

Detects your OS/distro and runs the right installer below: macOS
(Homebrew), Arch (pacman), Debian/Ubuntu (apt), Fedora/RHEL (dnf),
openSUSE (zypper), or a plain tarball into `~/.local/bin` on other Linux.
NixOS users get pointed at the [Nix/NixOS](#nixnixos) instructions.

### Homebrew

```sh
brew tap 0xhckr/gojo https://github.com/0xhckr/gojo
brew install --cask gojo
```

This installs `gojo` and pulls in `jj` (jujutsu) as a runtime dependency
automatically. The explicit URL is needed because the cask lives in the
source repo rather than a separate `homebrew-` repo. See the post-install
caveat for an optional `gj` shorthand alias.

### Nix/NixOS

```sh
nix run github:0xhckr/gojo      # run directly
# or, for development:
nix develop                     # drops you into a shell with go + jujutsu
go run .
```

### Debian / Ubuntu

Each GitHub release ships a `.deb` (`gojo_<version>_linux_<arch>.deb`):

```sh
apt install ./gojo_<version>_linux_amd64.deb
```

`jujutsu` is a recommended dependency (packaged on Debian 13+ and Ubuntu
24.10+); on older releases apt skips it and you install
[jj](https://jj-vcs.github.io/jj/latest/install-and-setup/) yourself.

### Fedora / RHEL

Each GitHub release ships an RPM (`gojo_<version>_linux_<arch>.rpm`):

```sh
dnf install ./gojo_<version>_linux_x86_64.rpm
```

`jujutsu` is a weak recommendation, so dnf pulls in jj automatically on
Fedora 39+ (older releases simply skip it).

### openSUSE (Tumbleweed / Leap)

The same RPM from the GitHub release works here:

```sh
zypper install ./gojo_<version>_linux_x86_64.rpm
```

zypper honors the weak `jujutsu` recommendation (packaged in Tumbleweed's
official repos; on Leap, install
[jj](https://jj-vcs.github.io/jj/latest/install-and-setup/) separately if it
isn't offered).

### Arch Linux (and derivatives)

Each GitHub release ships a pacman package
(`gojo_<version>_linux_<arch>.pkg.tar.zst`):

```sh
pacman -U gojo_<version>_linux_x86_64.pkg.tar.zst
```

The package depends on `jujutsu` (in [extra]), so pacman pulls jj in
automatically.

### From source

Requires Go 1.26+ and `jj` in `$PATH`.

```sh
go build -o gojo .
./gojo
```

## Requirements

- [Go](https://go.dev) 1.26+ (to build)
- [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS) v0.41+ in `$PATH`
- A jj repository (run `gojo` inside any `.jj` directory)

## Configuration

Gojo reads an optional TOML config file at `~/.config/gojo/gojo.toml`. Values
may also be placed under a `[tools.gojo]` section in `~/.config/jj/config.toml`
(the standalone gojo file takes precedence).

```toml
# API key for AI-generated commit messages (optional)
ai_api_key = "sk-or-..."

# Base URL of an OpenAI-compatible chat-completions endpoint
# (defaults to https://openrouter.ai/api/v1)
ai_base_url = "https://openrouter.ai/api/v1"

# Model to use
ai_model = "anthropic/claude-sonnet-4"

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
| `c` | Conflict resolution view (on conflicted commits) |
| `b` | Bookmark mode |
| `g` | Git mode |
| `u` | `jj undo` |
| `U` | `jj redo` |

### Diff panel

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Scroll |
| `enter` / `q` | Close panel |

### Conflict view

Press `c` on a conflicted commit (from the log view or diff panel), then:

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Navigate conflict hunks |
| `[` / `]` | Switch conflicted file |
| `l` / `r` / `b` | Take left / right / both sides for the hunk |
| `u` | Clear the hunk's pick |
| `enter` | Apply the resolution (`jj resolve`) |
| `q` / `esc` | Close view |

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
    ai.go               AI commit-message generation (OpenAI-compatible API)
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

The TOML config parser, unified-diff parser, and AI client are
implemented in-tree with the standard library.

## License

MIT
