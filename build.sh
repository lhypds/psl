#!/usr/bin/env bash
#
# Build the psl compiler. Run ./build.sh --help for details.

set -euo pipefail

usage() {
	cat <<'EOF'
Build the psl compiler.

Usage:
  ./build.sh [-o <output>]

Options:
  -o, --output <path>   where to write the executable (default: ./psl)
  -h, --help            show this help

The version reported by `psl --version` is stamped in from `git describe`, or
from $VERSION when that is set (release.sh uses it).

GOOS and GOARCH are honoured, so cross-compiling is just:

  GOOS=linux GOARCH=amd64 ./build.sh -o dist/psl-linux-amd64
EOF
}

cd "$(dirname "$0")"

output=psl
while [ $# -gt 0 ]; do
	case "$1" in
	-o | --output)
		[ $# -ge 2 ] || {
			echo "build.sh: $1 needs a value" >&2
			exit 2
		}
		output=$2
		shift 2
		;;
	--output=*)
		output=${1#*=}
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "build.sh: unknown argument \"$1\"" >&2
		usage >&2
		exit 2
		;;
	esac
done

if ! command -v go >/dev/null 2>&1; then
	echo "build.sh: go is not installed or not in PATH" >&2
	exit 1
fi

version=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo devel)}

mkdir -p "$(dirname "$output")"
go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$output" .

echo "built $output ($version, $(go env GOOS)/$(go env GOARCH))"
