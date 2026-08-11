#!/bin/sh
#
# Install the psl compiler from its latest GitHub release, on macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/lhypds/psl/main/get.sh | sh
#
# No Go toolchain is needed: this downloads the binary built for your platform,
# checks it against the release's SHA256SUMS — a release it cannot verify is
# never installed — and copies it into a bin directory.
#
# Options go through the pipe with `sh -s --`:
#
#   curl -fsSL https://raw.githubusercontent.com/lhypds/psl/main/get.sh |
#       sh -s -- --prefix "$HOME/.local"
#
# Env:
#   PSL_VERSION   version to install, with or without the leading "v"
#   PSL_BINDIR    install directory, overriding <prefix>/bin
#   PSL_REPO      "owner/name" to download from (default: lhypds/psl)
#   PREFIX        install prefix; the binary lands in <prefix>/bin
#   GITHUB_TOKEN  lifts GitHub's anonymous rate limit, if you hit it

set -eu

REPO=${PSL_REPO:-lhypds/psl}
version=${PSL_VERSION:-}
prefix=${PREFIX:-}
bindir=${PSL_BINDIR:-}
tmp=

usage() {
	cat <<'EOF'
Install the psl compiler from its latest GitHub release.

Usage:
  curl -fsSL .../get.sh | sh
  curl -fsSL .../get.sh | sh -s -- [-p <prefix>] [-V <version>]

Options:
  -p, --prefix <dir>    install prefix (default: /usr/local when writable,
                        otherwise $HOME/.local)
      --bindir <dir>    install directory, overriding <prefix>/bin
  -V, --version <ver>   version to install (default: the latest release)
  -h, --help            show this help

For a system-wide install on a machine where /usr/local needs root:

  curl -fsSL .../get.sh | sudo sh
EOF
}

die() {
	echo "get.sh: $*" >&2
	exit 1
}

cleanup() {
	[ -z "$tmp" ] || rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM

while [ $# -gt 0 ]; do
	case "$1" in
	-p | --prefix)
		[ $# -ge 2 ] || die "$1 needs a value"
		prefix=$2
		shift 2
		;;
	--prefix=*)
		prefix=${1#*=}
		shift
		;;
	--bindir)
		[ $# -ge 2 ] || die "$1 needs a value"
		bindir=$2
		shift 2
		;;
	--bindir=*)
		bindir=${1#*=}
		shift
		;;
	-V | --version)
		[ $# -ge 2 ] || die "$1 needs a value"
		version=$2
		shift 2
		;;
	--version=*)
		version=${1#*=}
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "get.sh: unknown argument \"$1\"" >&2
		usage >&2
		exit 2
		;;
	esac
done

# ── platform ─────────────────────────────────────────────────────────────────
# The names have to match the assets release.sh publishes: psl-<version>-<goos>-<goarch>.
os=$(uname -s 2>/dev/null || echo unknown)
case "$os" in
Linux) goos=linux ;;
Darwin) goos=darwin ;;
MINGW* | MSYS* | CYGWIN*)
	die "on Windows use the PowerShell installer:
    irm https://raw.githubusercontent.com/$REPO/main/get.ps1 | iex"
	;;
*) die "unsupported operating system \"$os\" — build from source instead: https://github.com/$REPO" ;;
esac

arch=$(uname -m 2>/dev/null || echo unknown)
case "$arch" in
x86_64 | amd64) goarch=amd64 ;;
arm64 | aarch64) goarch=arm64 ;;
*) die "unsupported architecture \"$arch\" — build from source instead: https://github.com/$REPO" ;;
esac

# ── downloader ───────────────────────────────────────────────────────────────
if command -v curl >/dev/null 2>&1; then
	http_get() { curl -fsSL ${GITHUB_TOKEN:+-H "Authorization: Bearer $GITHUB_TOKEN"} "$1"; }
	http_save() { curl -fsSL -o "$2" "$1"; }
	http_final_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	http_get() { wget -qO- ${GITHUB_TOKEN:+--header="Authorization: Bearer $GITHUB_TOKEN"} "$1"; }
	http_save() { wget -qO "$2" "$1"; }
	http_final_url() {
		wget -qS --spider "$1" 2>&1 |
			awk 'tolower($1) == "location:" { url = $2 } END { print url }'
	}
else
	die "neither curl nor wget is installed"
fi

