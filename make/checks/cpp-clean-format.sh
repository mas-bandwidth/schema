#!/bin/sh
# THE C++ FORMAT GATE's engine (make/cpp.mk tables-cpp-clean, PORTING.md J4).
# Every generated C++ file under the trees named on the command line must be
# what clang-format writes in the repo's .clang-format (the house C and C++
# style). Files that drift ride the SHRINK-ONLY ratchet, a list of paths
# (make/cpp-format-allow.txt): a drifting file not listed fails, and a listed
# file that is clean now (or is no longer generated) fails until its line is
# deleted. Files are checked in parallel; nothing here shares state.
#
# ONE FILE, FILE_SECONDS OF CPU AT MOST. clang-format runs under `ulimit -t
# $FILE_SECONDS` (CLANG_FORMAT_FILE_SECONDS, make/c.mk, where the measured
# times are recorded), and a file it cannot finish inside that is red by name
# whatever its drift, in either mode: the cure is the emitter's shape, never
# the number.
#
# usage: cpp-clean-format.sh <ratchet-file> <census|check> <tree>...
# env:   CLANG_FORMAT, FILE_SECONDS (required), JOBS, OUT (scratch dir), SHARD
#        (K/N: judge only the files make/checks/shard-filter.sh puts in shard
#        K of N, and only their ratchet lines; empty: every file)
set -eu -o pipefail
ratchet=$1
mode=$2
shift 2
case ${FILE_SECONDS-} in
'' | *[!0-9]* | 0) echo "tables-cpp-clean: FILE_SECONDS must name the per-file CPU limit in whole seconds (CLANG_FORMAT_FILE_SECONDS, make/c.mk); it is '${FILE_SECONDS-}'" >&2; exit 1 ;;
esac
CLANG_FORMAT=${CLANG_FORMAT:-clang-format}
OUT=${OUT:-build/cpp-clean}
JOBS=${JOBS:-$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)}
here=$(cd "$(dirname "$0")" && pwd)
style="file:$(pwd)/.clang-format"
FMT_TMP=$OUT/format
export CLANG_FORMAT style FILE_SECONDS FMT_TMP

mkdir -p "$OUT/format"
shard=${SHARD-}
# the whole gate's files are the universe every shard assigns from
find "$@" \( -name '*.h' -o -name '*.cpp' \) | sort > "$OUT/format/all.txt"
sh "$here/shard-filter.sh" "$shard" "$OUT/format/all.txt" < "$OUT/format/all.txt" > "$OUT/format/files.txt"
[ -s "$OUT/format/files.txt" ] || { echo "tables-cpp-clean: no generated C++ under $*"; exit 1; }

# one line per file that is not clean: DRIFT, SLOW (killed by the CPU
# limit: SIGXCPU at the soft limit, 152; SIGKILL at the hard one, 137, which
# `ulimit -t` sets to the same number) or FAILED (any other formatter
# failure, another signal included);
# the formatter's own status is read apart from the comparison
xargs -P "$JOBS" -n 1 sh -c '
	tmp="$FMT_TMP/fmt.$$"
	rc=0
	( ulimit -c 0; ulimit -t "$FILE_SECONDS"; exec "$CLANG_FORMAT" --style="$style" "$1" ) > "$tmp" || rc=$?
	if [ "$rc" -eq 152 ] || [ "$rc" -eq 137 ]; then
		echo "SLOW $1"
	elif [ "$rc" -ne 0 ]; then
		echo "FAILED $1"
	elif ! cmp -s "$1" "$tmp"; then
		echo "DRIFT $1"
	fi
	rm -f "$tmp"
' sh < "$OUT/format/files.txt" | sort > "$OUT/format/verdicts.txt"
sed -n 's/^DRIFT //p' "$OUT/format/verdicts.txt" > "$OUT/format/drift.txt"
bad=0
while read -r verdict f; do
	case $verdict in
	SLOW) echo "tables-cpp-clean: clang-format took more than $FILE_SECONDS s of CPU on $f (CLANG_FORMAT_FILE_SECONDS, make/c.mk) — change the emitter's shape, never the limit"; bad=1 ;;
	FAILED) echo "tables-cpp-clean: clang-format failed on $f"; bad=1 ;;
	esac
done < "$OUT/format/verdicts.txt"

if [ "$mode" = census ]; then cat "$OUT/format/drift.txt"; exit $bad; fi

sed -e '/^#/d' -e '/^$/d' "$ratchet" | sh "$here/shard-filter.sh" "$shard" "$OUT/format/all.txt" | sort > "$OUT/format/allowed.txt"
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
echo "C++: clang-format clean over $*${shard:+ (shard $shard)}: $(wc -l < "$OUT/format/files.txt" | tr -d ' ') files checked (each inside $FILE_SECONDS s of CPU), $(wc -l < "$OUT/format/drift.txt" | tr -d ' ') table-emitter files still on the ratchet"
