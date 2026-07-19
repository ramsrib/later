# Releasing later

Releases are cross-compiled static binaries published to
[GitHub Releases](https://github.com/ramsrib/later/releases) as `.tar.gz`
archives with a `SHA256SUMS` file, and installed via
[Homebrew](https://github.com/ramsrib/homebrew-tap).

```sh
make release VERSION=v0.1.0
```

That one command preflights, builds every platform, packages, checksums, tags,
and publishes. Everything below is detail.

## Prerequisites

- Go (see `go.mod` for the required version)
- The [GitHub CLI](https://cli.github.com) (`gh`), authenticated
- Push access to the repo

## What `make release` does

1. **Preflight.** Refuses to run unless `VERSION` looks like `v1.2.3`, is unused,
   and follows the previous tag; and unless the working tree is clean and pushed.
   A tag is the permanent record of what shipped — it must name code others can
   actually fetch. `FORCE_VERSION=1` deliberately skips a version.
2. **Build.** Cross-compiles `CGO_ENABLED=0` binaries for each target in
   `PLATFORMS`, with `-trimpath`. That flag is not cosmetic: without it the
   binary embeds the build machine's absolute paths, which then ship to every
   downloader. The script greps the artifacts for `/Users/` and fails if any
   survive.
3. **Package.** One `.tar.gz` per platform (binary + README + LICENSE), plus
   `SHA256SUMS`.
4. **Publish.** Tags, pushes the tag, and creates the GitHub release with
   `--generate-notes`.

`DRAFT=1 make release VERSION=v0.1.0` creates a draft release instead.

## After releasing — update Homebrew

The formula in [`ramsrib/homebrew-tap`](https://github.com/ramsrib/homebrew-tap)
pins a version and a sha256. Take both from the `SHA256SUMS` the release printed:

```sh
cat dist/SHA256SUMS
```

Update `Formula/later.rb` with the new `version` and the matching
`sha256` for each platform, then push the tap. Verify:

```sh
brew update && brew upgrade later && later --version
```

## Versioning

Semantic versioning, `v`-prefixed. The version is compiled in via
`-X main.version=`, so `later --version` reports exactly what shipped.
Releases are immutable: to fix a bad release, cut the next patch version.
