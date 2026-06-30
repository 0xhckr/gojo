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

[Unreleased]: https://github.com/0xhckr/gojo/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/0xhckr/gojo/releases/tag/v1.0.0
