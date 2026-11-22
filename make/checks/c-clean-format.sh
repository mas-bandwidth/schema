#!/bin/sh
# THE C FORMAT GATE's engine (make/c.mk tables-c-clean, docs/PORTING.md J4).
# Every generated C file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format. Files that drift ride
# the SHRINK-ONLY ratchet, a list of paths (make/c-format-allow.txt): a
# drifting file not listed fails, and a listed file that is clean now (or is no
# longer generated) fails until its line is deleted.
#
# THE GATE CHECKS WHAT IT WAS ASKED TO CHECK, OR IT IS RED:
#   - every tree named must be a directory that lists and holds a C file;
#   - every directory in EXPECT_DIRS (the units the caller's generation
#     registry writes) must have put a file into the scan;
#   - the formatter must exit 0 on every file, whatever bytes it wrote;
#   - every file listed must come back with a verdict;
#   - the ratchet must be a readable file (an empty one is the empty list),
#     and every path on it must be a drifting file of this scan;
#   - a path is one line, spaces and all, from the scan to the verdict.
#
# usage: c-clean-format.sh <ratchet-file> <tree>...
# env:   CLANG_FORMAT, OUT (scratch dir),
#        EXPECT_DIRS (required; space-separated, may be empty for one tree)
set -eu
allow=$1
shift
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build}
: "${EXPECT_DIRS?tables-c-clean: EXPECT_DIRS is not set: the gate must be told the unit directories it has to find}"
[ -f "$allow" ] && [ -r "$allow" ] || { echo "tables-c-clean: the ratchet $allow is missing or unreadable, so no file's standing on it can be judged"; exit 1; }
tab=$(printf '\t')
mkdir -p "$OUT"
seen=$OUT/c-format-seen.txt
: > "$seen"
style="file:$(pwd)/.clang-format"
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
work=$OUT/c-format-workers
rm -rf "$work" && mkdir -p "$work"

[ $# -gt 0 ] || { echo "tables-c-clean: no tree was named"; exit 1; }
: > "$OUT/c-format-files.txt"
for tree in "$@"; do
	[ -d "$tree" ] || { echo "tables-c-clean: $tree is not a directory, so nothing under it was checked"; exit 1; }
	find "$tree" \( -name '*.h' -o -name '*.c' \) > "$OUT/c-format-tree.txt" || { echo "tables-c-clean: $tree could not be listed"; exit 1; }
	[ -s "$OUT/c-format-tree.txt" ] || { echo "tables-c-clean: no generated C under $tree"; exit 1; }
	cat "$OUT/c-format-tree.txt" >> "$OUT/c-format-files.txt"
done
sort -u -o "$OUT/c-format-files.txt" "$OUT/c-format-files.txt"
checked=$(wc -l < "$OUT/c-format-files.txt" | tr -d ' ')
for dir in $EXPECT_DIRS; do
	awk -v p="${dir%/}/" 'index( $0, p ) == 1 { found = 1; exit } END { exit !found }' "$OUT/c-format-files.txt" || \
		{ echo "tables-c-clean: the unit directory $dir put no generated C into the check (missing, empty, or outside the trees named)"; exit 1; }
done

# one path per line in, one NUL-ended argument out: xargs splits nothing else
tr '\n' '\000' < "$OUT/c-format-files.txt" | xargs -0 -P "$jobs" -n 16 sh -c '
	cf=$1; st=$2; wk=$3; shift 3
	out="$wk/res.$$"
	tmp="$wk/fmt.$$"
	for f in "$@"; do
		if "$cf" --style="$st" "$f" > "$tmp"; then
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
' _ "$CLANG_FORMAT" "$style" "$work" || \
	{ echo "tables-c-clean: a format worker failed, so the check is incomplete"; exit 1; }

bad=0
cat "$work"/res.* | sort -t "$tab" -k2,2 > "$OUT/c-format-results.txt"
if [ "$(wc -l < "$OUT/c-format-results.txt" | tr -d ' ')" != "$(wc -l < "$OUT/c-format-files.txt" | tr -d ' ')" ]; then
	echo "tables-c-clean: $(wc -l < "$OUT/c-format-files.txt" | tr -d ' ') files were listed and $(wc -l < "$OUT/c-format-results.txt" | tr -d ' ') came back with a verdict"; bad=1
fi
while IFS="$tab" read -r verdict f; do
	case "$verdict" in
	CLEAN)
		if grep -Fxq "$f" "$allow"; then echo "tables-c-clean: $f is clean now — remove its line from $allow"; bad=1; fi ;;
	DRIFT)
		echo "$f" >> "$seen"
		grep -Fxq "$f" "$allow" || { echo "tables-c-clean: clang-format drift in $f, which $allow does not list (the list only shrinks)"; bad=1; } ;;
	*)
		echo "tables-c-clean: $CLANG_FORMAT exited non-zero on $f, so its output is no verdict"; bad=1 ;;
	esac
done < "$OUT/c-format-results.txt"

# every ratchet line against this scan: a scanned path was judged above; one
# the scan never reached stands for nothing, whether or not it exists
grep -v '^#' "$allow" | grep . | while IFS= read -r f; do
	grep -Fxq -- "$f" "$OUT/c-format-files.txt" && continue
	if [ -f "$f" ]; then
		echo "tables-c-clean: $f is listed in $allow but lies outside the trees scanned ($*) — remove its line"
	else
		echo "tables-c-clean: $f is listed in $allow but no longer generated — remove its line"
	fi
done > "$OUT/c-format-unscanned.txt"
if [ -s "$OUT/c-format-unscanned.txt" ]; then cat "$OUT/c-format-unscanned.txt"; bad=1; fi
rm -rf "$work" "$OUT/c-format-files.txt" "$OUT/c-format-tree.txt" "$OUT/c-format-results.txt" "$OUT/c-format-unscanned.txt"
[ $bad -eq 0 ] || exit 1
echo "C: clang-format clean over $*: $checked files checked, $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
