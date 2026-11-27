#!/bin/sh
# The fixed-form corpus table for tools/testguard: every root of the corpus the
# C++ reference writes (build/fixedform-corpus, made by
# `make tables-fixedform-corpus`), read from the corpus's own manifest.txt,
# against every port's table backend. A pair is covered when the port's tests or
# drivers (internal/codegen/<port>table, test/<port>-* and make/<port>*.mk) name
# the root's file, or read the manifest, which lists every root. This is what
# the tree claims; the leg jobs that run the corpus are the evidence that it
# runs. No per-PR config uses it yet: the Lua port reads 2 of the roots (its
# fix is in flight), and the table needs the corpus built.
#
#   sh test/testguard/fixed-corpus.sh rows | columns | pairs
set -eu
manifest=build/fixedform-corpus/manifest.txt
roots() {
	if [ ! -s "$manifest" ]; then
		echo "fixed-corpus: $manifest is missing or empty; run: make tables-fixedform-corpus" >&2
		exit 1
	fi
	sed -n 's/^file=\([^ ]*\.bin\) .*/\1/p' "$manifest" | sort -u
}
ports() { ls -d internal/codegen/*table | sed 's|internal/codegen/||; s|table$||' | sort; }
case "${1:-}" in
rows) roots ;;
columns) ports ;;
pairs)
	list=$(mktemp)
	trap 'rm -f "$list"' EXIT
	roots > "$list"
	[ -s "$list" ] || { echo "fixed-corpus: $manifest lists no file=*.bin root" >&2; exit 1; }
	for p in $(ports); do
		set -- "internal/codegen/${p}table"
		for d in test/"$p"-* make/"$p".mk make/"$p"-*.mk; do
			if [ -e "$d" ]; then set -- "$@" "$d"; fi
		done
		find "$@" -type f -exec cat {} + | tr -c 'a-z0-9_.\n' '\n' |
			awk -v p="$p" 'NR == FNR { r[$0]; next } $0 == "manifest.txt" { all = 1 } ($0 in r) { seen[$0] = 1 }
				END { for (x in r) if (all || (x in seen)) print x "\t" p }' "$list" -
	done
	;;
*) echo "usage: sh test/testguard/fixed-corpus.sh rows|columns|pairs" >&2; exit 2 ;;
esac
