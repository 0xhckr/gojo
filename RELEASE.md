# Releasing gojo

gojo uses [goreleaser](https://goreleaser.com) (v2) to cross-build binaries,
publish a GitHub Release, and auto-update the Homebrew formula.

## One-time setup

You only do this once.

### 1. Create the tap repo

Create an **empty** GitHub repo named `homebrew-gojo` under `0xhckr`
(Homebrew requires tap repos to be prefixed with `homebrew-`).

```
https://github.com/new
  Owner:  0xhckr
  Name:   homebrew-gojo
```

No license / README / .gitignore — leave it empty.

### 2. Create a PAT for the tap

goreleaser needs push access to `0xhckr/homebrew-gojo`. The default
`GITHUB_TOKEN` is scoped to `0xhckr/gojo` only, so create a PAT:

- **Fine-grained** (recommended): scoped to `0xhckr/homebrew-gojo` only,
  with **Contents: Read and write**.
- **Classic**: a `public_repo` (or `repo` for private) scoped token.

Save it as a secret in `0xhckr/gojo`:

```
https://github.com/0xhckr/gojo/settings/secrets/actions
  Name:  HOMEBREW_TAP_GITHUB_TOKEN
```

### 3. Tag the first release

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
4. Generates `Formula/gojo.rb` and pushes it to `0xhckr/homebrew-gojo`.

## Installing (end users)

```sh
brew tap 0xhckr/gojo
brew install gojo
```

This also installs `jj` (jujutsu) as a runtime dependency and a `gj` symlink.
