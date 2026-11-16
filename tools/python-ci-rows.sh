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
  corpus=false
  case "$target" in python-fixed-*) corpus=true ;; esac
  jq -cn --arg name "$target" --arg target "$target" --argjson corpus "$corpus" '{name:$name,target:$target,package:"",test:"",corpus:$corpus}' >> "$rows"
done
go list -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... > "$packages"
while IFS= read -r package; do
  corpus=false
  case "$package" in */internal/codegen/pythontable|*/internal/codegen/pythontable/*) corpus=true ;; esac
  tests=$(go test -tags schema_leg_python -list '^(Test|Example|Fuzz)' "$package")
  names=$(printf '%s\n' "$tests" | sed -n '/^\(Test\|Example\|Fuzz\)[A-Za-z0-9_]*$/p')
  # These four complete shape tests are delegated to the support row shards
  # above. Keeping the monolithic parent in an automatic package shard would
  # still hit the two-minute limit. New test names remain auto-discovered.
  case "$package" in */internal/codegen/pythontable)
    names=$(printf '%s\n' "$names" | sed '/^TestPythonFixedProofShapes$/d; /^TestPythonFixedProofShapesText$/d; /^TestPythonFixedProofShapesArrayBounds$/d; /^TestPythonFixedProofShapesNegativeControl$/d; /^TestPythonFixedProofValues$/d; /^TestPythonFixedProofValuesNegativeControl$/d; /^TestPythonFixedProofValuesTypes$/d; /^TestPythonFixedProofValuesReadRules$/d; /^TestPythonFixedProofValuesFloatBounds$/d; /^TestPythonFixedProofUnionsOptionals$/d') ;;
  esac
  if [ -z "$names" ]; then
    jq -cn --arg name "$package (compile)" --arg package "$package" --argjson corpus "$corpus" '{name:$name,target:"",package:$package,test:"",corpus:$corpus}' >> "$rows"
  else
    shards=8
    case "$package" in */internal/codegen/python) shards=16 ;; esac
    shard=0
    while [ "$shard" -lt "$shards" ]; do
      selected=$(printf '%s\n' "$names" | awk -v shard="$shard" -v shards="$shards" '(NR-1)%shards == shard' | paste -sd '|' -)
      if [ -n "$selected" ]; then
        jq -cn --arg name "$package shard $((shard+1))/$shards" --arg package "$package" --arg test "($selected)" --argjson corpus "$corpus" '{name:$name,target:"",package:$package,test:$test,corpus:$corpus}' >> "$rows"
      fi
      shard=$((shard+1))
    done
  fi
done < "$packages"
jq -cs '.' "$rows"
