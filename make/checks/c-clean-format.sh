#!/bin/sh
# THE C FORMAT GATE's engine (make/c.mk tables-c-clean, docs/PORTING.md J4).
# Every generated C file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format. Files that drift ride
# the SHRINK-ONLY ratchet, a list of paths (make/c-format-allow.txt): a
# drifting file not listed fails, and a listed file that is clean now (or is no
# longer generated) fails until its line is deleted.
#
# usage: c-clean-format.sh <ratchet-file> <tree>...
# env:   CLANG_FORMAT, OUT (scratch dir), C_FORMAT_SHARD (K/N, below)
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

find "$@" \( -name '*.h' -o -name '*.c' \) | sort > "$OUT/c-format-files.txt"
# THE SHARD (C_FORMAT_SHARD=K/N, unset by default: every file). The per-commit
# lane (ci-fast.yml lang-clean, the c rows) runs the gate as N rows side by
# side, each formatting the files make/shard.sh deals it from the sorted list:
# the gate is about 250 s of clang-format CPU over 580 files, past what one
# runner gives a row in two minutes, and no row keeps a verdict between runs.
# A ratchet line is judged in the row that formats its file; the "no longer
# generated" check reads the tree and runs in every row.
shard=
if [ -n "${C_FORMAT_SHARD:-}" ]; then
	mv "$OUT/c-format-files.txt" "$OUT/c-format-all.txt"
	sh "$(dirname "$0")/../shard.sh" "$C_FORMAT_SHARD" < "$OUT/c-format-all.txt" > "$OUT/c-format-files.txt" || {
		echo "tables-c-clean: C_FORMAT_SHARD=$C_FORMAT_SHARD is not a shard"
		exit 1
	}
	shard=" (shard $C_FORMAT_SHARD: $(wc -l < "$OUT/c-format-files.txt" | tr -d ' ') of $(wc -l < "$OUT/c-format-all.txt" | tr -d ' ') files)"
	rm -f "$OUT/c-format-all.txt"
fi
if [ -s "$OUT/c-format-files.txt" ]; then
	xargs -P "$jobs" -n 16 sh -c '
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
echo "C: clang-format clean over $*$shard, $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
