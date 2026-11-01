#!/bin/sh
# make/hostile-packet-units.sh <lang> <out-dir> <manifest.tsv>
#
# Generates the packet units the shared hostile corpus's `packet` rows name
# (docs/HOSTILE-CORPUS.md), for one port's hostile runner. Each distinct
# schema is generated on its own when it stands alone, and otherwise as its
# directory (a file that names its siblings' types or constants), each
# directory once. <out-dir>/units.tsv maps every packet schema to the unit
# directory that carries its code: `schema<TAB>unit`. Run from the repository
# root, after bin/schema is built.
set -eu

lang=$1
out=$2
manifest=$3

rm -rf "$out"
mkdir -p "$out"
: > "$out/units.tsv"

key() {
	printf '%s' "$1" | tr '/.' '__'
}

awk -F'\t' '!/^#/ && $2 == "packet" { print $3 }' "$manifest" | sort -u | while read -r schema; do
	unit=$(key "$schema")
	if ./bin/schema generate --lang "$lang" --out "$out/$unit" "$schema" > /dev/null 2> "$out/$unit.err"; then
		rm -f "$out/$unit.err"
	else
		rm -rf "$out/$unit"
		dir=$(dirname "$schema")
		unit=$(key "$dir")
		if [ ! -d "$out/$unit" ]; then
			./bin/schema generate --lang "$lang" --out "$out/$unit" "$dir" > /dev/null || {
				echo "hostile-packet-units: $schema generates neither alone nor as $dir:" >&2
				cat "$out/$(key "$schema").err" >&2
				exit 1
			}
		fi
		rm -f "$out/$(key "$schema").err"
	fi
	printf '%s\t%s\n' "$schema" "$unit" >> "$out/units.tsv"
done
