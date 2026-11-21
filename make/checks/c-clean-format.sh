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

# ONE FORMAT PER DISTINCT (CONTENT, NAME). Every table unit emits its own copy
# of the same runtime header, so much of the corpus is the same bytes under the
# same base name in another directory. clang-format's answer is a function of
# the bytes, the style (one file, named absolutely here) and the file NAME, from
# which it reads the language (a .c and a .h of one content can differ: the
# same-bytes control, tables-c-clean-same-bytes-control). So the key is the
# content hash AND the base name: a path whose key was already formatted IN THIS
# RUN gets that format's verdict, because it is the same question; nothing is
# kept between runs. Every path still gets its own verdict line and its own
# ratchet check below, and a path left without a verdict is red.
#
# LARGEST FIRST, ONE PER WORKER. The distinct files go to the workers in
# descending size, one file per worker, so the longest format starts at once
# instead of queueing behind fifteen others in a batch of sixteen.
if command -v sha256sum >/dev/null 2>&1; then
	hash="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	hash="shasum -a 256"
else
	hash=""
fi
: > "$OUT/c-format-order.txt"
if [ -s "$OUT/c-format-files.txt" ]; then
	# <key> <path>, one line per file; with no sha256 tool the key is the
	# path itself, and every file is formatted
	if [ -n "$hash" ]; then
		# shellcheck disable=SC2086
		xargs $hash < "$OUT/c-format-files.txt" \
			| awk '{ p = $2; sub(/^\*/, "", p); n = split(p, s, "/"); print $1 "/" s[n] " " p }' \
			| sort -k2,2 > "$OUT/c-format-keys.txt"
	else
		awk '{ print $1 " " $1 }' "$OUT/c-format-files.txt" > "$OUT/c-format-keys.txt"
	fi
	sort -u -k1,1 "$OUT/c-format-keys.txt" | awk '{ print $2 }' > "$OUT/c-format-reps.txt"
	xargs wc -c < "$OUT/c-format-reps.txt" | awk '$2 != "total" { print $1 " " $2 }' \
		| sort -k1,1nr | awk '{ print $2 }' > "$OUT/c-format-order.txt"
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
	' _ "$CLANG_FORMAT" "$style" "$work" < "$OUT/c-format-order.txt"
fi

bad=0
if [ -s "$OUT/c-format-files.txt" ]; then
	# the formatted file's verdict, carried to every path that shares its key
	cat "$work"/res.* 2>/dev/null | awk -F '\t' '
		NR == FNR { split($0, kp, " "); key[kp[2]] = kp[1]; next }
		{ verdict[key[$2]] = $1 }
		END { for (p in key) printf "%s\t%s\n", (key[p] in verdict) ? verdict[key[p]] : "NONE", p }
	' "$OUT/c-format-keys.txt" - | sort -k2,2 > "$OUT/c-format-results.txt"
	while IFS="$(printf '\t')" read -r verdict f; do
		if [ "$verdict" = "CLEAN" ]; then
			if grep -Fxq "$f" "$allow"; then echo "tables-c-clean: $f is clean now — remove its line from $allow"; bad=1; fi
		elif [ "$verdict" = "DRIFT" ]; then
			echo "$f" >> "$seen"
			grep -Fxq "$f" "$allow" || { echo "tables-c-clean: clang-format drift in $f, which $allow does not list (the list only shrinks)"; bad=1; }
		else
			echo "tables-c-clean: no verdict for $f (the file sharing its content and name was never formatted)"; bad=1
		fi
	done < "$OUT/c-format-results.txt"
	[ "$(wc -l < "$OUT/c-format-results.txt")" -eq "$(wc -l < "$OUT/c-format-files.txt")" ] || {
		echo "tables-c-clean: $(wc -l < "$OUT/c-format-results.txt" | tr -d ' ') verdicts for $(wc -l < "$OUT/c-format-files.txt" | tr -d ' ') files"; bad=1; }
	echo "tables-c-clean: $(wc -l < "$OUT/c-format-files.txt" | tr -d ' ') files, $(wc -l < "$OUT/c-format-order.txt" | tr -d ' ') distinct by content and name, each formatted once"
fi
rm -rf "$work" "$OUT/c-format-files.txt" "$OUT/c-format-results.txt" "$OUT/c-format-keys.txt" "$OUT/c-format-reps.txt" "$OUT/c-format-order.txt"

for f in $(grep -v '^#' "$allow" | grep . || true); do
	[ -f "$f" ] || { echo "tables-c-clean: $f is listed in $allow but no longer generated — remove its line"; bad=1; }
done
[ $bad -eq 0 ] || exit 1
echo "C: clang-format clean over $*$shard, $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
