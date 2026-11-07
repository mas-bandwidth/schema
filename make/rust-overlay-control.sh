#!/bin/sh
# A NEGATIVE CONTROL THROUGH `go test -overlay` for a Rust gate that is a Go
# test in internal/codegen/rusttable: one emitter source is sabotaged by a sed
# expression into a copy under build/<name>, no tracked file is written, the
# named test is run against the copy and must go RED, and its log must name the
# clause the sabotage broke, so a control that reddened for another reason
# proves nothing.
#
# usage: make/rust-overlay-control.sh <name> <source.go> <sed-expr> <test-regex> <must-name>
set -u
name=$1 file=$2 expr=$3 run=$4 must=$5
dir=build/$name
rm -rf "$dir" && mkdir -p "$dir"
sed "$expr" "$file" > "$dir/src.go.txt"
if cmp -s "$file" "$dir/src.go.txt"; then
	echo "NEGATIVE CONTROL ($name): the sabotage did not apply to $file"
	exit 1
fi
printf '{"Replace":{"%s/%s":"%s/%s/src.go.txt"}}\n' "$PWD" "$file" "$PWD" "$dir" > "$dir/overlay.json"
if SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -overlay="$dir/overlay.json" \
	-run "$run" ./internal/codegen/rusttable/ > "$dir/log" 2>&1; then
	echo "NEGATIVE CONTROL FAILED ($name): $run stayed green with $file sabotaged"
	exit 1
fi
if ! grep -q -- "$must" "$dir/log"; then
	echo "NEGATIVE CONTROL FAILED ($name): $run went red, but not on: $must"
	cat "$dir/log"
	exit 1
fi
grep -m1 -- "$must" "$dir/log"
echo "negative control ($name): $file sabotaged turns $run RED"
