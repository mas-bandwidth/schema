#!/bin/sh
# THE C++ FORMAT GATE's engine (make/cpp.mk tables-cpp-clean, PORTING.md J4).
# Every generated C++ file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format (the house C and C++
# style). Files that drift ride the SHRINK-ONLY ratchet, a list of paths
# (make/cpp-format-allow.txt): a drifting file not listed fails, and a listed
# file that is clean now (or is no longer generated) fails until its line is
# deleted. Files are checked in parallel; nothing here shares state.
#
# THE GATE CHECKS WHAT IT WAS ASKED TO CHECK, OR IT IS RED (as its C twin,
# make/checks/c-clean-format.sh, does):
#   - the ratchet is a readable file (an empty one is the empty list);
#   - every tree named is a directory that holds a .h or .cpp file, so a
#     unit tree that was never generated cannot hide beside one that was;
#   - clang-format exits 0 on every file: a formatter that failed wrote no
#     verdict, whatever bytes it printed;
#   - every file listed comes back with a verdict;
#   - every ratchet line names a file this scan reached: a line for a file on
#     disk but under none of the trees is named as such, not "clean now".
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
style="file:$(pwd)/.clang-format"
tab=$(printf '\t')
export CLANG_FORMAT style

[ -f "$ratchet" ] && [ -r "$ratchet" ] || { echo "tables-cpp-clean: the ratchet $ratchet is missing or unreadable, so no file's standing on it can be judged"; exit 1; }
[ $# -gt 0 ] || { echo "tables-cpp-clean: no tree was named"; exit 1; }

mkdir -p "$OUT/format"
work=$OUT/format/workers
rm -rf "$work" && mkdir -p "$work"
: > "$OUT/format/files.txt"
for tree in "$@"; do
	[ -d "$tree" ] || { echo "tables-cpp-clean: $tree is not a directory, so nothing under it was checked"; exit 1; }
	find "$tree" \( -name '*.h' -o -name '*.cpp' \) > "$OUT/format/tree.txt" || { echo "tables-cpp-clean: $tree could not be listed"; exit 1; }
	[ -s "$OUT/format/tree.txt" ] || { echo "tables-cpp-clean: no generated C++ under $tree; nothing there was held to clang-format"; exit 1; }
	cat "$OUT/format/tree.txt" >> "$OUT/format/files.txt"
done
sort -u -o "$OUT/format/files.txt" "$OUT/format/files.txt"

# one verdict line per file: CLEAN, DRIFT, or ERROR when clang-format failed
tr '\n' '\000' < "$OUT/format/files.txt" | xargs -0 -P "$JOBS" -n 16 sh -c '
	wk=$1; shift
	out="$wk/res.$$"
	tmp="$wk/fmt.$$"
	for f in "$@"; do
		if "$CLANG_FORMAT" --style="$style" "$f" > "$tmp"; then
			if cmp -s "$f" "$tmp"; then
				printf "CLEAN\t%s\n" "$f" >> "$out"
			else
				printf "DRIFT\t%s\n" "$f" >> "$out"
			fi
		else
			printf "ERROR\t%s\n" "$f" >> "$out"
		fi
	done
	rm -f "$tmp"
' sh "$work" || { echo "tables-cpp-clean: a format worker failed, so the check is incomplete"; exit 1; }

: > "$OUT/format/results.txt"
if ls "$work"/res.* >/dev/null 2>&1; then
	cat "$work"/res.* | sort -t "$tab" -k2,2 > "$OUT/format/results.txt"
fi
bad=0
listed=$(wc -l < "$OUT/format/files.txt" | tr -d ' ')
judged=$(wc -l < "$OUT/format/results.txt" | tr -d ' ')
if [ "$listed" != "$judged" ]; then
	echo "tables-cpp-clean: $listed files were listed and $judged came back with a verdict"; bad=1
fi
grep "^ERROR$tab" "$OUT/format/results.txt" | cut -f2- | while IFS= read -r f; do
	echo "tables-cpp-clean: $CLANG_FORMAT exited non-zero on $f, so its output is no verdict"
done > "$OUT/format/errors.txt"
if [ -s "$OUT/format/errors.txt" ]; then head -40 "$OUT/format/errors.txt"; bad=1; fi
# one line per drifting file
grep "^DRIFT$tab" "$OUT/format/results.txt" | cut -f2- | sort > "$OUT/format/drift.txt" || true

if [ "$mode" = census ]; then
	[ $bad -eq 0 ] || exit 1
	cat "$OUT/format/drift.txt"; exit 0
fi

sed -e '/^#/d' -e '/^$/d' "$ratchet" | sort > "$OUT/format/allowed.txt"
comm -13 "$OUT/format/allowed.txt" "$OUT/format/drift.txt" | while IFS= read -r f; do
	echo "tables-cpp-clean: clang-format drift in $f, which $ratchet does not list (the list only shrinks)"
done > "$OUT/format/new.txt"
if [ -s "$OUT/format/new.txt" ]; then head -40 "$OUT/format/new.txt"; bad=1; fi
# listed, but clean now, under none of the scanned trees, or no longer generated
comm -23 "$OUT/format/allowed.txt" "$OUT/format/drift.txt" | while IFS= read -r f; do
	if grep -Fxq -- "CLEAN$tab$f" "$OUT/format/results.txt"; then
		echo "tables-cpp-clean: $f is clean now — remove its line from $ratchet"
	elif grep -Fxq -- "$f" "$OUT/format/files.txt"; then
		: # no verdict: named above as the formatter's failure
	elif [ -f "$f" ]; then
		echo "tables-cpp-clean: $f is listed in $ratchet but is under none of the trees this gate scans ($*) — remove its line, or scan its tree"
	else
		echo "tables-cpp-clean: $f is listed in $ratchet but no longer generated — remove its line"
	fi
done > "$OUT/format/gone.txt"
if [ -s "$OUT/format/gone.txt" ]; then head -40 "$OUT/format/gone.txt"; bad=1; fi
rm -rf "$work"
[ $bad -eq 0 ] || exit 1
echo "C++: clang-format clean over $*, $(wc -l < "$OUT/format/drift.txt" | tr -d ' ') table-emitter files still on the ratchet"
