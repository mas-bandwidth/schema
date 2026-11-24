#!/bin/sh
# The fixed-form corpus table for nova-testguard (test/testguard.tsv): every
# corpus root the C++ reference writes (test/tables/fixedform_dump.cpp) against
# every port's table backend. A pair is covered when the port's tests or drivers
# name the root's file: internal/codegen/<port>table, test/<port>-* and
# make/<port>*.mk. A root none of a port's tests names is a root that port never
# reads.
#
#   sh test/testguard/fixed-corpus.sh rows | columns | pairs
set -eu
roots() { grep -o '"[a-z0-9_]*\.bin"' test/tables/fixedform_dump.cpp | tr -d '"' | sort -u; }
ports() { ls -d internal/codegen/*table | sed 's|internal/codegen/||; s|table$||' | sort; }
case "${1:-}" in
rows) roots ;;
columns) ports ;;
pairs)
	list=$(mktemp)
	trap 'rm -f "$list"' EXIT
	roots > "$list"
	for p in $(ports); do
		set -- "internal/codegen/${p}table"
		for d in test/"$p"-* make/"$p".mk make/"$p"-*.mk; do
			if [ -e "$d" ]; then set -- "$@" "$d"; fi
		done
		find "$@" -type f -exec cat {} + | tr -c 'a-z0-9_.\n' '\n' |
			awk -v p="$p" 'NR == FNR { r[$0]; next } ($0 in r) && !seen[$0]++ { print $0 "\t" p }' "$list" -
	done
	;;
*) echo "usage: sh test/testguard/fixed-corpus.sh rows|columns|pairs" >&2; exit 2 ;;
esac
