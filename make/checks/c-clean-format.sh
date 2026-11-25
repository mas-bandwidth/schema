#!/bin/sh
# THE C FORMAT GATE's engine (make/c.mk tables-c-clean, docs/PORTING.md J4).
# Every generated C file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format. Files that drift ride
# the SHRINK-ONLY ratchet, a list of paths (make/c-format-allow.txt): a
# drifting file not listed fails, and a listed file that is clean now (or is no
# longer generated) fails until its line is deleted.
#
# usage: c-clean-format.sh <ratchet-file> <tree>...
# env:   CLANG_FORMAT, OUT (scratch dir), SHARD (K/N: judge only the files
#        make/checks/shard-filter.sh puts in shard K of N, and only their
#        ratchet lines; empty: every file)
set -eu -o pipefail
allow=$1
[ -r "$allow" ] || { echo "tables-c-clean: the ratchet $allow cannot be read" >&2; exit 1; }
shift
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build}
mkdir -p "$OUT"
here=$(cd "$(dirname "$0")" && pwd)
shard=${SHARD-}
held=$OUT/c-format-held.txt
seen=$OUT/c-format-seen.txt
: > "$seen"
style="file:$(pwd)/.clang-format"
# loud-ok: a worker count, not a verdict: nproc on Linux, sysctl on macOS, 4 where neither answers
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
work=$OUT/c-format-workers
rm -rf "$work" && mkdir -p "$work"

# THE GATE PROVES IT RAN. A tree that is not there, or trees that hold no C at
# all, used to leave the file list empty, skip every check below and print
# "clean": a gate that looked at nothing. Both are failures by name.
for t in "$@"; do
	[ -d "$t" ] || { echo "tables-c-clean: $t is not a directory — the gate was pointed at a tree that is not generated"; exit 1; }
done
# find's own status is read (a pipe would hand back sort's), and a tree with
# no .h or .c is a failure: the gate would otherwise print "clean" over nothing.
if ! find "$@" \( -name '*.h' -o -name '*.c' \) > "$OUT/c-format-found.txt"; then
	echo "tables-c-clean: cannot list $*" >&2
	exit 1
fi
# the whole gate's files are the universe every shard assigns from, and
# the ratchet lines this run holds are its shard's (all of them, whole)
sort "$OUT/c-format-found.txt" > "$OUT/c-format-all.txt"
sh "$here/shard-filter.sh" "$shard" "$OUT/c-format-all.txt" < "$OUT/c-format-all.txt" > "$OUT/c-format-files.txt"
sed -e '/^#/d' -e '/^$/d' "$allow" | sh "$here/shard-filter.sh" "$shard" "$OUT/c-format-all.txt" > "$held"
rm -f "$OUT/c-format-found.txt"
if [ ! -s "$OUT/c-format-files.txt" ]; then
	echo "tables-c-clean: no .h or .c file under $*; nothing was held to clang-format" >&2
	exit 1
fi
checked=$(wc -l < "$OUT/c-format-files.txt" | tr -d ' ')
# one file per worker call: the files run from a few hundred bytes to 5 MB
# (clang-format 0.01 s to 69 s), and a batch of sixteen left one worker
# holding the big ones while the rest sat idle (93 s of wall for 34 s of
# even share on amd-2, 2026-11-25)
if [ -s "$OUT/c-format-files.txt" ]; then
	xargs -P "$jobs" -n 1 sh -c '
		cf=$1; st=$2; wk=$3; shift 3
		out="$wk/res.$$"
		for f in "$@"; do
			if "$cf" --style="$st" "$f" | cmp -s "$f" -; then
				printf "CLEAN\t%s\n" "$f" >> "$out"
			else
				printf "DRIFT\t%s\n" "$f" >> "$out"
					fi
		done
	' _ "$CLANG_FORMAT" "$style" "$work" < "$OUT/c-format-files.txt"
fi

bad=0
: > "$OUT/c-format-results.txt"
if ls "$work"/res.* >/dev/null 2>&1; then
	cat "$work"/res.* | sort -k2,2 > "$OUT/c-format-results.txt"
fi
# every file found has exactly one verdict: a worker that died, or wrote
# nothing, is a file nobody judged
found=$(wc -l < "$OUT/c-format-files.txt" | tr -d ' ')
judged=$(wc -l < "$OUT/c-format-results.txt" | tr -d ' ')
if [ "$found" != "$judged" ]; then
	echo "tables-c-clean: found $found generated C files and judged $judged — a file without a verdict is not a clean file"
	bad=1
fi
if [ -s "$OUT/c-format-results.txt" ]; then
	while IFS="$(printf '\t')" read -r verdict f; do
		if [ "$verdict" = "CLEAN" ]; then
			if grep -Fxq "$f" "$held"; then echo "tables-c-clean: $f is clean now — remove its line from $allow"; bad=1; fi
		else
			echo "$f" >> "$seen"
			grep -Fxq "$f" "$held" || { echo "tables-c-clean: clang-format drift in $f, which $allow does not list (the list only shrinks)"; bad=1; }
		fi
	done < "$OUT/c-format-results.txt"
fi
# every listed file has a verdict: a worker that died, or clang-format missing
# from every worker, leaves fewer verdicts than files
judged=0
[ -f "$OUT/c-format-results.txt" ] && judged=$(wc -l < "$OUT/c-format-results.txt" | tr -d ' ')
if [ "$judged" -ne "$checked" ]; then
	echo "tables-c-clean: $judged verdicts for $checked files; the format check did not finish" >&2
	bad=1
fi
for f in $(cat "$held"); do
	if [ ! -f "$f" ]; then
		echo "tables-c-clean: $f is listed in $allow but no longer generated — remove its line"; bad=1
	elif ! grep -Fxq "$f" "$OUT/c-format-files.txt"; then
		# listed, on disk, and outside every tree this run judged: the line
		# excuses a file the gate never looked at
		echo "tables-c-clean: $f is listed in $allow but is under none of the trees this gate scans ($*) — remove its line, or scan its tree"; bad=1
	fi
done
rm -rf "$work" "$OUT/c-format-files.txt" "$OUT/c-format-results.txt" "$OUT/c-format-all.txt" "$held"
[ $bad -eq 0 ] || exit 1
echo "C: clang-format clean over $*${shard:+ (shard $shard)}: $checked files checked, $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
