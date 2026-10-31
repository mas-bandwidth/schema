#!/bin/sh
# THE C++ FORMAT GATE's engine (make/cpp.mk tables-cpp-clean, PORTING.md J4).
# Every generated C++ file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format (the house C and C++
# style). Files that drift ride the SHRINK-ONLY ratchet, a list of paths
# (make/cpp-format-allow.txt): a drifting file not listed fails, and a listed
# file that is clean now (or is no longer generated) fails until its line is
# deleted. Files are checked in parallel; nothing here shares state.
#
# usage: cpp-clean-format.sh <ratchet-file> <census|check> <tree>...
# env:   CLANG_FORMAT, JOBS, OUT (scratch dir)
set -eu
ratchet=$1
mode=$2
shift 2
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build/cpp-clean}
JOBS=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}
here=$(cd "$(dirname "$0")" && pwd)
style="file:$(pwd)/.clang-format"
export CLANG_FORMAT style

mkdir -p "$OUT/format"
find "$@" \( -name '*.h' -o -name '*.cpp' \) | sort > "$OUT/format/files.txt"
[ -s "$OUT/format/files.txt" ] || { echo "tables-cpp-clean: no generated C++ under $*"; exit 1; }

# one line per drifting file
xargs -P "$JOBS" -n 1 sh -c '"$CLANG_FORMAT" --style="$style" "$1" | cmp -s "$1" - || echo "$1"' sh \
	< "$OUT/format/files.txt" | sort > "$OUT/format/drift.txt"

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