# latest_version reads the version off the redirect that /releases/latest
# serves, which costs no GitHub API request, and falls back to the API.
latest_version() {
	url=$(http_final_url "https://github.com/$REPO/releases/latest" 2>/dev/null || echo)
	case "$url" in
	*/releases/tag/*)
		echo "${url##*/releases/tag/}"
		return 0
		;;
	esac
	http_get "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

if [ -z "$version" ]; then
	version=$(latest_version) || version=
	[ -n "$version" ] || die "could not work out the latest release of $REPO; pass --version <ver>"
fi
version=${version#v}

# ── install directory ────────────────────────────────────────────────────────
# writable reports whether the nearest existing ancestor of a path can be
# written to, so a bindir that does not exist yet is judged by its parent.
writable() {
	dir=$1
	while [ ! -e "$dir" ] && [ "$dir" != "/" ] && [ "$dir" != "." ]; do
		dir=$(dirname "$dir")
	done
	[ -w "$dir" ]
}

if [ -z "$bindir" ]; then
	if [ -n "$prefix" ]; then
		bindir=$prefix/bin
	elif [ "$(id -u)" = 0 ] || writable /usr/local/bin; then
		bindir=/usr/local/bin
	else
		bindir=${HOME:?HOME is not set; pass --prefix <dir>}/.local/bin
	fi
fi

if ! writable "$bindir"; then
	echo "get.sh: $bindir is not writable by this user." >&2
	echo "get.sh: either install it system-wide with root:" >&2
	echo "get.sh:   curl -fsSL https://raw.githubusercontent.com/$REPO/main/get.sh | sudo sh" >&2
	echo "get.sh: or install somewhere you own:" >&2
	echo "get.sh:   curl -fsSL https://raw.githubusercontent.com/$REPO/main/get.sh | sh -s -- --prefix \"\$HOME/.local\"" >&2
	exit 1
fi

# ── download and verify ──────────────────────────────────────────────────────
archive=psl-$version-$goos-$goarch.tar.gz
base=https://github.com/$REPO/releases/download/v$version
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t psl-install)

echo "get.sh: downloading psl $version for $goos/${goarch}..."
http_save "$base/$archive" "$tmp/$archive" ||
	die "no $archive in release v$version of $REPO — see https://github.com/$REPO/releases"
http_save "$base/SHA256SUMS" "$tmp/SHA256SUMS" ||
	die "release v$version has no SHA256SUMS; psl will not install a release it cannot verify"

want=$(awk -v name="$archive" '{ sub(/^\*/, "", $2); if ($2 == name) { print tolower($1); exit } }' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "SHA256SUMS of release v$version lists no checksum for $archive"

if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
elif command -v openssl >/dev/null 2>&1; then
	got=$(openssl dgst -sha256 "$tmp/$archive" | awk '{ print $NF }')
else
	die "none of sha256sum, shasum or openssl is installed; psl will not install a release it cannot verify"
fi
[ "$(echo "$got" | tr 'A-Z' 'a-z')" = "$want" ] ||
	die "checksum mismatch for $archive: got $got, want $want"

# ── install ──────────────────────────────────────────────────────────────────
tar -xzf "$tmp/$archive" -C "$tmp" || die "could not unpack $archive"
bin=$tmp/psl-$version-$goos-$goarch/psl
if [ ! -f "$bin" ]; then
	bin=$(find "$tmp" -type f -name psl | head -n 1)
	[ -n "$bin" ] || die "$archive contains no psl binary"
fi

mkdir -p "$bindir" || die "could not create $bindir"
chmod 0755 "$bin"
# Copy in beside the target and rename, so an interrupted install cannot leave
# a half-written psl behind, and a running one is replaced rather than rewritten.
cp "$bin" "$bindir/.psl.new" || die "could not write to $bindir"
mv "$bindir/.psl.new" "$bindir/psl" || die "could not install $bindir/psl"

echo "get.sh: installed $bindir/psl ($("$bindir/psl" --version))"

case ":${PATH-}:" in
*":$bindir:"*) ;;
*)
	echo
	echo "warning: $bindir is not on your PATH; add it with"
	echo "  export PATH=\"$bindir:\$PATH\""
	;;
esac

if [ -z "${OPENAI_API_KEY-}" ] && [ -z "${ANTHROPIC_API_KEY-}" ] &&
	[ ! -f "$PWD/.pslrc" ] && [ ! -f "${HOME-}/.pslrc" ]; then
	echo
	echo "next: export OPENAI_API_KEY or ANTHROPIC_API_KEY, or write a .pslrc"
	echo "      (run: psl config)"
fi
