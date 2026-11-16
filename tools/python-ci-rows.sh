#!/bin/sh
# Plan every prerequisite of test-python and every top-level Go test exactly once across bounded shards
# per package. The packet package has sixteen, to keep its heavy array and
# float tests apart; other packages use eight. New package tests are automatically added instead of falling through a
# stale hand-maintained list. The unsplit make target stays the local/nightly
# contract; each support target is one named portion of its prerequisites.
set -eu
rows=$(mktemp)
packages=$(mktemp)
trap 'rm -f "$rows" "$packages"' EXIT HUP INT TERM
: > "$rows"
make -s python-ci-support-list | while IFS= read -r target; do
  jq -cn --arg name "$target" --arg target "$target" '{name:$name,target:$target,package:"",test:""}' >> "$rows"
done
go list -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... > "$packages"
while IFS= read -r package; do
  tests=$(go test -tags schema_leg_python -list '^(Test|Example|Fuzz)' "$package")
  names=$(printf '%s\n' "$tests" | sed -n '/^\(Test\|Example\|Fuzz\)[A-Za-z0-9_]*$/p')
  if [ -z "$names" ]; then
    jq -cn --arg name "$package (compile)" --arg package "$package" '{name:$name,target:"",package:$package,test:""}' >> "$rows"
  else
    shards=8
    case "$package" in */internal/codegen/python) shards=16 ;; esac
    shard=0
    while [ "$shard" -lt "$shards" ]; do
      selected=$(printf '%s\n' "$names" | awk -v shard="$shard" -v shards="$shards" '(NR-1)%shards == shard' | paste -sd '|' -)
      if [ -n "$selected" ]; then
        jq -cn --arg name "$package shard $((shard+1))/$shards" --arg package "$package" --arg test "($selected)" '{name:$name,target:"",package:$package,test:$test}' >> "$rows"
      fi
      shard=$((shard+1))
    done
  fi
done < "$packages"
jq -cs '.' "$rows"
