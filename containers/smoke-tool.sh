#!/bin/sh
set -eu

if [ "$#" -lt 3 ]; then
	echo "usage: $0 <image> <binary> <--help|--version> [args...]" >&2
	exit 2
fi

image=$1
binary=$2
shift 2

smoke_env=
if [ "${SMOKE_ALLOW_NONZERO:-}" = "1" ]; then
	smoke_env="-e SMOKE_ALLOW_NONZERO=1"
fi

"${DOCKER:-docker}" run --rm $smoke_env --entrypoint sh "$image" -c '
set -eu
binary=$1
shift
command -v "$binary" >/dev/null
out="${TMPDIR:-/tmp}/heph-tool-smoke.out"
if "$binary" "$@" >"$out" 2>&1; then
	exit 0
fi
status=$?
if [ "${SMOKE_ALLOW_NONZERO:-}" = "1" ] && [ -s "$out" ]; then
	exit 0
fi
if [ "$status" -ne 0 ]; then
	cat "$out" >&2
	exit "$status"
fi
' smoke "$binary" "$@"
