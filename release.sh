#!/bin/bash
# Releases psl to GitHub and publishes the binaries as release assets.
#
# The version comes from the VERSION file; the tag is "v$VERSION". Every target
# in TARGETS is cross-compiled with ./build.sh and packaged with the README and
# .pslrc.example — tar.gz everywhere, zip for Windows — plus a SHA256SUMS file.
#
# Usage:
#   ./release.sh              # build, tag, and publish the GitHub release
#   ./release.sh --dry-run    # build and package into dist/, publish nothing
#
# Env:
#   TARGETS="darwin/arm64 linux/amd64 …"   platforms to build
#                                          (default: the five below)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

usage() {
  cat <<'EOF'
Release psl to GitHub.

Usage:
  ./release.sh              # build, tag, and publish the GitHub release
  ./release.sh --dry-run    # build and package into dist/, publish nothing

The version comes from the VERSION file; the tag is "v$VERSION". Every target
in TARGETS is cross-compiled and packaged with the README and .pslrc.example,
plus a SHA256SUMS file.

Env:
  TARGETS="darwin/arm64 linux/amd64 …"   platforms to build
EOF
}

DRY_RUN=0
while [ $# -gt 0 ]; do
  case "$1" in
    -n|--dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "❌ Unknown argument '$1' — use --dry-run or --help."
      exit 2
      ;;
  esac
done

if [ ! -f VERSION ]; then
  echo "❌ No VERSION file found — create one holding the version, e.g. 0.1.0."
  exit 1
fi
VERSION="$(tr -d '[:space:]' < VERSION)"
TAG="v$VERSION"
TARGETS="${TARGETS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64}"
DIST="dist"

echo "==> Releasing psl $TAG"

# ── preflight ────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  echo "❌ Go is required to build the release."
  exit 1
fi
if [ -z "${TARGETS// /}" ]; then
  echo "❌ TARGETS is empty — nothing to build."
  exit 1
fi
case "$TARGETS" in
  *windows*)
    if ! command -v zip &>/dev/null; then
      echo "❌ zip is required to package the Windows build."
      exit 1
    fi
    ;;
esac

if [ "$DRY_RUN" -eq 0 ]; then
  if ! command -v gh &>/dev/null; then
    echo "❌ The GitHub CLI (gh) is required — install it, or use --dry-run."
    exit 1
  fi
  if ! gh auth status &>/dev/null; then
    echo "❌ gh is not authenticated — run: gh auth login"
    exit 1
  fi
  if ! git remote get-url origin &>/dev/null; then
    echo "❌ No 'origin' remote — add the GitHub repository first."
    exit 1
  fi
  if [ -n "$(git status --porcelain)" ]; then
    echo "❌ The working tree has uncommitted changes — commit them so the tag"
    echo "   points at exactly what is being released:"
    git status --short
    exit 1
  fi
  if gh release view "$TAG" &>/dev/null; then
    echo "❌ Release $TAG already exists — bump VERSION first."
    exit 1
  fi
fi

echo "==> Running tests…"
go test ./...

# ── build ────────────────────────────────────────────────────────────────────
rm -rf "$DIST"
mkdir -p "$DIST"

ASSETS=()
NAMES=()
for TARGET in $TARGETS; do
  GOOS="${TARGET%%/*}"
  GOARCH="${TARGET##*/}"
  NAME="psl-${VERSION}-${GOOS}-${GOARCH}"
  STAGE="$DIST/$NAME"

  if [ "$GOOS" = "windows" ]; then
    BIN="psl.exe"
  else
    BIN="psl"
  fi

  echo "==> Building $GOOS/${GOARCH}…"
  mkdir -p "$STAGE"
  VERSION="$TAG" GOOS="$GOOS" GOARCH="$GOARCH" ./build.sh -o "$STAGE/$BIN"

  cp README.md .pslrc.example "$STAGE/"
  if [ -f LICENSE ]; then
    cp LICENSE "$STAGE/"
  fi

  if [ "$GOOS" = "windows" ]; then
    ARCHIVE="$NAME.zip"
    (cd "$DIST" && zip -q -r "$ARCHIVE" "$NAME")
  else
    ARCHIVE="$NAME.tar.gz"
    tar -czf "$DIST/$ARCHIVE" -C "$DIST" "$NAME"
  fi
  rm -rf "$STAGE"

  ASSETS+=("$DIST/$ARCHIVE")
  NAMES+=("$ARCHIVE")
done

# ── checksums ────────────────────────────────────────────────────────────────
if command -v sha256sum &>/dev/null; then
  SHA=(sha256sum)
else
  SHA=(shasum -a 256)
fi
(cd "$DIST" && "${SHA[@]}" "${NAMES[@]}" > SHA256SUMS)
ASSETS+=("$DIST/SHA256SUMS")

echo ""
echo "==> Packaged into $DIST/"
for A in "${ASSETS[@]}"; do
  echo "  $(basename "$A")  ($(du -h "$A" | cut -f1 | tr -d ' '))"
done

if [ "$DRY_RUN" -eq 1 ]; then
  echo ""
  echo "Dry run — nothing was tagged or published. Assets are in $DIST/."
  exit 0
fi

# ── release notes ────────────────────────────────────────────────────────────
echo ""
read -r -p "Release notes: " NOTES
if [ -z "$NOTES" ]; then
  echo "❌ Release notes are empty — aborting."
  exit 1
fi

REPO="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || echo "origin")"
echo ""
read -r -p "Publish $TAG with ${#ASSETS[@]} assets to $REPO? [y/N] " CONFIRM
case "$CONFIRM" in
  y|Y|yes|YES) ;;
  *)
    echo "Aborted. Assets are in $DIST/."
    exit 1
    ;;
esac

# ── git tag ──────────────────────────────────────────────────────────────────
if git rev-parse "$TAG" &>/dev/null; then
  echo "==> Tag $TAG already exists, skipping tag creation."
else
  echo "==> Creating git tag $TAG"
  git tag -a "$TAG" -m "Release $TAG"
  git push origin "$TAG"
fi

# ── github release ───────────────────────────────────────────────────────────
echo "==> Creating GitHub release $TAG"
gh release create "$TAG" \
  --title "psl $TAG" \
  --notes "$NOTES" \
  "${ASSETS[@]}"

echo ""
echo "Released: $TAG"
for A in "${ASSETS[@]}"; do
  echo "  Asset:  $(basename "$A")"
done

# ── cleanup ──────────────────────────────────────────────────────────────────
echo "==> Cleaning up build artifacts…"
rm -rf "$DIST"
