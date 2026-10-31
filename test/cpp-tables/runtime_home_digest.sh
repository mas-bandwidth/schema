#!/bin/sh
# The shared runtime of one generated C++ unit, as a checksum per <Base>Table.h.
#
# Every <Base>Table.h of a unit carries the SAME runtime behind package-named
# include guards (*_SCHEMA_TABLE_*, *_SCHEMA_BUILD_VERSION): whichever header a
# translation unit includes first defines it for every other file. That is only
# safe because the guarded blocks are byte-identical in every header, so the
# order the files are included in cannot change what the unit means. This prints
# `<checksum> <size> <file>` for the concatenation of those guarded blocks, one
# line per header, so a caller can require one distinct checksum per tree and
# the same one across two trees (docs/PORTING.md J2, tables-cpp-runtime-home).
#
# The per-declaration guards (*_SCHEMA_TABLE_ENUM_<Name>) are not the runtime:
# a header carries the enum helpers of the declarations IT owns, and they are
# left out.
#
# usage: runtime_home_digest.sh <dir>
set -e
dir=$1
[ -d "$dir" ] || { echo "runtime_home_digest: no directory $dir" >&2; exit 2; }
for f in "$dir"/*Table.h; do
	[ -f "$f" ] || continue
	awk '
		!open && /^#ifndef [A-Z0-9_]+_SCHEMA_(TABLE|BUILD)[A-Z0-9_]*$/ && $2 !~ /_SCHEMA_TABLE_ENUM_/ { open = 1; name = $2 }
		open { print }
		open && index($0, "#endif // " name) == 1 { open = 0 }
	' "$f" | cksum | awk -v f="$(basename "$f")" '{print $1, $2, f}'
done
