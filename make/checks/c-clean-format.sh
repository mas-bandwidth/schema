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

# ONE FORMAT PER DISTINCT (CONTENT, NAME). clang-format's answer is a function
# of the bytes, the style (one file, named absolutely above) and the file NAME,
# from which it reads the language (a .c and a .h of one content can differ:
# tables-c-clean-same-bytes-control). So each file's key is its content hash AND
# its base name, one file per key is formatted, and every path with that key
# gets that verdict IN THIS RUN, because it is the same question; nothing is
# kept between runs. Every path still gets its own verdict line and its own
# ratchet check below, and a path left without a verdict is red. With no sha256
# tool the key is the path, and every file is formatted.
if command -v sha256sum >/dev/null 2>&1; then
	hash="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	hash="shasum -a 256"
else
	hash=""
fi
find "$@" \( -name '*.h' -o -name '*.c' \) | sort > "$OUT/c-format-all.txt"
: > "$OUT/c-format-keys-all.txt"
: > "$OUT/c-format-order-all.txt"
if [ -s "$OUT/c-format-all.txt" ]; then
	# <key> <path>, one line per file, in path order
	if [ -n "$hash" ]; then
		# shellcheck disable=SC2086
		xargs $hash < "$OUT/c-format-all.txt" \
			| awk '{ p = $2; sub(/^\*/, "", p); n = split(p, s, "/"); print $1 "/" s[n] " " p }' \
			| sort -k2,2 > "$OUT/c-format-keys-all.txt"
	else
		awk '{ print $1 " " $1 }' "$OUT/c-format-all.txt" > "$OUT/c-format-keys-all.txt"
	fi
	# one file per key (its first path in byte order), LARGEST FIRST (ties in
	# byte order of path): the order the workers take them in and the order
	# the shards are dealt from. LC_ALL=C, so every row computes the same list
	# whatever its locale, and shard.sh deals each key to exactly one row.
	LC_ALL=C sort -k1,1 -k2,2 "$OUT/c-format-keys-all.txt" | awk '!seen[$1]++ { print $2 }' \
		| xargs wc -c | awk '$2 != "total" { print $1 " " $2 }' \
		| LC_ALL=C sort -k1,1nr -k2,2 | awk '{ print $2 }' > "$OUT/c-format-order-all.txt"
fi

# THE SHARD (C_FORMAT_SHARD=K/N, unset by default: every file). The per-commit
# lane (ci-fast.yml lang-clean, the c rows) runs the gate as N rows side by
# side: the gate is about 250 s of clang-format CPU over 580 files, past what
# one runner gives a row in two minutes, and no row keeps a verdict between
# runs. make/shard.sh deals each row its share of the distinct files above,
# largest first and SNAKED (rows 1..N, then N..1, then 1..N), so no row takes
# the largest of every round and the rows carry about the same work. Dealt
# from the name-sorted file list, the three rows' formats took 31, 51 and 59 s
# on four workers (per-file clang-format times measured on a loaded bench);
# dealt this way they take 33, 30 and 31 s, against a floor of 30 s, the one
# slowest file. A row judges every path whose key it formats: a ratchet line is
# judged in the row that formats its file's key; the "no longer generated"
# check reads the tree and runs in every row.
shard=
if [ -n "${C_FORMAT_SHARD:-}" ]; then
	n=${C_FORMAT_SHARD#*/}
	case "$n" in
	'' | *[!0-9]* | 0*) cp "$OUT/c-format-order-all.txt" "$OUT/c-format-snake.txt" ;; # shard.sh refuses it below
	*) awk -v n="$n" '{ a[NR] = $0 } END {
		for (lo = 1; lo <= NR; lo += n) {
			hi = lo + n - 1; if (hi > NR) hi = NR
			if (((lo - 1) / n) % 2 == 0) { for (i = lo; i <= hi; i++) print a[i] }
			else { for (i = hi; i >= lo; i--) print a[i] }
		}
	}' "$OUT/c-format-order-all.txt" > "$OUT/c-format-snake.txt" ;;
	esac
	sh "$(dirname "$0")/../shard.sh" "$C_FORMAT_SHARD" < "$OUT/c-format-snake.txt" > "$OUT/c-format-order.txt" || {
		echo "tables-c-clean: C_FORMAT_SHARD=$C_FORMAT_SHARD is not a shard"
		exit 1
	}
else
	cp "$OUT/c-format-order-all.txt" "$OUT/c-format-order.txt"
fi
# this run's paths: every path whose key is a key this run formats
awk 'NR == FNR { o[$1] = 1; next } ($2 in o) { print $1 }' "$OUT/c-format-order.txt" "$OUT/c-format-keys-all.txt" > "$OUT/c-format-run-keys.txt"
awk 'NR == FNR { k[$1] = 1; next } ($1 in k)' "$OUT/c-format-run-keys.txt" "$OUT/c-format-keys-all.txt" > "$OUT/c-format-keys.txt"
awk '{ print $2 }' "$OUT/c-format-keys.txt" > "$OUT/c-format-files.txt"
if [ -n "${C_FORMAT_SHARD:-}" ]; then
	shard=" (shard $C_FORMAT_SHARD: $(wc -l < "$OUT/c-format-files.txt" | tr -d ' ') of $(wc -l < "$OUT/c-format-all.txt" | tr -d ' ') files)"
fi

# ONE FILE PER WORKER, in the order above, so the longest format starts at once
# instead of queueing behind fifteen others in a batch of sixteen.
if [ -s "$OUT/c-format-order.txt" ]; then
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
rm -rf "$work" "$OUT/c-format-all.txt" "$OUT/c-format-keys-all.txt" "$OUT/c-format-order-all.txt" "$OUT/c-format-snake.txt" "$OUT/c-format-run-keys.txt" \
	"$OUT/c-format-keys.txt" "$OUT/c-format-order.txt" "$OUT/c-format-files.txt" "$OUT/c-format-results.txt"

for f in $(grep -v '^#' "$allow" | grep . || true); do
	[ -f "$f" ] || { echo "tables-c-clean: $f is listed in $allow but no longer generated — remove its line"; bad=1; }
done
[ $bad -eq 0 ] || exit 1
echo "C: clang-format clean over $*$shard, $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
