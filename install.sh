#!/usr/bin/env bash
#
# Install the psl compiler system-wide. Run ./install.sh --help for details.

set -euo pipefail

usage() {
	cat <<'EOF'
Install the psl compiler.

Usage:
  ./install.sh [-p <prefix>] [--no-build]

Options:
  -p, --prefix <dir>   install prefix (default: /usr/local, or $PREFIX)
      --bindir <dir>   install directory, overriding <prefix>/bin
  -n, --no-build       install the existing ./psl instead of rebuilding
  -h, --help           show this help

The binary is written to <prefix>/bin/psl. When that directory needs root,
the copy is run through sudo and the command is printed first.

To install without root, pick a prefix you own:

  ./install.sh --prefix "$HOME/.local"
EOF
}

cd "$(dirname "$0")"

prefix=${PREFIX:-/usr/local}
bindir=${BINDIR:-}
build=1

while [ $# -gt 0 ]; do
	case "$1" in
	-p | --prefix)
		[ $# -ge 2 ] || {
			echo "install.sh: $1 needs a value" >&2
			exit 2
		}
		prefix=$2
		shift 2
		;;
	--prefix=*)
		prefix=${1#*=}
		shift
		;;
	--bindir)
		[ $# -ge 2 ] || {
			echo "install.sh: $1 needs a value" >&2
			exit 2
		}
		bindir=$2
		shift 2
		;;
	--bindir=*)
		bindir=${1#*=}
		shift
		;;
	-n | --no-build)
		build=0
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "install.sh: unknown argument \"$1\"" >&2
		usage >&2
		exit 2
		;;
	esac
done

[ -n "$bindir" ] || bindir=$prefix/bin

if [ "$build" -eq 1 ]; then
	./build.sh
fi
if [ ! -x psl ]; then
	echo "install.sh: no ./psl to install; run ./build.sh first" >&2
	exit 1
fi

# writable reports whether the nearest existing ancestor of a path can be
# written to, so a bindir that does not exist yet is judged by its parent.
writable() {
	local dir=$1
	while [ ! -e "$dir" ] && [ "$dir" != "/" ] && [ "$dir" != "." ]; do
		dir=$(dirname "$dir")
	done
	[ -w "$dir" ]
}

sudo=
if [ "$(id -u)" -ne 0 ] && ! writable "$bindir"; then
	if ! command -v sudo >/dev/null 2>&1; then
		echo "install.sh: $bindir is not writable and sudo is not available" >&2
		echo "install.sh: re-run as root, or choose a prefix you own:" >&2
		echo "install.sh:   ./install.sh --prefix \"\$HOME/.local\"" >&2
		exit 1
	fi
	sudo=sudo
	echo "install.sh: $bindir needs root, running: sudo install -m 0755 psl $bindir/psl"
fi

# shellcheck disable=SC2086 # $sudo is deliberately empty when root is not needed
$sudo install -d -m 0755 "$bindir"
# shellcheck disable=SC2086
$sudo install -m 0755 psl "$bindir/psl"

echo "installed $bindir/psl ($("$bindir/psl" --version))"

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
	echo "      (see .pslrc.example)"
fi
