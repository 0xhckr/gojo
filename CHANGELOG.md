# Changelog

This is a **human-curated** summary of notable changes per release — the
"what matters to users" view. For the full commit-level detail of any
release, see the auto-generated notes on the [GitHub Releases][releases]
page (produced by goreleaser on each tag).

The format is based on [Keep a Changelog][keepachangelog], and this project
adheres to [Semantic Versioning][semver].

[releases]: https://github.com/0xhckr/gojo/releases
[keepachangelog]: https://keepachangelog.com/en/1.1.0/
[semver]: https://semver.org/spec/v2.0.0.html

## [Unreleased]

## [1.9.0] - 2026-09-12

### Added
- Sticky file headers in the diff viewer keep the current filename visible
  while scrolling through long files.

## [1.8.0] - 2026-08-31

### Added
- CLI flags to print the version with `gojo --version` or `gojo -v`, and the
  full changelog with `gojo --changelog`.
- Windows amd64 and arm64 packages installable through Scoop, including the
  required `jj` runtime dependency.

## [1.7.0] - 2026-08-30

### Added
- Workspace management from the log with `w`: list and switch workspaces, add
  workspace directories, rename, forget safely without deleting files, and
  update stale workspaces.
- Workspace ownership labels (`name@`) on working-copy commits in the log and
  revision search.

### Changed
- Typing in the file browser now starts fuzzy search; arrow keys remain
  available for tree navigation without conflicting single-letter shortcuts.
- Diff content appears immediately and gains syntax highlighting
  asynchronously, with faster highlighting for large diffs.
- Updated the terminal runtime for improved input, mouse, focus, and rendering
  compatibility.

### Removed
- x86_64 Darwin support from the Nix flake; supported Darwin builds are arm64.

## [1.4.0] - 2026-08-09

### Added
- Arch Linux pacman packages (`.pkg.tar.zst`) attached to each GitHub
  release, depending on the distro `jujutsu` package.
- Debian/Ubuntu packages (`.deb`) attached to each GitHub release, with
  `jujutsu` as a recommended dependency.
- Fedora/RHEL packages (`.rpm`) attached to each GitHub release, with
  `jujutsu` as a weak recommendation (also usable on openSUSE).
- openSUSE installation support via the same release `.rpm` (zypper).
- `nix run .#bump -- [major|minor|patch|X.Y.Z]` helper: writes VERSION and
  refreshes the flake vendorHash.

## [1.0.0] - 2026-06-29

First tagged release.

### Added
- Fullscreen terminal UI for jj: scrollable commit graph with change IDs,
  authors, dates, bookmarks, and working-copy highlighting.
- Diff panel with file status summary and chroma syntax highlighting.
- File browser with blame annotation and history navigation.
- Bookmark management (create, delete, move, rename, set, track, untrack).
- Git integration: fetch, push, and remote management from within the TUI.
- AI-generated commit descriptions via OpenRouter.
- Undo / redo, squash mode, and context-aware help bar.
- Native jj graph rendering with styled nodes and edges.
- TOML config at `~/.config/gojo/gojo.toml` (or `[tools.gojo]` in jj config).
- Nix flake (devShell + package), Homebrew formula, and release automation.

[Unreleased]: https://github.com/0xhckr/gojo/compare/v1.9.0...HEAD
[1.9.0]: https://github.com/0xhckr/gojo/releases/tag/v1.9.0
[1.8.0]: https://github.com/0xhckr/gojo/releases/tag/v1.8.0
[1.7.0]: https://github.com/0xhckr/gojo/releases/tag/v1.7.0
[1.4.0]: https://github.com/0xhckr/gojo/releases/tag/v1.4.0
[1.0.0]: https://github.com/0xhckr/gojo/releases/tag/v1.0.0
