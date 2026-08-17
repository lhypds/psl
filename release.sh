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

normalize_version() {
  local v="$1"
  v="${v#v}"
  v="$(printf '%s' "$v" | tr -d '[:space:]')"
  printf '%s' "$v"
}

version_compare() {
  local lhs="$1"
  local rhs="$2"
  local IFS='.'
  local -a a_parts b_parts
  local max_len i a_seg b_seg

  read -r -a a_parts <<< "$lhs"
  read -r -a b_parts <<< "$rhs"

  max_len="${#a_parts[@]}"
  if [ "${#b_parts[@]}" -gt "$max_len" ]; then
    max_len="${#b_parts[@]}"
  fi

  for ((i = 0; i < max_len; i++)); do
    a_seg="${a_parts[i]:-0}"
    b_seg="${b_parts[i]:-0}"
    if ! [[ "$a_seg" =~ ^[0-9]+$ ]] || ! [[ "$b_seg" =~ ^[0-9]+$ ]]; then
      echo "Error: VERSION contains non-numeric segments ($lhs vs $rhs)."
      exit 1
    fi

    if ((10#$a_seg > 10#$b_seg)); then
      echo 1
      return
    fi
    if ((10#$a_seg < 10#$b_seg)); then
      echo -1
      return
    fi
  done

  echo 0
}

bump_version_interactive() {
  local current="$1"
  local IFS='.'
  local -a parts
  local count choice idx i

  read -r -a parts <<< "$current"
  count="${#parts[@]}"
  if [ "$count" -eq 0 ]; then
    echo "Error: invalid VERSION '$current'."
    exit 1
  fi

  for i in "${parts[@]}"; do
    if ! [[ "$i" =~ ^[0-9]+$ ]]; then
      echo "Error: VERSION contains non-numeric segments ($current)."
      exit 1
    fi
  done

  read -r -p "VERSION $current equals latest release. Which segment to bump from right? [1=last, 2=second last, ...] (default: 1): " choice
  choice="${choice:-1}"
  if ! [[ "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -lt 1 ] || [ "$choice" -gt "$count" ]; then
    echo "Error: invalid segment selection '$choice'."
    exit 1
  fi

  idx=$((count - choice))
  parts[idx]=$((10#${parts[idx]} + 1))
  for ((i = idx + 1; i < count; i++)); do
    parts[i]=0
  done

  local result="${parts[0]}"
  for ((i = 1; i < count; i++)); do
    result+=".${parts[i]}"
  done
  printf '%s' "$result"
}

prepare_version_for_release() {
  if ! command -v gh &>/dev/null; then
    echo "Error: GitHub CLI (gh) is required."
    exit 1
  fi
  if ! gh auth status &>/dev/null; then
    echo "Error: gh is not authenticated. Run: gh auth login"
    exit 1
  fi

  local current draft_tag draft_tags latest_tag latest cmp new_version branch
  current="$1"

  if ! draft_tags="$(gh release list --limit 1000 --json tagName,isDraft \
    --jq '.[] | select(.isDraft) | .tagName' 2>/dev/null)"; then
    echo "Error: unable to check GitHub for draft releases."
    exit 1
  fi
  if [ -n "$draft_tags" ]; then
    echo "Warning: GitHub draft release(s) found:"
    while IFS= read -r draft_tag; do
      echo "  - $draft_tag"
    done <<< "$draft_tags"
    echo "Review, publish, or delete the draft release(s) before continuing."
    exit 1
  fi

  latest_tag="$(gh release view --json tagName --jq '.tagName' 2>/dev/null || true)"
  if [ "$latest_tag" = "null" ]; then
    latest_tag=""
  fi

  if [ -z "$latest_tag" ]; then
    echo "No existing GitHub release found. Releasing VERSION $current."
    VERSION="$current"
    return
  fi

  latest="$(normalize_version "$latest_tag")"
  cmp="$(version_compare "$current" "$latest")"

  if [ "$cmp" -gt 0 ]; then
    echo "VERSION $current is greater than latest release $latest. Continue releasing."
    VERSION="$current"
    return
  fi

  if [ "$cmp" -lt 0 ]; then
    echo "Error: VERSION $current is lower than latest release $latest."
    exit 1
  fi

  new_version="$(bump_version_interactive "$current")"
  printf '%s\n' "$new_version" > VERSION

  git add VERSION
  git commit -m "$new_version"

  branch="$(git branch --show-current 2>/dev/null || true)"
  if [ -n "$branch" ]; then
    git push origin "$branch"
  else
    git push
  fi

  echo "VERSION bumped to $new_version, committed, and pushed."
  VERSION="$new_version"
}

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
VERSION="$(normalize_version "$(cat VERSION)")"
if [ -z "$VERSION" ]; then
  echo "❌ VERSION file is empty."
  exit 1
fi

if [ "$DRY_RUN" -eq 0 ]; then
  prepare_version_for_release "$VERSION"
fi

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
