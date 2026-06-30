# Contributing to gojo

Thanks for considering a contribution! gojo is a small project, so this is
kept short. The heavier architectural reference is
[`AGENTS.md`](./AGENTS.md) — read that if you're touching the TUI internals.

## Prerequisites

- **Go 1.26+** (`go.mod` pins `1.26.2`)
- **[jj](https://github.com/jj-vcs/jj)** (Jujutsu VCS) v0.41+ on `$PATH`
- gojo is itself a jj repo (colocated with git), so you'll want jj for
  local development.

If you use Nix, everything's provided:

```sh
nix develop          # go, gopls, go-tools, jujutsu
# or, with direnv: just cd in — .envrc runs `use flake`
```

## Building & running

```sh
go run .             # run from source (version reports "dev")
go build -o gojo .   # produce a binary
```

## Testing

```sh
go test ./...        # all tests
go test ./internal/ui/...   # one package
go test -run TestSmoke ./internal/jj   # a single test
```

`internal/jj/TestSmoke` and a couple of others exercise the real jj CLI
against the repo itself; they **skip automatically** when not run inside a
jj repo, so the suite passes in plain CI environments too.

Please add or update tests for behavior you change. The UI is tested
headlessly by driving the Bubble Tea `Model` through `Update` and asserting
on `View()` — no PTY required (see `internal/ui/model_test.go` for the
pattern).

## Before opening a PR

```sh
go build ./...
go vet ./...
go test ./...
```

CI (`.github/workflows/ci.yml`) runs exactly these on every push and PR, so
a green local run means a green CI run.

## Commit messages

This repo uses plain imperative, sentence-case messages with no
conventional-commit prefixes — match the existing log:

```
Add syntax highlighting support to file view using chroma
Fix index out of bounds panic in fileview ensureSections
Refactor blame line rendering to align separator column across row types
```

- Capitalized first word, no trailing period.
- Imperative mood ("Add", "Fix", "Refactor").
- If the change needs context, leave a blank line and a body paragraph.

## Releasing

Maintainers only — see [`RELEASE.md`](./RELEASE.md). The short version: bump
`VERSION`, tag `v<VERSION>`, push the tag; goreleaser handles binaries,
the GitHub Release, and the Homebrew formula.

## License

By contributing you agree your changes are licensed under the project's
[MIT license](./LICENSE).
