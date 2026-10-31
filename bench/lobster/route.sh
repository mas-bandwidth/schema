#!/bin/sh
# route.sh — runs one Lobster bench program under the pinned toolchain and
# routes its tagged output (bench/lobster/harness.lobster, THE OUTPUT GRAMMAR).
# Shared by the type leg (bench/lobster/run) and the tables leg
# (bench/tables/lobster/leg). Run from the repository root:
#
#   bench/lobster/route.sh <program.lobster> <import dir>... -- [runner args...]
#
# Import dirs are relative to the program's own directory, as the toolchain's
# --import takes them. The program runs under the DEFAULT JIT with
# --runtime-no-asserts (the release leg: `checks=contract`). `csv ` lines go to
# stdout, `log ` lines to stderr, and ANY other line is refused: the pinned
# toolchain prints compiler warnings on stdout and still exits 0, so a warning
# fails the leg here (zero warnings).
set -e

ROOT="$(pwd -P)"
LOBSTER="${LOBSTER:-$ROOT/dist/lobster-v2026.8/bin/lobster}"
PROGRAM="$1"
shift
IMPORTS=""
while [ $# -gt 0 ] && [ "$1" != "--" ]; do
    IMPORTS="$IMPORTS --import $1"
    shift
done
[ "${1:-}" = "--" ] && shift

OUT="$(mktemp "${TMPDIR:-/tmp}/lobster-bench.XXXXXX")"
trap 'rm -f "$OUT"' EXIT

status=0
# shellcheck disable=SC2086 # IMPORTS is a word list by construction
"$LOBSTER" --runtime-no-asserts $IMPORTS "$PROGRAM" -- --root "$ROOT" "$@" > "$OUT" || status=$?

bad=0
while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
        "csv "*) printf '%s\n' "${line#csv }" ;;
        "log "*) printf '%s\n' "${line#log }" >&2 ;;
        *) printf 'lobster leg REFUSES an untagged line (a compiler warning?): %s\n' "$line" >&2; bad=1 ;;
    esac
done < "$OUT"

if [ "$bad" != 0 ]; then
    exit 1
fi
exit "$status"
