#!/bin/sh
# THE C++ FORMAT GATE's engine (make/cpp.mk tables-cpp-clean, PORTING.md J4).
# Every generated C++ file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format (the house C and C++
# style). Files that drift ride the SHRINK-ONLY ratchet, a list of paths
# (make/cpp-format-allow.txt): a drifting file not listed fails, and a listed
# file that is clean now (or is no longer generated) fails until its line is
# deleted. Files are checked in parallel; nothing here shares state.
#
# THE GATE CHECKS WHAT IT WAS ASKED TO CHECK, OR IT IS RED:
#   - every tree named must be a directory that lists and holds a C++ file;
#   - every directory in EXPECT_DIRS (the units the caller's generation
#     registry writes) must have put a file into the scan;
#   - the formatter must exit 0 on every file, whatever bytes it wrote;
#   - every file listed must come back with a verdict.
#
# usage: cpp-clean-format.sh <ratchet-file> <census|check> <tree>...
# env:   CLANG_FORMAT, JOBS, OUT (scratch dir),
#        EXPECT_DIRS (required; space-separated, may be empty for one tree)
set -eu
ratchet=$1
mode=$2
shift 2
: "${EXPECT_DIRS?tables-cpp-clean: EXPECT_DIRS is not set: the gate must be told the unit directories it has to find}"
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build/cpp-clean}
JOBS=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}
here=$(cd "$(dirname "$0")" && pwd)
style="file:$(pwd)/.clang-format"
export CLANG_FORMAT style

mkdir -p "$OUT/format"
[ $# -gt 0 ] || { echo "tables-cpp-clean: no tree was named"; exit 1; }
: > "$OUT/format/files.txt"
for tree in "$@"; do
	[ -d "$tree" ] || { echo "tables-cpp-clean: $tree is not a directory, so nothing under it was checked"; exit 1; }
	find "$tree" \( -name '*.h' -o -name '*.cpp' \) > "$OUT/format/tree.txt" || { echo "tables-cpp-clean: $tree could not be listed"; exit 1; }
	[ -s "$OUT/format/tree.txt" ] || { echo "tables-cpp-clean: no generated C++ under $tree"; exit 1; }
	cat "$OUT/format/tree.txt" >> "$OUT/format/files.txt"
done
sort -u -o "$OUT/format/files.txt" "$OUT/format/files.txt"
for dir in $EXPECT_DIRS; do
	awk -v p="${dir%/}/" 'index( $0, p ) == 1 { found = 1; exit } END { exit !found }' "$OUT/format/files.txt" || \
		{ echo "tables-cpp-clean: the unit directory $dir put no generated C++ into the check (missing, empty, or outside the trees named)"; exit 1; }
done

# one verdict per file: the formatter's exit status first, then its bytes
rm -rf "$OUT/format/work" && mkdir -p "$OUT/format/work"
work="$OUT/format/work"
export work
xargs -P "$JOBS" -n 1 sh -c '
	tmp="$work/fmt.$$"
	if "$CLANG_FORMAT" --style="$style" "$1" > "$tmp"; then
		if cmp -s "$1" "$tmp"; then v=CLEAN; else v=DRIFT; fi
	else
		v=ERROR
	fi
	rm -f "$tmp"
	printf "%s\t%s\n" "$v" "$1"
' sh < "$OUT/format/files.txt" > "$OUT/format/results.txt" || { echo "tables-cpp-clean: a format worker failed, so the check is incomplete"; exit 1; }
rm -rf "$work"
if [ "$(wc -l < "$OUT/format/results.txt" | tr -d ' ')" != "$(wc -l < "$OUT/format/files.txt" | tr -d ' ')" ]; then
	echo "tables-cpp-clean: $(wc -l < "$OUT/format/files.txt" | tr -d ' ') files were listed and $(wc -l < "$OUT/format/results.txt" | tr -d ' ') came back with a verdict"; exit 1
fi
if grep -q '^ERROR' "$OUT/format/results.txt"; then
	grep '^ERROR' "$OUT/format/results.txt" | cut -f2 | sort | head -40 | while read -r f; do
		echo "tables-cpp-clean: $CLANG_FORMAT exited non-zero on $f, so its output is no verdict"
	done
	exit 1
fi
grep '^DRIFT' "$OUT/format/results.txt" | cut -f2 | sort > "$OUT/format/drift.txt" || true

if [ "$mode" = census ]; then cat "$OUT/format/drift.txt"; exit 0; fi

grep -v '^#' "$ratchet" | grep . | sort > "$OUT/format/allowed.txt" || true
bad=0
comm -13 "$OUT/format/allowed.txt" "$OUT/format/drift.txt" | while read -r f; do
	echo "tables-cpp-clean: clang-format drift in $f, which $ratchet does not list (the list only shrinks)"
done > "$OUT/format/new.txt"
if [ -s "$OUT/format/new.txt" ]; then head -40 "$OUT/format/new.txt"; bad=1; fi
# listed, but clean now or no longer generated
comm -23 "$OUT/format/allowed.txt" "$OUT/format/drift.txt" | while read -r f; do
	if [ -f "$f" ]; then
		echo "tables-cpp-clean: $f is clean now — remove its line from $ratchet"
	else
		echo "tables-cpp-clean: $f is listed in $ratchet but no longer generated — remove its line"
	fi
done > "$OUT/format/gone.txt"
if [ -s "$OUT/format/gone.txt" ]; then head -40 "$OUT/format/gone.txt"; bad=1; fi
[ $bad -eq 0 ] || exit 1
echo "C++: clang-format clean over $*, $(wc -l < "$OUT/format/drift.txt" | tr -d ' ') table-emitter files still on the ratchet"
