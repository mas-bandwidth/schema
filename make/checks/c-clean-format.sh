#!/usr/bin/env bash
# THE C FORMAT GATE's engine (make/c.mk tables-c-clean, docs/PORTING.md J4).
# Every generated C file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format. Files that drift ride
# the SHRINK-ONLY ratchet, a list of paths (make/c-format-allow.txt): a
# drifting file not listed fails, and a listed file that is clean now (or is no
# longer generated) fails until its line is deleted.
#
# usage: c-clean-format.sh <ratchet-file> <tree>...
# env:   CLANG_FORMAT, OUT (scratch dir)
set -eu -o pipefail
allow=$1
[ -r "$allow" ] || { echo "tables-c-clean: the ratchet $allow cannot be read" >&2; exit 1; }
shift
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build}
mkdir -p "$OUT"
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
if ! find "$@" \( -name '*.h' -o -name '*.c' \) -print0 > "$work/found"; then
	echo "tables-c-clean: cannot list $*" >&2
	exit 1
fi
# The manifest and xargs use simple generated paths. Validate before
# translating NULs to newlines so a newline in a name cannot split it.
if [ "$(LC_ALL=C tr -d 'A-Za-z0-9_./+\000-' < "$work/found" | wc -c)" -ne 0 ]; then
	echo "tables-c-clean: unsupported path in generated C file list" >&2
	exit 1
fi
tr '\000' '\n' < "$work/found" | LC_ALL=C sort -u > "$OUT/c-format-files.txt"
if [ ! -s "$OUT/c-format-files.txt" ]; then
	echo "tables-c-clean: no .h or .c file under $*; nothing was held to clang-format" >&2
	exit 1
fi
checked=$(wc -l < "$OUT/c-format-files.txt" | tr -d ' ')
# The explicit style is shared by every path. Keep the basename in the key
# because clang-format uses the input name to select the language.
if command -v sha256sum >/dev/null 2>&1; then
	xargs sha256sum -- < "$OUT/c-format-files.txt" > "$work/hashes"
elif command -v shasum >/dev/null 2>&1; then
	xargs shasum -a 256 -- < "$OUT/c-format-files.txt" > "$work/hashes"
else
	# Preserve the gate on hosts without a SHA-256 tool.
	awk '{ print NR, $0 }' "$OUT/c-format-files.txt" > "$work/hashes"
fi
awk '{
	path = $2
	n = split(path, parts, "/")
	print $1 "/" parts[n], path
}' "$work/hashes" > "$work/keys"

# Schedule one file per worker, with the largest inputs first so an expensive
# file does not wait behind a batch of small ones.
awk '!seen[$1]++ { print $2 }' "$work/keys" | xargs wc -c -- |
	awk '$2 != "total" { print $1, $2 }' |
	LC_ALL=C sort -k1,1nr -k2,2 | awk '{ print $2 }' > "$work/order"
xargs -P "$jobs" -n 1 sh -c '
	set -eu
	cf=$1; st=$2; wk=$3; f=$4
	if ! "$cf" --style="$st" "$f" > "$wk/formatted.$$"; then
		echo "tables-c-clean: clang-format failed on $f" >&2
		exit 1
	fi
	if cmp -s "$f" "$wk/formatted.$$"; then
		printf "CLEAN %s\n" "$f" > "$wk/res.$$"
	else
		status=$?
		[ "$status" -eq 1 ] || exit "$status"
		printf "DRIFT %s\n" "$f" > "$wk/res.$$"
	fi
	rm "$wk/formatted.$$"
' _ "$CLANG_FORMAT" "$style" "$work" < "$work/order"

bad=0
# Emit a verdict for every path so each still gets its own ratchet check.
cat "$work"/res.* | awk '
	FNR == NR { key[$2] = $1; next }
	$2 in key { verdict[key[$2]] = $1 }
	END {
		for (path in key) {
			k = key[path]
			printf "%s\t%s\n", k in verdict ? verdict[k] : "NONE", path
		}
	}
' "$work/keys" - | LC_ALL=C sort -k2,2 > "$OUT/c-format-results.txt"
if [ -s "$OUT/c-format-results.txt" ]; then
	while IFS="$(printf '\t')" read -r verdict f; do
		if [ "$verdict" = "CLEAN" ]; then
			if grep -Fxq "$f" "$allow"; then echo "tables-c-clean: $f is clean now — remove its line from $allow"; bad=1; fi
		elif [ "$verdict" = "DRIFT" ]; then
			echo "$f" >> "$seen"
			grep -Fxq "$f" "$allow" || { echo "tables-c-clean: clang-format drift in $f, which $allow does not list (the list only shrinks)"; bad=1; }
		else
			echo "tables-c-clean: no verdict for $f" >&2
			bad=1
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
for f in $(sed -e '/^#/d' -e '/^$/d' "$allow"); do
	if [ ! -f "$f" ]; then
		echo "tables-c-clean: $f is listed in $allow but no longer generated — remove its line"; bad=1
	elif ! grep -Fxq "$f" "$OUT/c-format-files.txt"; then
		# listed, on disk, and outside every tree this run judged: the line
		# excuses a file the gate never looked at
		echo "tables-c-clean: $f is listed in $allow but is under none of the trees this gate scans ($*) — remove its line, or scan its tree"; bad=1
	fi
done
calls=$(wc -l < "$work/order" | tr -d ' ')
rm -rf "$work" "$OUT/c-format-files.txt" "$OUT/c-format-results.txt"
[ $bad -eq 0 ] || exit 1
echo "C: clang-format clean over $*: $checked files checked ($calls formatter calls), $(wc -l < "$seen" | tr -d ' ') table-emitter files still on the ratchet"
