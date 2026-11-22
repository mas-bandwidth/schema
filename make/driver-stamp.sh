#!/bin/sh
# THE DRIVER BUILD'S CONTENT STAMP: a build whose inputs the build tool cannot
# see is decided by what the inputs CONTAIN, never by what their timestamps say.
#
# `dotnet build` (and cargo, and make) compare modification times. A conformance
# driver is compiled from sources generated OUTSIDE its project (build/tables-
# generated-cs, the sibling ../serialize.cs checkout), and a timestamp compare
# against those misses a regenerated file whose mtime is not newer than the
# built assembly: a cache restore that stands everything "newer", a copy that
# keeps the old time, a revert that lands in the same tick. The driver is then
# the OLD code and a test passes against it. This wrapper hashes every input
# file's path and bytes, and:
#
#   - stamp matches and the output exists: the driver is current, nothing runs
#     (not even the build tool: a restored driver costs one hash, 0.04 s);
#   - otherwise: the command runs (it must be a FULL build: `--no-incremental`
#     for dotnet), and the stamp is written only when it succeeded.
#
# usage: driver-stamp.sh <stamp-file> <output-file> <input>... -- <command...>
#   <input> is a file or a directory (every file under it, except a .stamp).
#   The command string itself is part of the hash, so a changed flag rebuilds.
#   `driver-stamp.sh --hash <input>...` prints the hash alone.
#
# Paths hold no whitespace (they are repo-relative build paths).
set -e

if [ "$1" = --hash ]; then
	shift
	mode=hash
else
	[ $# -ge 4 ] || { echo "usage: driver-stamp.sh <stamp> <output> <input>... -- <command...>" >&2; exit 2; }
	stamp=$1
	out=$2
	shift 2
	mode=build
fi

inputs=
while [ $# -gt 0 ] && [ "$1" != -- ]; do
	inputs="$inputs $1"
	shift
done
if [ "$mode" = build ]; then
	[ "$1" = -- ] || { echo "driver-stamp: no -- before the command" >&2; exit 2; }
	shift
	cmd=$*
else
	cmd=
fi

if command -v sha256sum >/dev/null 2>&1; then
	sum() { sha256sum; }
	sumfile() { xargs sha256sum; }
else
	sum() { shasum -a 256; }
	sumfile() { xargs shasum -a 256; }
fi

hash_inputs() {
	{
		printf 'command %s\n' "$cmd"
		for p in $inputs; do
			if [ -d "$p" ]; then
				find "$p" -type f ! -name .stamp
			elif [ -f "$p" ]; then
				printf '%s\n' "$p"
			else
				# a missing input is part of the hash: it appearing rebuilds
				printf 'missing %s\n' "$p" >&2
			fi
		done | LC_ALL=C sort | sumfile
	} | sum | cut -d' ' -f1
}

now=$(hash_inputs 2>/dev/null)

if [ "$mode" = hash ]; then
	echo "$now"
	exit 0
fi

if [ -f "$out" ] && [ -f "$stamp" ] && [ "$(cat "$stamp")" = "$now" ]; then
	echo "driver-stamp: $out is current for its input content ($(echo "$now" | cut -c1-12))"
	exit 0
fi

rm -f "$stamp"
mkdir -p "$(dirname "$stamp")"
sh -c "$cmd"
[ -f "$out" ] || { echo "driver-stamp: the build did not produce $out" >&2; exit 1; }
# hash again: an input that moved during the build is not recorded as built
after=$(hash_inputs 2>/dev/null)
if [ "$after" = "$now" ]; then
	printf '%s\n' "$now" > "$stamp.tmp"
	mv "$stamp.tmp" "$stamp"
fi
