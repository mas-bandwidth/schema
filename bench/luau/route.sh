#!/bin/sh
# route.sh — stages and runs one Luau bench program under the pinned
# interpreter and routes its tagged output (bench/luau/harness.luau, THE OUTPUT
# GRAMMAR). Shared by the type leg (bench/luau/run) and the tables leg
# (bench/tables/luau/leg). Run from the repository root:
#
#   bench/luau/route.sh <name> <program.luau> <generated dir> <row> -- [runner args...]
#
# THE STAGING. The interpreter has no io and a generated module requires its
# runtime as "./schema_runtime" (docs/PORT-LUAU.md, THE HARNESS, and the
# security rule on requires), so the program runs from a scratch directory,
# build/bench-luau/<name>/, holding exactly: the generated unit, harness.luau,
# the program as main.luau, and `inputs.luau`, the committed corpus as source:
# <row>.bin (the wire golden, from --wire-dir, default testdata/wire) and
# <row>.variants.bin (the 64 records, from --variant-dir, default
# bench/corpus/variants), each as one string of \x escapes. Nothing outside
# that directory can resolve, and the tree under generated/ is never written.
#
# THE RUN. The pin's `luau` CLI, NATIVE CODEGEN OFF (no --codegen): the row
# labels say so. LUAU overrides the interpreter. `csv ` lines go to stdout,
# `log ` lines to stderr, and ANY other line on the program's stdout is
# refused, so a stray print fails the leg.
set -e

ROOT="$(pwd -P)"
LUAU="${LUAU:-$ROOT/dist/luau-0.741/luau}"
NAME="$1"
PROGRAM="$2"
GEN="$3"
ROW="$4"
shift 4
[ "${1:-}" = "--" ] && shift

WIRE_DIR="$ROOT/testdata/wire"
VARIANT_DIR="$ROOT/bench/corpus/variants"
ARGS=""
while [ $# -gt 0 ]; do
    case "$1" in
        --wire-dir)
            [ $# -ge 2 ] || { echo "--wire-dir takes a directory" >&2; exit 1; }
            case "$2" in /*) WIRE_DIR="$2" ;; *) WIRE_DIR="$ROOT/$2" ;; esac
            shift 2 ;;
        --variant-dir)
            [ $# -ge 2 ] || { echo "--variant-dir takes a directory" >&2; exit 1; }
            case "$2" in /*) VARIANT_DIR="$2" ;; *) VARIANT_DIR="$ROOT/$2" ;; esac
            shift 2 ;;
        *)
            ARGS="$ARGS $1"
            shift ;;
    esac
done

STAGE="$ROOT/build/bench-luau/$NAME"
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp "$GEN"/*.luau "$STAGE/"
cp bench/luau/harness.luau "$STAGE/harness.luau"
cp "$PROGRAM" "$STAGE/main.luau"

# source_string <file> — the file's bytes as one Luau string literal of \x
# escapes ("" for a missing file, which the harness refuses by name)
source_string() {
    printf '"'
    if [ -f "$1" ]; then
        od -An -v -tx1 "$1" | tr -d '\n' | sed 's/ /\\x/g'
    fi
    printf '"'
}
{
    printf 'return {\n'
    printf '\tgolden = { name = "%s.bin", data = ' "$ROW"
    source_string "$WIRE_DIR/$ROW.bin"
    printf ' },\n'
    printf '\tvariants = { name = "%s.variants.bin", data = ' "$ROW"
    source_string "$VARIANT_DIR/$ROW.variants.bin"
    printf ' },\n}\n'
} > "$STAGE/inputs.luau"

OUT="$STAGE/out.txt"
status=0
# shellcheck disable=SC2086 # ARGS is a word list by construction
(cd "$STAGE" && "$LUAU" main.luau -a $ARGS) > "$OUT" || status=$?

bad=0
while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
        "csv "*) printf '%s\n' "${line#csv }" ;;
        "log "*) printf '%s\n' "${line#log }" >&2 ;;
        *) printf 'luau leg REFUSES an untagged line: %s\n' "$line" >&2; bad=1 ;;
    esac
done < "$OUT"

if [ "$bad" != 0 ]; then
    exit 1
fi
exit "$status"
