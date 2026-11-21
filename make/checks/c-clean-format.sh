#!/bin/sh
# THE C FORMAT GATE's engine (make/c.mk tables-c-clean, docs/PORTING.md J4).
# Every generated C file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format. Files that drift ride
# the SHRINK-ONLY ratchet, a list of paths (make/c-format-allow.txt): a
# drifting file not listed fails, and a listed file that is clean now (or is no
# longer generated) fails until its line is deleted.
#
# usage: c-clean-format.sh <ratchet-file> <tree>...
# env:   CLANG_FORMAT, OUT (scratch dir), C_FORMAT_CACHE (the verdict store, below)
set -eu
allow=$1
shift
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build}
mkdir -p "$OUT"
seen=$OUT/c-format-seen.txt
: > "$seen"
style="file:$(pwd)/.clang-format"
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
work=$OUT/c-format-workers
rm -rf "$work" && mkdir -p "$work"

# THE VERDICT STORE ($C_FORMAT_CACHE, unset by default; the per-commit lane
# sets it on a fleet runner, to a directory every runner of the host shares).
# The gate is clang-format over 580 generated files, 66 MB, about 250 s of CPU
# every run (84 s of a 16-thread runner's two minutes, 2026-11-21), and almost
# every file is the one the last run formatted. A file's verdict is a function
# of its bytes, the formatter and the style file, so the store keeps it under
# exactly that: <SHA-256 of `clang-format --version` and .clang-format>/<SHA-256
# of the file>. A verdict is stored only when clang-format itself exited 0;
# a file whose bytes, formatter or style changed has no entry and is formatted.
# Verdicts not read for 30 days are dropped.
store=
if [ -n "${C_FORMAT_CACHE:-}" ] && command -v sha256sum >/dev/null 2>&1; then
	tool=$({ "$CLANG_FORMAT" --version; cat .clang-format; echo "c-clean-format v1"; } | sha256sum | cut -d' ' -f1)
	if mkdir -p "$C_FORMAT_CACHE/$tool" 2>/dev/null; then
		store=$C_FORMAT_CACHE/$tool
	fi
fi

find "$@" \( -name '*.h' -o -name '*.c' \) | sort > "$OUT/c-format-files.txt"
if [ -s "$OUT/c-format-files.txt" ]; then
	# One file per worker call: fifteen *Table.h files are almost half of the
	# formatter's time (the longest alone is a ninth of it), and neighbours
	# in the sorted list (lists/MigrateTable.h, ReportTable.h, SaveTable.h)
	# landed in one batch of sixteen and ran one after another.
	xargs -P "$jobs" -n 1 sh -c '
		cf=$1; st=$2; wk=$3; store=$4; shift 4
		out="$wk/res.$$"
		for f in "$@"; do
			v=
			if [ -n "$store" ]; then
				h=$(sha256sum "$f" | cut -d" " -f1)
				if [ -f "$store/$h" ]; then
					v=$(cat "$store/$h")
					touch "$store/$h"
					echo "$f" >> "$wk/hit.$$"
				fi
			fi
			if [ "$v" != CLEAN ] && [ "$v" != DRIFT ]; then
				if "$cf" --style="$st" "$f" > "$wk/fmt.$$"; then
					if cmp -s "$f" "$wk/fmt.$$"; then v=CLEAN; else v=DRIFT; fi
					if [ -n "$store" ]; then
						printf "%s\n" "$v" > "$store/.tmp.$$" && mv "$store/.tmp.$$" "$store/$h"
					fi
				else
					v=DRIFT
				fi
			fi
			printf "%s\t%s\n" "$v" "$f" >> "$out"
		done
		rm -f "$wk/fmt.$$"
	' _ "$CLANG_FORMAT" "$style" "$work" "$store" < "$OUT/c-format-files.txt"
fi
if [ -n "$store" ]; then
	echo "C format store: $(cat "$work"/hit.* 2>/dev/null | wc -l | tr -d ' ') of $(wc -l < "$OUT/c-format-files.txt" | tr -d ' ') verdicts were already held for these bytes, this formatter and this style"
	find "$C_FORMAT_CACHE" -type f -mtime +30 -delete 2>/dev/null || true
	find "$C_FORMAT_CACHE" -mindepth 1 -type d -empty -delete 2>/dev/null || true
fi

bad=0
if ls "$work"/res.* >/dev/null 2>&1; then
	cat "$work"/res.* | sort -k2,2 > "$OUT/c-format-results.txt"
	while IFS="$(printf '\t')" read -r verdict f; do
		if [ "$verdict" = "CLEAN" ]; then
			if grep -Fxq "$f" "$allow"; then echo "tables-c-clean: $f is clean now — remove its line from $allow"; bad=1; fi
		else
			echo "$f" >> "$seen"
			grep -Fxq "$f" "$allow" || { echo "tables-c-clean: clang-format drift in $f, which $allow does not list (the list only shrinks)"; bad=1; }
		fi
	done < "$OUT/c-format-results.txt"
fi
rm -rf "$work" "$OUT/c-format-files.txt" "$OUT/c-format-results.txt"

for f in $(grep -v '^#' "$allow" | grep . || true); do
	[ -f "$f" ] || { echo "tables-c-clean: $f is listed in $allow but no longer generated — remove its line"; bad=1; }
done
[ $bad -eq 0 ] || exit 1
echo "C: clang-format clean over $*, $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
