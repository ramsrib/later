#!/usr/bin/env bash
#
# release.sh — cross-compile, package, and publish a release to GitHub.
#
#   make release VERSION=v0.1.0
#
# Steps:
#   1. Preflight: version is well-formed, unused, and follows the last tag;
#      the working tree is clean and pushed (a tag must name code others can get).
#   2. Cross-compile a static, reproducible binary per platform. -trimpath is
#      not cosmetic: without it the binary embeds the build machine's absolute
#      paths (/Users/<name>/...), which then ship to every downloader.
#   3. Package each as a .tar.gz, plus a SHA256SUMS file (Homebrew reads these).
#   4. Tag and publish the GitHub release with every artifact attached.
#
# Env knobs: VERSION (required, e.g. v0.1.0) · DRAFT=1 · FORCE_VERSION=1
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BINARY="${BINARY:?set BINARY, e.g. BINARY=recall}"
VERSION="${VERSION:?set VERSION, e.g. VERSION=v0.1.0}"
# Space-separated os/arch pairs. macOS-only tools override this.
PLATFORMS="${PLATFORMS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64}"
OUT="$ROOT/dist"

command -v gh >/dev/null || { echo "error: gh CLI required" >&2; exit 1; }

# 1. preflight ---------------------------------------------------------------
# VERSION is typed by hand, and a tag is the permanent record of what shipped.
# Check the two mistakes that record cannot recover from: reusing a version, and
# skipping one (a gap in the tags is indistinguishable from a release whose
# artifacts went missing).
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
  || { echo "error: VERSION must look like v1.2.3 (got '$VERSION')" >&2; exit 1; }

git fetch --tags --quiet origin 2>/dev/null || true
if git rev-parse "$VERSION" >/dev/null 2>&1 || gh release view "$VERSION" >/dev/null 2>&1; then
  echo "error: $VERSION already exists — releases are immutable; cut the next version" >&2
  exit 1
fi

LAST_TAG="$(git tag -l 'v*' --sort=-v:refname | head -1)"
if [[ -n "$LAST_TAG" ]]; then
  IFS=. read -r lm ln lp <<< "${LAST_TAG#v}"
  EXPECTED=("v$lm.$ln.$((lp + 1))" "v$lm.$((ln + 1)).0" "v$((lm + 1)).0.0")
  if [[ ! " ${EXPECTED[*]} " =~ " $VERSION " && -z "${FORCE_VERSION:-}" ]]; then
    echo "error: $VERSION does not follow $LAST_TAG — expected one of: ${EXPECTED[*]}" >&2
    echo "       (FORCE_VERSION=1 to skip a version deliberately)" >&2
    exit 1
  fi
fi

[[ -z "$(git status --porcelain)" ]] \
  || { echo "error: working tree is dirty — commit or stash before releasing" >&2; exit 1; }
git fetch --quiet origin main 2>/dev/null || true
if [[ -n "$(git log --oneline origin/main..HEAD 2>/dev/null)" ]]; then
  echo "error: HEAD is ahead of origin/main — push first, or the tag points at" >&2
  echo "       code nobody else has" >&2
  exit 1
fi
echo "==> releasing $BINARY $VERSION (previous: ${LAST_TAG:-none})"

# 2. build -------------------------------------------------------------------
rm -rf "$OUT"; mkdir -p "$OUT"
LDFLAGS="-s -w -X main.version=${VERSION#v}"

for platform in $PLATFORMS; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  STAGE="$OUT/${BINARY}_${GOOS}_${GOARCH}"
  mkdir -p "$STAGE"

  echo "==> building $GOOS/$GOARCH"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$STAGE/$BINARY" .

  cp README.md LICENSE "$STAGE/" 2>/dev/null || true
  tar -czf "$OUT/${BINARY}_${VERSION#v}_${GOOS}_${GOARCH}.tar.gz" -C "$STAGE" .
  rm -rf "$STAGE"
done

# The binary must not carry the build machine's paths to every downloader.
if strings "$OUT"/*.tar.gz 2>/dev/null | grep -q '/Users/'; then
  echo "error: build embeds absolute /Users/ paths — is -trimpath being applied?" >&2
  exit 1
fi

# 3. checksums ---------------------------------------------------------------
( cd "$OUT" && shasum -a 256 ./*.tar.gz | sed 's|\./||' > SHA256SUMS )
echo "==> artifacts"
( cd "$OUT" && ls -1 ./*.tar.gz | sed 's|\./|    |' )

# 4. tag + release -----------------------------------------------------------
echo "==> tagging $VERSION"
git tag -a "$VERSION" -m "$BINARY $VERSION"
git push origin "$VERSION"

echo "==> creating GitHub release $VERSION"
GH_ARGS=(release create "$VERSION" "$OUT"/*.tar.gz "$OUT/SHA256SUMS"
  --title "$BINARY $VERSION"
  --generate-notes)
[[ -n "${DRAFT:-}" ]] && GH_ARGS+=(--draft)
gh "${GH_ARGS[@]}"

echo "✓ released $BINARY $VERSION"
echo
echo "  Homebrew: update the formula's version + sha256 from:"
echo "    $OUT/SHA256SUMS"
