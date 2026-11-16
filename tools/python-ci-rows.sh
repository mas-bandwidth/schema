#!/bin/sh
# Plan every prerequisite of test-python and each top-level Go test exactly
# once. New package tests are automatically added instead of falling through a
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
    printf '%s\n' "$names" | while IFS= read -r test; do
      jq -cn --arg name "$test" --arg package "$package" --arg test "$test" '{name:$name,target:"",package:$package,test:$test}' >> "$rows"
    done
  fi
done < "$packages"
jq -cs '.' "$rows"
