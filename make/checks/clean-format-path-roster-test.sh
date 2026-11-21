#!/bin/sh
# Exercise the regular format-corpus command with each committed roster as
# its cases.tsv. Keep the repository's manifest intact.
set -eu
lang=$1
out=$2
root=$(pwd)
mkdir -p "$out"
stage=$(mktemp -d "$out/path-rosters.XXXXXX")
stage=$(cd "$stage" && pwd)
trap 'rm -rf "$stage"' 0

mkdir -p "$stage/make/checks/testdata"
cp .clang-format "$stage/"
cp make/checks/clean-format-path-corpus.sh "make/checks/$lang-clean-format.sh" "$stage/make/checks/"
cp -R make/checks/testdata/format-paths "$stage/make/checks/testdata/"

# Check both the status and the observed row. A green exit after silently
# skipping the final row would otherwise satisfy the missing-newline control.
status=0
check() {
	roster=$1 want=$2 detail=$3
	cp "$root/make/checks/testdata/format-rosters/$roster.tsv" "$stage/make/checks/testdata/format-paths/cases.tsv"
	got=0
	( cd "$stage" && sh make/checks/clean-format-path-corpus.sh "$lang" "$stage/out" ) > "$stage/actual" 2>&1 || got=$?
	if [ "$got" = "$want" ] && grep -Fq -- "$detail" "$stage/actual"; then
		echo "PASS $lang roster $roster"
	else
		echo "FAIL $lang roster $roster (exit $got, expected $want and '$detail')" >&2
		cat "$stage/actual" >&2
		status=1
	fi
}
check comment-only 1 'missing required case ordinary-path'
check missing-spaced-path 1 'missing required case space-in-tree'
check duplicate-ordinary 1 'duplicate case ordinary-path'
check no-final-lf 0 "PASS $lang space-in-tree (exit 0)"
check extra-case 0 "PASS $lang additional-path (exit 0)"
exit "$status"
