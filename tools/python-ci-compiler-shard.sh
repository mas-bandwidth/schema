#!/bin/sh
# Every planner-selected compiler test must actually pass under SCHEMA_SLOW.
set -eu -o pipefail
group=${1:?missing compiler shard}
case "$group" in *[!A-Za-z0-9_+]*|+*|*+|*++*) echo "invalid compiler shard" >&2; exit 1 ;; esac
names=$(printf '%s' "$group" | tr '+' ' ')
pattern=$(printf '%s' "$group" | tr '+' '|')
label=$(printf '%s' "$group" | cksum | cut -d ' ' -f1)
exec sh test/slowgate/proof "python-ci-compiler-$label" "$names" -tags schema_leg_python ./compiler -run "^($pattern)$" -count=1
