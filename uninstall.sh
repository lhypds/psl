#!/usr/bin/env bash
#
# Remove an installed psl compiler. Run ./uninstall.sh --help for details.

set -euo pipefail

usage() {
	cat <<'EOF'
Remove the psl compiler installed by ./install.sh.

Usage:
  ./uninstall.sh [-p <prefix>]

Options:
  -p, --prefix <dir>   install prefix to remove from (default: /usr/local, or $PREFIX)
      --bindir <dir>   install directory, overriding <prefix>/bin
  -h, --help           show this help

Only <prefix>/bin/psl is removed. Your .pslrc is configuration, not part of
the installation, and is left alone.
EOF
}

cd "$(dirname "$0")"

prefix=${PREFIX:-/usr/local}
bindir=${BINDIR:-}

while [ $# -gt 0 ]; do
	case "$1" in
	-p | --prefix)
		[ $# -ge 2 ] || {
			echo "uninstall.sh: $1 needs a value" >&2
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
			echo "uninstall.sh: $1 needs a value" >&2
			exit 2
		}
		bindir=$2
		shift 2
		;;
	--bindir=*)
		bindir=${1#*=}
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "uninstall.sh: unknown argument \"$1\"" >&2
		usage >&2
		exit 2
		;;
	esac
done

[ -n "$bindir" ] || bindir=$prefix/bin
target=$bindir/psl

if [ ! -e "$target" ]; then
	echo "uninstall.sh: nothing to remove, $target does not exist"
	exit 0
fi
if [ -d "$target" ]; then
	echo "uninstall.sh: $target is a directory, refusing to remove it" >&2
	exit 1
fi

sudo=
if [ "$(id -u)" -ne 0 ] && [ ! -w "$bindir" ]; then
	if ! command -v sudo >/dev/null 2>&1; then
		echo "uninstall.sh: $bindir is not writable and sudo is not available" >&2
		exit 1
	fi
	sudo=sudo
	echo "uninstall.sh: $bindir needs root, running: sudo rm -f $target"
fi

# shellcheck disable=SC2086 # $sudo is deliberately empty when root is not needed
$sudo rm -f "$target"
echo "removed $target"

# A copy installed elsewhere (say by `go install`) would still shadow the PATH.
if remaining=$(command -v psl 2>/dev/null); then
	echo
	echo "note: another psl is still on your PATH at $remaining"
fi

if [ -f "${HOME-}/.pslrc" ]; then
	echo "note: ${HOME}/.pslrc was left in place; delete it yourself if you want it gone"
fi
