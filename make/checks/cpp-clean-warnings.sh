#!/bin/sh
# THE C++ WARNING GATE's engine (make/cpp.mk tables-cpp-clean, PORTING.md J4).
#
# Every generated C++ file under the trees named on the command line is
# compiled by clang++ with -Weverything minus the c++98-compat family and the unsafe-buffer-usage pair (the
# generated code is C++17 by design, so a warning that it is not C++98 is not
# a finding). Headers are compiled as the one-line translation unit a consumer
# writes, `#include "X.h"`, so a header is judged as its consumers see it (a
# header compiled as the main file would also report its own include-guard
# macros as unused). The runtime, serialize, is -isystem: its own text is the
# runtime repo's, and what its macros leave in a generated body is still
# reported where the macro is expanded.
#
# A diagnostic is attributed to the generated file it points into, and each
# file's observed set of warning kinds is compared with the RATCHET, a file of
# `<path> <kind>[,<kind>...]` lines (make/cpp-warn-allow.txt). A kind observed
# in a file the ratchet does not list for that file fails; a listed kind that
# no longer occurs fails until its line is edited. The ratchet only shrinks.
#
# usage: cpp-clean-warnings.sh <ratchet-file> <census|check> <tree>...
# env:   CLANGXX, SERIALIZE_DIR, JOBS, OUT (scratch dir)
set -eu
ratchet=$1
mode=$2
shift 2
CLANGXX=${CLANGXX:-clang++}
OUT=${OUT:-build/cpp-clean}
JOBS=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}
: "${SERIALIZE_DIR:?SERIALIZE_DIR names the serialize checkout}"

rm -rf "$OUT" && mkdir -p "$OUT/logs"
here=$(cd "$(dirname "$0")" && pwd)

# every directory of the table corpus is an include root, as the table legs'
# own builds name them (make tables_includes)
incs=
for t in "$@"; do
	for d in $(find "$t" -type d | sort); do incs="$incs -I$d"; done
done
incs="$incs -Itest/tables"
export CLANGXX SERIALIZE_DIR OUT incs

find "$@" \( -name '*.h' -o -name '*.cpp' \) | sort > "$OUT/files.txt"
if [ ! -s "$OUT/files.txt" ]; then echo "tables-cpp-clean: no generated C++ under $*"; exit 1; fi

# two families are dropped, each for a stated reason; CPATH is unset so the
# -isystem order below is what the compiler sees on every host:
#   c++98-compat      the code is C++17 by design.
#   unsafe-buffer-usage, unsafe-buffer-usage-in-libc-call
#                     clang 20+'s opt-in "Safe Buffers" model, which refuses every
#                     raw-pointer subscript and every memcpy and wants std::span.
#                     The emitted C++ is C-like on purpose (pointer, length, loop;
#                     serialize.h is written the same way), so the warning is
#                     the style the code deliberately does not have, not a
#                     defect in it. Older clang does not know either kind, so
#                     leaving them in would make the ratchet depend on the
#                     version of the host's clang.
export WFLAGS="-std=c++17 -fsyntax-only -fno-caret-diagnostics -Weverything -Wno-c++98-compat -Wno-c++98-compat-pedantic -Wno-unsafe-buffer-usage -Wno-unsafe-buffer-usage-in-libc-call"
export CPATH=

xargs -P "$JOBS" -n 1 sh "$here/cpp-clean-one.sh" < "$OUT/files.txt"

# <path> <kind> for every distinct finding that lands in a generated file
cat "$OUT"/logs/* | awk '
	/: warning: / && match($0, /\[-W[A-Za-z0-9+_-]+\]$/) {
		path = $0; sub(/:[0-9]+:[0-9]+: warning: .*/, "", path)
		if (path ~ /^(build|generated)\//) print path, substr($0, RSTART + 3, RLENGTH - 4)
	}' | sort -u > "$OUT/found.txt"

# fold to one `<path> kind,kind` line per file
awk '{ k[$1] = ($1 in k) ? k[$1] "," $2 : $2 } END { for (p in k) print p, k[p] }' "$OUT/found.txt" | sort > "$OUT/observed.txt"

if [ "$mode" = census ]; then
	cat "$OUT/observed.txt"
	exit 0
fi

# the ratchet, kinds sorted per line so the comparison is order-free
norm() { awk '!/^#/ && NF { n = split($2, a, ","); for (i = 1; i <= n; i++) print $1, a[i] }' "$1" | sort -u; }
norm "$ratchet" > "$OUT/allowed.txt"
bad=0
# observed and not allowed
comm -13 "$OUT/allowed.txt" "$OUT/found.txt" > "$OUT/new.txt"
if [ -s "$OUT/new.txt" ]; then
	echo "tables-cpp-clean: clang++ -Weverything findings the ratchet ($ratchet) does not list (the list only shrinks; fix the emitter):"
	sed 's/^/  /' "$OUT/new.txt" | head -60
	n=$(wc -l < "$OUT/new.txt" | tr -d ' ')
	[ "$n" -le 60 ] || echo "  ... and $((n - 60)) more (the full list is $OUT/new.txt)"
	bad=1
fi
# allowed and no longer observed (or the file is gone). A kind this compiler
# does not know (a warning a newer clang added) cannot be observed by it, so
# its entries are not held against a compiler that lacks it; a kind the
# compiler knows and does not report is a line to delete.
comm -23 "$OUT/allowed.txt" "$OUT/found.txt" > "$OUT/gone.all"
: > "$OUT/gone.txt"
for k in $(awk '{print $2}' "$OUT/gone.all" | sort -u); do
	if printf '' | $CLANGXX -std=c++17 -fsyntax-only -W"$k" -x c++ - 2>&1 | grep -q 'unknown warning option'; then
		echo "tables-cpp-clean: note: $CLANGXX does not know -W$k, so its ratchet entries are not checked here" >&2
	else
		awk -v k="$k" '$2 == k' "$OUT/gone.all" >> "$OUT/gone.txt"
	fi
done
sort -o "$OUT/gone.txt" "$OUT/gone.txt"
if [ -s "$OUT/gone.txt" ]; then
	echo "tables-cpp-clean: ratchet entries that no longer occur — delete them from $ratchet:"
	sed 's/^/  /' "$OUT/gone.txt" | head -60
	n=$(wc -l < "$OUT/gone.txt" | tr -d ' ')
	[ "$n" -le 60 ] || echo "  ... and $((n - 60)) more (the full list is $OUT/gone.txt)"
	bad=1
fi
[ $bad -eq 0 ] || exit 1
echo "C++: clang++ -Weverything (less the c++98-compat family) over $* : $(wc -l < "$OUT/files.txt" | tr -d ' ') files, $(wc -l < "$OUT/observed.txt" | tr -d ' ') still on the ratchet"
