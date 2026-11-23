#!/bin/sh
# make/checks/moo-pinned-list.sh LIST DIR — the moo wire-fuzz pinned-mutant
# list (make/moo-fuzz.mk tables-moo-wire-fuzz-pinned) is read whole and held to
# the saved mutants under DIR, both ways, before anything is replayed.
#
# A list whose last line has no newline is refused: the replay's shell `read`
# loop returns false on that line and drops it, so the replay reported one
# mutant fewer and agreed (finding F-055). A listed file that is not saved, a
# saved file the list does not name, and a list that names nothing are
# refused too. Toolchain-free and milliseconds; its controls are the committed
# fixtures under testdata/moo-wire-fuzz-pinned-controls
# (tables-moo-wire-fuzz-pinned-list-controls).
set -eu -o pipefail
list=$1
dir=$2
fail() { echo "moo pinned list $list: $1" >&2; exit 1; }
[ -f "$list" ] || fail "the list does not exist"
[ -d "$dir" ] || fail "the directory $dir does not exist"
[ -z "$(tail -c 1 "$list")" ] || fail "the list does not end in a newline, so its last line would not be read"
n=0
while read -r file rest; do
	case "$file" in "" | \#*) continue ;; esac
	[ -f "$file" ] || fail "$file is listed and not saved"
	n=$((n + 1))
done <"$list"
[ "$n" -gt 0 ] || fail "the list names no mutant"
for f in "$dir"/*; do
	[ -e "$f" ] || fail "$dir holds no saved mutant"
	awk -v f="$f" '$1 == f { found = 1 } END { exit !found }' "$list" || fail "$f is saved and the list does not name it, so nothing replays it"
done
echo "moo pinned list $list: $n mutant(s) listed, every saved one named"
