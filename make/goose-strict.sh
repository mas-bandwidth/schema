#!/bin/sh
# make/goose-strict.sh — THE STRICT GATE over generated Goose (docs/PORTING.md
# J4; make/goose-gates.mk carries the targets and says why).
#
#   goose-strict.sh <goose> <tree>...
#
# Every *.goose file under each tree is checked ON ITS OWN, from its own
# directory, with `goose check` under the pinned compiler: the module and what
# it imports must parse and typecheck, and the compiler must print no warning.
# Goose v2026.2 has no warnings-as-errors switch, so this script is that
# switch: a warning line is a finding, a non-zero exit is a finding, and one
# finding is red. Each finding is printed once, as <tree path>:<line>: ...,
# whichever module's check reached it (a runtime module imported by ten
# modules warns ten times and is one finding). A tree with no module in it is
# a refusal, never a vacuous pass.
set -u

if [ $# -lt 2 ]; then
	echo "usage: goose-strict.sh <goose> <tree>..." >&2
	exit 2
fi
goose=$1
shift
if [ ! -x "$goose" ]; then
	echo "goose strict gate REFUSES: the pinned compiler is not at $goose (make dist-goose)" >&2
	exit 2
fi

findings=$(mktemp)
modules=0
trap 'rm -f "$findings"' EXIT

for tree in "$@"; do
	if [ ! -d "$tree" ]; then
		echo "goose strict gate REFUSES: $tree is not a directory" >&2
		exit 2
	fi
	n=0
	for f in $(find "$tree" -name '*.goose' | LC_ALL=C sort); do
		n=$((n + 1))
		dir=$(dirname "$f")
		out=$(cd "$dir" && "$goose" check "$(basename "$f")" 2>&1)
		rc=$?
		# a warning or an error the compiler places: <file>:<line>: <what>
		printf '%s\n' "$out" | grep -E '^[^ :]+\.goose:[0-9]+: (warning|error):' |
			sed "s|^|$dir/|" >>"$findings"
		if [ $rc -ne 0 ]; then
			first=$(printf '%s\n' "$out" | grep -v -E '^(parsed ok|typechecked ok|bce:)' | head -1)
			echo "$f: goose check exits $rc: $first" >>"$findings"
		fi
	done
	if [ $n -eq 0 ]; then
		echo "goose strict gate REFUSES: no .goose module under $tree" >&2
		exit 2
	fi
	modules=$((modules + n))
done

LC_ALL=C sort -u "$findings" -o "$findings"
count=$(wc -l <"$findings" | tr -d ' ')
if [ "$count" -ne 0 ]; then
	cat "$findings"
	echo "GOOSE STRICT GATE FAILED: $count finding(s) over $modules modules in $# trees (warnings are errors)"
	exit 1
fi
echo "goose strict gate: $modules modules in $# trees, each checked on its own under $("$goose" version), zero warnings"
