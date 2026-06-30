# Releasing gojo

gojo uses [goreleaser](https://goreleaser.com) (v2) to cross-build binaries,
publish a GitHub Release, and auto-update the Homebrew formula.

The formula lives in **this same repo** at `Formula/gojo.rb` — there is no
separate tap repo and no PAT to manage. The default `GITHUB_TOKEN` (granted
`contents: write` by the release workflow) is enough to commit the formula
back into `0xhckr/gojo`.

## Prerequisites

- The repo `0xhckr/gojo` must be **public** (Homebrew can't install from
  private repos without per-user credentials).
- Actions must be enabled (they are by default).

That's it — no setup steps.

## Releasing

The **VERSION** file at the repo root is the single source of truth for the
version number. To release:

```sh
# 1. bump VERSION (it must match the tag, minus the leading `v`)
echo "1.0.0" > VERSION

# 2. commit and tag
git add VERSION
git commit -m "chore: release v1.0.0"
git tag v1.0.0
git push origin v1.0.0
```

The goreleaser `before` hook errors if VERSION != the tag, so they can't
drift silently. The `release` workflow then:

1. Builds `gojo` for `linux/{amd64,arm64}` and `darwin/{amd64,arm64}`.
2. Stamps `main.version` via ldflags (`gojo --version` → `gojo 1.0.0`).
3. Publishes tarballs + checksums to the GitHub Release.
4. Generates `Formula/gojo.rb` and commits it back to `main` in this repo.

## Installing (end users)

```sh
brew tap 0xhckr/gojo https://github.com/0xhckr/gojo
brew install gojo
```

The explicit URL is required because Homebrew's `brew tap <user>/<repo>`
shorthand only auto-discovers repos named `homebrew-<repo>`. Since the
formula lives here instead of in a `homebrew-gojo` repo, the URL must be
given. (This is the only downside of the single-repo setup; it trades one
extra line at install time for not maintaining a second repo + PAT.)

This also installs `jj` (jujutsu) as a runtime dependency and a `gj` symlink.
